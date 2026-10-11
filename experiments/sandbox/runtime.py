"""Linux test runtime: owned process groups, private LAN and minimal mounts.

Invoke only inside the controller's new user/network/PID namespace. Credentials
are already projected into per-node homes; this module never reads host Auth.
"""
import errno
import fcntl
import os
from pathlib import Path
import pty as pty_module
import select
import signal
import subprocess
import struct
import termios
import time


KINDS = ('codex', 'opencode', 'pi')


def _run(*argv):
    return subprocess.run(argv, check=True, capture_output=True, text=True).stdout


def supervise(argv, env, timeout, stdout, stderr, stdin=subprocess.DEVNULL):
    """Run an outer namespace controller with a bounded lifetime.

    argv must retain its outer PID1 in the newly created process group. Killing
    that PID1 also terminates descendants which created their own groups inside
    its PID namespace. stdout/stderr are caller-owned file handles (not PIPE).
    Returns pid/returncode/timed_out; never deletes profiles or closes log files.
    """
    process = subprocess.Popen(argv, env=env, stdin=stdin,
                               stdout=stdout, stderr=stderr, start_new_session=True)
    timed_out = False
    try:
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        _terminate_group(process)
    except BaseException:
        _terminate_group(process)
        raise
    return {'pid': process.pid, 'returncode': process.returncode, 'timed_out': timed_out}


def _terminate_group(process):
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    # Leader may exit before an ignoring PID1. Kill its group after the full
    # grace period regardless of leader state; PID1 exit kills its descendants.
    time.sleep(2)
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=5)


def _codex_package(path):
    """Only a fixed native release package, never .codex or its selector tree."""
    if (path.name != 'codex' or path.parent.name != 'bin' or len(path.parents) < 6
            or path.parents[2].name != 'releases'
            or path.parents[3].name not in ('app-server-daemon', 'standalone')
            or path.parents[4].name != 'packages' or path.parents[5].name != '.codex'):
        return path
    release = path.parents[1]
    allowed = {'bin', 'codex', 'codex-package.json', 'codex-path', 'codex-resources'}
    if not (release/'codex-package.json').is_file():
        raise ValueError('Codex native package marker is missing')
    if any(item.name not in allowed for item in release.iterdir()):
        raise ValueError('Codex release contains unexpected top-level files')
    for item in release.rglob('*'):
        if item.name in ('auth.json', 'config.toml', 'credentials.json', 'tokens.json'):
            raise ValueError('Codex release unexpectedly contains credentials or config')
        if item.is_symlink() and not item.resolve(strict=True).is_relative_to(release):
            raise ValueError('Codex package symbolic link escapes its fixed release')
    return release


class Process:
    """A controller-owned process; PTY input must pass the controller budget."""
    def __init__(self, process, master=None, logs=()):
        self.process, self.master, self.logs = process, master, logs
        self.pid = process.pid

    def poll(self):
        return self.process.poll()

    def write(self, value):
        if self.master is None:
            raise RuntimeError('process has no interactive terminal')
        value = value.encode() if isinstance(value, str) else value
        return os.write(self.master, value)

    def read(self, timeout=.1, size=65536):
        if self.master is None:
            raise RuntimeError('process has no interactive terminal')
        if not select.select([self.master], [], [], timeout)[0]:
            return b''
        try:
            return os.read(self.master, size)
        except OSError as error:
            if error.errno == errno.EIO:
                return b''
            raise

    def stop(self):
        if self.process.poll() is None:
            # Only this Popen's newly created group, never a name or host PID.
            os.killpg(self.pid, signal.SIGTERM)
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(self.pid, signal.SIGKILL)
        self.process.wait(timeout=3)
        if self.master is not None:
            os.close(self.master)
            self.master = None
        for log in self.logs:
            log.close()


