"""No Agent, credentials or model: nested X display + private bwrap/D-Bus."""
import json
import os
from pathlib import Path
import secrets
import selectors
import subprocess
import sys


def main():
    root = Path(sys.argv[1]).resolve()
    root.mkdir(mode=0o700, parents=True, exist_ok=False)
    auth = root / 'display.auth'
    auth.touch(mode=0o600)
    cookie = secrets.token_hex(16)  # Only this disposable display's cookie.
    subprocess.run(['xauth', '-f', str(auth), 'add', ':199', '.', cookie], check=True)
    uid, gid = os.getuid(), os.getgid()
    (root / 'passwd').write_text(f'sandbox:x:{uid}:{gid}:Sandbox:/home/test:/bin/sh\n')
    (root / 'group').write_text(f'sandbox:x:{gid}:\n')
    readfd, writefd = os.pipe()
    with (root / 'xephyr.log').open('w') as log:
        server = subprocess.Popen(['Xephyr', '-displayfd', str(writefd), '-screen', '800x600',
                                   '-nolisten', 'tcp', '-noreset', '-auth', str(auth)],
                                  pass_fds=(writefd,), stdout=log, stderr=log)
        os.close(writefd)
        client = None
        result = {'agentStarted': False, 'modelCalls': 0}
        try:
            selector = selectors.DefaultSelector()
            selector.register(readfd, selectors.EVENT_READ)
            if not selector.select(8):
                raise RuntimeError('nested display did not become ready')
            display = os.read(readfd, 32).decode().strip()
            if not display.isdigit():
                raise RuntimeError('nested display start failed; see xephyr.log')
            subprocess.run(['xauth', '-f', str(auth), 'add', ':'+display, '.', cookie], check=True)
            args = ['bwrap', '--unshare-all', '--die-with-parent', '--new-session', '--clearenv']
            for path in ['/usr', '/bin', '/lib', '/lib64']:
                args += ['--ro-bind', path, path]
            args += ['--proc', '/proc', '--dev', '/dev', '--tmpfs', '/tmp', '--tmpfs', '/run',
                     '--dir', '/etc', '--dir', '/home', '--dir', '/home/test',
                     '--ro-bind', str(root/'passwd'), '/etc/passwd',
                     '--ro-bind', str(root/'group'), '/etc/group',
                     '--dir', '/tmp/.X11-unix',
                     '--ro-bind', '/tmp/.X11-unix/X'+display, '/tmp/.X11-unix/X'+display,
                     '--ro-bind', str(auth), '/tmp/display.auth',
                     '--setenv', 'HOME', '/home/test', '--setenv', 'PATH', '/usr/bin:/bin',
                     '--setenv', 'DISPLAY', ':'+display, '--setenv', 'XAUTHORITY', '/tmp/display.auth',
                     '--setenv', 'XDG_RUNTIME_DIR', '/run/private',
                     '/bin/sh', '-c', 'mkdir -m700 /run/private; '
                     'test ! -e /home/zhangxu/.codex || exit 7; '
                     'ls -la /tmp/.X11-unix; '
                     'test "$(ls -1 /tmp/.X11-unix)" = "X'+display+'" || exit 8; '
                     'test -S /tmp/.X11-unix/X'+display+' || exit 9; '
                     'dbus-run-session -- sh -c \'xdpyinfo >/dev/null && '
                     'test -n "$DBUS_SESSION_BUS_ADDRESS" && '
                     'echo nested-display-and-private-dbus-pass\'']
            client = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            out, err = client.communicate(timeout=15)
            result = {'display': display, 'exit': client.returncode, 'stdout': out, 'stderr': err,
                      'agentStarted': False, 'modelCalls': 0}
            (root/'result.json').write_text(json.dumps(result, indent=2))
            print(json.dumps(result))
            if client.returncode:
                raise RuntimeError('private display client failed')
        finally:
            os.close(readfd)
            if client is not None:
                if client.poll() is None:
                    client.terminate()
                client.wait(timeout=5)
            if server.poll() is None:
                server.terminate()
            server.wait(timeout=5)
            result['serverPID'] = server.pid
            result['serverReaped'] = True
            result['serverExit'] = server.returncode
            result['clientReaped'] = client is not None and client.returncode is not None
            # Keep logs; explicitly remove only our disposable display cookie.
            auth.unlink()
            result['disposableCookieRemoved'] = True
            (root/'result.json').write_text(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