class Runtime:
    def __init__(self, root: Path, bins: dict, profiles: dict, host_netns: str):
        self.root = Path(root).resolve()
        self.bins, self.profiles, self.host_netns = bins, profiles, host_netns
        self.children, self.processes, self.nodes = {}, [], {}
        self.pid_holders = {}
        self.counter, self.started = 0, False
        self.mounts = {kind: [] for kind in KINDS}

    def __enter__(self):
        self.start()
        return self

    def __exit__(self, *_):
        self.stop()

    def start(self):
        if self.started:
            raise RuntimeError('runtime already started')
        own = os.readlink('/proc/self/ns/net')
        # A new PID namespace has a controller PID of 1 (or its direct child).
        if not self.host_netns or own == self.host_netns or os.getuid() != 0:
            raise RuntimeError('private outer user/network namespace required')
        if Path('/proc/1/ns/net').readlink().as_posix() != own:
            raise RuntimeError('private outer PID namespace required')
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.root.chmod(0o700)
        (self.root/'passwd').write_text('sandbox:x:0:0:Sandbox:/home/sandbox:/bin/sh\n')
        (self.root/'group').write_text('sandbox:x:0:\n')
        # Public DNS metadata only; no bind of the host /etc tree.
        dns = Path('/etc/resolv.conf').read_text()
        (self.root/'resolv.conf').write_text(dns)
        _run('ip', 'link', 'add', 'matrix-br', 'type', 'bridge')
        self.started = True
        try:
            _run('ip', 'link', 'set', 'matrix-br', 'up')
            for index, kind in enumerate(KINDS, 1):
                child = subprocess.Popen(['unshare', '--user', '--map-root-user', '--net', '--pid', '--fork', '--mount-proc',
                                          'python3', '-c', 'import ctypes,time,signal,sys; '
                                          'ctypes.CDLL(None).prctl(4,0,0,0,0); '
                                          'signal.signal(signal.SIGTERM,lambda *a:sys.exit(0)); '
                                          'print("PRIVATE_PID_READY",flush=True); time.sleep(86400)'],
                                         stdout=subprocess.PIPE, start_new_session=True,
                                         env={'PATH': '/usr/bin:/bin'})
                self.children[kind] = Process(child, logs=(child.stdout,))
                deadline = time.monotonic()+3
                while os.readlink(f'/proc/{child.pid}/ns/net') == own:
                    if child.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError('node network namespace did not start')
                    time.sleep(.01)
                if not select.select([child.stdout], [], [], 3)[0] or child.stdout.readline() != b'PRIVATE_PID_READY\n':
                    raise RuntimeError('private PID holder did not become ready')
                held = Path(f'/proc/{child.pid}/task/{child.pid}/children').read_text().split()
                if len(held) != 1:
                    raise RuntimeError('private PID holder identity is ambiguous')
                self.pid_holders[kind] = int(held[0])
                _run('ip', 'link', 'add', f'mv{index}', 'type', 'veth', 'peer', 'name', f'mp{index}')
                _run('ip', 'link', 'set', f'mp{index}', 'netns', str(child.pid))
                _run('ip', 'link', 'set', f'mv{index}', 'master', 'matrix-br')
                _run('ip', 'link', 'set', f'mv{index}', 'up')
                prefix = self._prefix(kind)
                _run(*prefix, 'ip', 'link', 'set', 'lo', 'up')
                _run(*prefix, 'ip', 'addr', 'add', f'198.18.0.{index}/24', 'dev', f'mp{index}')
                _run(*prefix, 'ip', 'link', 'set', f'mp{index}', 'up')
                if 'default' in _run(*prefix, 'ip', 'route'):
                    raise RuntimeError('node has an external default route')
        except BaseException:
            self.stop()
            raise

    def namespace_pid(self, kind):
        return self.children[kind].pid

    def _prefix(self, kind):
        return ['nsenter', '-t', str(self.namespace_pid(kind)), '-n']

    def add_mount(self, kind, source, target, readonly=True):
        """Controller-only display/relay mounts, never copied from Agent input."""
        source = Path(source).resolve(strict=True)
        target = str(target)
        if not target.startswith('/') or target in ('/', '/proc', '/home', '/home/sandbox'):
            raise ValueError('invalid supplemental mount destination')
        self.mounts[kind].append((str(source), target, readonly))

    def _binary_mounts(self):
        paths = set()
        for info in self.bins.values():
            values = [info] if isinstance(info, str) else [
                info.get('executable', ''), *info.get('args', []), info.get('sdk', '')]
            for value in values:
                if not value or not str(value).startswith('/'):
                    continue
                path = _codex_package(Path(value).resolve(strict=True))
                # Bundled Pi imports packages beneath its dedicated Node prefix.
                # Other executables mount individually: their parent could hold Auth.
                if '/node_modules/' in str(path):
                    path = Path(str(path).split('/node_modules/')[0])
                if not any(path == Path(p) or Path(p) in path.parents
                           for p in ('/usr', '/bin', '/lib', '/lib64')):
                    paths.add(str(path))
        return sorted(paths)

    def _command(self, kind, argv, env, extra_mounts=()):
        if kind not in self.children:
            raise RuntimeError('node is not running')
        profile = self.profiles[kind]
        home = Path(profile['home'] if isinstance(profile, dict) else profile).resolve(strict=True)
        if home == Path('/') or self.root not in home.parents:
            raise ValueError('node home must be a private runtime descendant')
        (home/'work').mkdir(mode=0o700, exist_ok=True)
        private_tmp = home.parent/'tmp'
        private_tmp.mkdir(mode=0o700, exist_ok=True)
        args = self._prefix(kind)+[
            '--user=/proc/'+str(self.namespace_pid(kind))+'/ns/user', '--preserve-credentials',
            '--pid=/proc/'+str(self.namespace_pid(kind))+'/ns/pid_for_children',
            '--mount=/proc/'+str(self.pid_holders[kind])+'/ns/mnt', 'bwrap', '--cap-drop', 'ALL', '--unshare-ipc', '--unshare-uts',
                                  '--unshare-cgroup', '--die-with-parent', '--clearenv']
        for path in ('/usr', '/bin', '/lib', '/lib64'):
            if Path(path).exists():
                args += ['--ro-bind', path, path]
        args += ['--proc', '/proc', '--dev', '/dev', '--bind', str(private_tmp), '/tmp', '--tmpfs', '/run',
                 '--dir', '/etc', '--dir', '/home', '--bind', str(home), '/home/sandbox',
                 '--ro-bind', str(self.root/'passwd'), '/etc/passwd',
                 '--ro-bind', str(self.root/'group'), '/etc/group',
                 '--ro-bind', str(self.root/'resolv.conf'), '/etc/resolv.conf']
        for path in ('/etc/fonts', '/etc/ssl/certs', '/etc/os-release'):
            if Path(path).exists():
                args += ['--ro-bind', path, path]
        for directory in self._binary_mounts():
            args += ['--ro-bind', directory, directory]
        for source, target, readonly in [*self.mounts[kind], *extra_mounts]:
            args += ['--ro-bind' if readonly else '--bind', str(source), str(target)]
        values = {'HOME': '/home/sandbox', 'USERPROFILE': '/home/sandbox',
                  'CODEX_HOME': '/home/sandbox/.codex', 'PATH': '/usr/bin:/bin',
                  'XDG_CONFIG_HOME': '/home/sandbox/.config',
                  'XDG_DATA_HOME': '/home/sandbox/.local/share',
                  'XDG_CACHE_HOME': '/home/sandbox/.cache', 'TERM': 'xterm-256color',
                  'LANG': 'C.UTF-8', 'RA2A_PI_SESSION_DIR': '/home/sandbox/.config/ra2a/pi-sessions',
                  'RA2A_OC_SESSION_DIR': '/home/sandbox/.config/ra2a/opencode-sessions'}
        values.update(env or {})
        if any(key in values for key in ('LD_PRELOAD', 'LD_LIBRARY_PATH')):
            raise ValueError('loader overrides are prohibited')
        for key, value in values.items():
            args += ['--setenv', key, str(value)]
        return args+['--chdir', '/home/sandbox/work', '--', *map(str, argv)]

    def spawn(self, kind, argv, env=None, pty=False, extra_mounts=(), mounts=()):
        command = self._command(kind, argv, env, [*extra_mounts, *mounts])
        self.counter += 1
        logdir = self.root/'logs'
        logdir.mkdir(mode=0o700, exist_ok=True)
        master, slave = pty_module.openpty() if pty else (None, None)
        logs = ()
        if pty:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 120, 0, 0))
            options = {'stdin': slave, 'stdout': slave, 'stderr': slave}
        else:
            out = (logdir/f'{kind}-{self.counter}.stdout').open('xb')
            err = (logdir/f'{kind}-{self.counter}.stderr').open('xb')
            logs = (out, err)
            options = {'stdin': subprocess.DEVNULL, 'stdout': out, 'stderr': err}
        try:
            child = subprocess.Popen(command, env={'PATH': '/usr/sbin:/usr/bin:/bin'},
                                     start_new_session=True, **options)
        except BaseException:
            if master is not None:
                os.close(master)
            for stream in logs:
                stream.close()
            raise
        finally:
            if slave is not None:
                os.close(slave)
        owned = Process(child, master, logs)
        self.processes.append(owned)
        return owned

    def start_node(self, kind, node_id=None, pin=None, budget_socket=None, env=None):
        values = dict(env or {})
        values.update({'RA2A_SANDBOX_KIND': 'codex-cli' if kind == 'codex' else kind,
                       'RA2A_SANDBOX_NODE_ID': node_id or values.get('RA2A_SANDBOX_NODE_ID', ''),
                       'RA2A_SANDBOX_PIN': pin or values.get('RA2A_SANDBOX_PIN', ''),
                       'RA2A_SANDBOX_BUDGET_SOCKET': '/run/budget.sock',
                       'RA2A_SANDBOX_HOST_NETNS': self.host_netns,
                       'RA2A_CONTROL_ADDRESS': '127.0.0.1:47321'})
        binary = self.bins['worker']
        binary = binary['executable'] if isinstance(binary, dict) else binary
        mounts = [(str(budget_socket), '/run/budget.sock', True)] if budget_socket else []
        process = self.spawn(kind, [binary], env=values, extra_mounts=mounts)
        self.nodes[kind] = process
        return process

    def stop(self):
        errors = []
        for process in reversed(self.processes):
            try:
                process.stop()
            except (OSError, subprocess.TimeoutExpired) as error:
                errors.append(type(error).__name__)
        self.processes.clear()
        for process in reversed(list(self.children.values())):
            try:
                process.stop()
            except (OSError, subprocess.TimeoutExpired) as error:
                errors.append(type(error).__name__)
        self.children.clear()
        self.pid_holders.clear()
        if self.started:
            try:
                _run('ip', 'link', 'delete', 'matrix-br')
            except subprocess.CalledProcessError:
                errors.append('bridge-cleanup')
        self.started = False
        if errors:
            raise RuntimeError('owned sandbox resources could not be fully reaped: '+','.join(errors))
