"""Private real Desktop UI; controller-owned CDP pipes never expose a TCP port."""
import base64
import json
import os
from pathlib import Path
import secrets
import select
import subprocess
import time

import clients
from runtime import Process


class Display:
    def __init__(self, process, log, auth):
        self.process, self.log, self.auth = process, log, auth

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=3)
        self.log.close()
        self.auth.unlink(missing_ok=True)


def host_display(root):
    root = Path(root)
    root.mkdir(mode=0o700, parents=True, exist_ok=False)
    auth = root/'display.auth'
    auth.touch(mode=0o600)
    cookie = secrets.token_hex(16)
    subprocess.run(['xauth', '-f', str(auth), 'add', ':199', '.', cookie],
                   check=True, capture_output=True)
    read, write = os.pipe()
    log = (root/'xephyr.log').open('xb')
    process = subprocess.Popen(['Xephyr', '-displayfd', str(write), '-screen', '1200x900',
        '-nolisten', 'tcp', '-noreset', '-extension', 'MIT-SHM', '-auth', str(auth)],
        pass_fds=(write,), stdout=log, stderr=log)
    os.close(write)
    owner = Display(process, log, auth)
    try:
        if not select.select([read], [], [], 8)[0]:
            raise RuntimeError('private display readiness timeout')
        number = os.read(read, 32).decode().strip()
        if not number.isdigit():
            raise RuntimeError('private display unavailable')
        subprocess.run(['xauth', '-f', str(auth), 'add', ':'+number, '.', cookie],
                       check=True, capture_output=True)
        return {'display': ':'+number, 'auth': str(auth),
                'socket': '/tmp/.X11-unix/X'+number}, owner
    except BaseException:
        owner.close()
        raise
    finally:
        os.close(read)


def auth_copy(directory, original):
    path = directory/'display.auth'
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(Path(original).read_bytes())
    return path


class AppProcess:
    def __init__(self, owned, send, receive, ledger):
        self.owned, self.send, self.receive, self.ledger = owned, send, receive, ledger
        self.sequence, self.buffer, self.session = 0, b'', None
        self.bootstrap, self.closed, self.protected = False, False, False

    def poll(self):
        return self.owned.poll()

    def stop(self):
        if self.closed:
            return
        self.closed = True
        self.owned.stop()
        os.close(self.send)
        os.close(self.receive)

    def rpc(self, method, params=None, page=False):
        if self.closed or self.poll() is not None:
            raise RuntimeError('private App stopped')
        self.sequence += 1
        request = {'id': self.sequence, 'method': method, 'params': params or {}}
        if page:
            request['sessionId'] = self.session
        os.write(self.send, json.dumps(request).encode()+b'\0')
        deadline = time.monotonic()+8
        while time.monotonic() < deadline:
            if b'\0' not in self.buffer:
                if select.select([self.receive], [], [], .2)[0]:
                    value = os.read(self.receive, 1048576)
                    if not value:
                        raise RuntimeError('private App debugger pipe closed')
                    self.buffer += value
                continue
            frame, self.buffer = self.buffer.split(b'\0', 1)
            try:
                value = json.loads(frame)
            except json.JSONDecodeError:
                raise RuntimeError('private App debugger protocol corruption') from None
            if value.get('id') == self.sequence:
                if 'error' in value:
                    raise RuntimeError('private App UI operation rejected')
                return value.get('result', {})
        raise TimeoutError('private App UI operation timeout')

    def evaluate(self, expression):
        value = self.rpc('Runtime.evaluate', {'expression': expression, 'returnByValue': True}, True)
        if 'exceptionDetails' in value:
            raise RuntimeError('private App UI expression failed')
        return value.get('result', {}).get('value')

    def attach(self):
        deadline = time.monotonic()+25
        while time.monotonic() < deadline:
            targets = self.rpc('Target.getTargets').get('targetInfos', [])
            pages = [target for target in targets if target.get('type') == 'page']
            if pages:
                self.session = self.rpc('Target.attachToTarget',
                    {'targetId': pages[0]['targetId'], 'flatten': True})['sessionId']
                return
            time.sleep(.2)
        raise TimeoutError('private App window unavailable')

    def visible(self, marker):
        return bool(self.evaluate('!!document.body && document.body.innerText.includes('+json.dumps(marker)+')'))

    def screenshot(self, destination):
        data = self.rpc('Page.captureScreenshot', {'format': 'png'}, True)['data']
        destination = Path(destination)
        descriptor = os.open(destination, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(descriptor, 'wb') as output:
            output.write(base64.b64decode(data))

    def select_session(self, session_id):
        # Click the rendered sidebar control; no backend RPC or internal React API.
        expression = '''(() => { const id=%s; const link=[...document.querySelectorAll('[data-app-action-sidebar-thread-id]')]
          .find(e=>e.getAttribute('data-app-action-sidebar-thread-id')===id && e.getClientRects().length); if(!link)return false;
          link.click(); return true; })()''' % json.dumps(session_id)
        deadline = time.monotonic()+20
        while time.monotonic() < deadline:
            if self.evaluate(expression):
                return
            time.sleep(.25)
        raise RuntimeError('private App session control is not rendered')

    def submit(self, text, identity=None):
        if not self.protected:
            raise RuntimeError('private App input guard not verified')
        ready = self.evaluate('''(() => { const e=[...document.querySelectorAll('textarea,[contenteditable=true]')]
          .find(e=>e.getClientRects().length && !e.disabled); if(!e)return false;
          e.focus(); return true; })()''')
        if not ready:
            raise RuntimeError('private App editor unavailable; authentication or onboarding pending')
        identity = identity or 'app-gui-'+secrets.token_hex(12)
        body = {'kind': 'codex-app', 'id': identity}
        reservation = self.ledger.call('/reserve', body)
        if not reservation.get('allow') or reservation.get('duplicate'):
            raise RuntimeError('private App input budget refused')
        self.rpc('Input.insertText', {'text': text}, True)
        self.rpc('Input.dispatchKeyEvent', {'type': 'keyDown', 'key': 'Enter', 'code': 'Enter',
            'windowsVirtualKeyCode': 13}, True)
        self.rpc('Input.dispatchKeyEvent', {'type': 'keyUp', 'key': 'Enter', 'code': 'Enter',
            'windowsVirtualKeyCode': 13}, True)
        self.ledger.call('/finish', {**body, 'result': {'code': 'unknown'}})


def start(runtime, env, bins, home, ledger, display_config, session_id=None):
    (Path(home)/'.cache').mkdir(mode=0o700, exist_ok=True)
    identity = clients.api(runtime, 'codex', '/sandbox/identity')['body']
    if not identity.get('pid') or not identity.get('start'):
        raise RuntimeError('protected App worker identity unavailable')
    values = {**env, 'DISPLAY': display_config['display'], 'XAUTHORITY': '/run/display.auth',
        'CODEX_ELECTRON_USER_DATA_PATH': '/home/sandbox/.config/Codex',
        'CODEX_CLI_PATH': bins['appbridge'], 'CODEX_APP_SERVER_FORCE_CLI': '1',
        'RA2A_SANDBOX_APP_NATIVE': bins['app_native'],
        'RA2A_SANDBOX_APP_WORKER_PID': str(identity['pid']),
        'RA2A_SANDBOX_APP_WORKER_START': str(identity['start']),
        'RA2A_SANDBOX_APP_SOCKET_DIR': '/run/app-bridges',
        'RA2A_SANDBOX_HOST_NETNS': runtime.host_netns}
    passwd, group = runtime.root/'app.passwd', runtime.root/'app.group'
    passwd.write_text('sandbox:x:1000:1000:Sandbox:/home/sandbox:/bin/sh\n')
    group.write_text('sandbox:x:1000:\n')
    control = runtime.root/('app-control-'+secrets.token_hex(5))
    control.mkdir(mode=0o700)
    auth = auth_copy(control, display_config['auth'])
    mounts = [(str(control), '/run/app-control', False),
              (bins['app_guard'], '/run/app_guard.so', True),
              (str(passwd), '/etc/passwd', True), (str(group), '/etc/group', True),
              (str(auth), '/run/display.auth', True),
              (display_config['socket'], display_config['socket'], True)]
    launch = """import os,sys,fcntl,subprocess
r=fcntl.fcntl(0,fcntl.F_DUPFD,10); w=fcntl.fcntl(1,fcntl.F_DUPFD,10)
log=fcntl.fcntl(os.open('/home/sandbox/.cache/app-output.log',os.O_CREAT|os.O_APPEND|os.O_WRONLY,0o600),fcntl.F_DUPFD,10)
a,b=os.pipe()
bus=subprocess.Popen(['dbus-daemon','--session','--nofork','--print-address='+str(b)],pass_fds=(b,),stdout=log,stderr=log)
os.close(b); os.environ['DBUS_SESSION_BUS_ADDRESS']=os.read(a,4096).decode().strip(); os.close(a)
os.dup2(r,3); os.dup2(w,4); os.set_inheritable(3,True); os.set_inheritable(4,True)
os.dup2(os.open('/dev/null',os.O_RDONLY),0); os.dup2(log,1)
os.close(r); os.close(w); os.environ['LD_PRELOAD']='/run/app_guard.so'; os.execv(sys.argv[1],sys.argv[1:])
"""
    command = runtime._command('codex', ['python3', '-c', launch,
        bins['app'], '--ozone-platform=x11', '--proxy-server=http://127.0.0.1:7890',
        '--password-store=basic', '--remote-debugging-pipe'], values, mounts)
    position = command.index('bwrap')+1
    command[position:position] = ['--unshare-user', '--uid', '1000', '--gid', '1000']
    logs = runtime.root/'logs'
    logs.mkdir(mode=0o700, exist_ok=True)
    log = (logs/('app-'+secrets.token_hex(5)+'.log')).open('xb')
    process = subprocess.Popen(command, env={'PATH': '/usr/sbin:/usr/bin:/bin'},
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log, start_new_session=True)
    owned = Process(process, logs=(process.stdin, process.stdout, log))
    runtime.processes.append(owned)
    app = AppProcess(owned, os.dup(process.stdin.fileno()), os.dup(process.stdout.fileno()), ledger)
    try:
        app.attach()
        deadline = time.monotonic()+20
        while not app.evaluate('!!document.body'):
            if time.monotonic() > deadline:
                raise TimeoutError('private App renderer readiness timeout')
            time.sleep(.1)
        (control/'activate').touch(mode=0o600, exist_ok=False)
        while not (control/'protected').exists():
            if app.poll() is not None or time.monotonic() > deadline:
                raise RuntimeError('private App guard activation failed')
            time.sleep(.05)
        auth.write_bytes(b'')  # Existing X11 clients retain their authenticated connection.
        if auth.stat().st_size:
            raise RuntimeError('private App display authorization withdrawal failed')
        app_pid = int((control/'protected').read_text())
        probe = '''import os,sys
from pathlib import Path
if int(next(s.split()[1] for s in Path('/proc/self/status').read_text().splitlines() if s.startswith('CapEff:')),16):sys.exit(4)
for fd in (3,4):
 try: os.close(os.open('/proc/'+sys.argv[1]+'/fd/'+str(fd),os.O_WRONLY))
 except PermissionError: continue
 sys.exit(5)
print('APP_PIPE_DENIED')
'''
        check = runtime.spawn('codex', ['python3', '-c', probe, str(app_pid)])
        if check.process.wait(timeout=8) != 0:
            raise RuntimeError('private App debugger pipe accessible to Agent')
        app.rpc('Target.getTargets')
        app.protected = True
        if session_id:
            app.select_session(session_id)
        else:
            deadline = time.monotonic()+45
            while time.monotonic() < deadline:
                if app.evaluate("!![...document.querySelectorAll('textarea,[contenteditable=true]')].find(e=>e.getClientRects().length&&!e.disabled)"):
                    break
                if app.visible('Sign in to ChatGPT'):
                    raise RuntimeError('private App authentication unavailable')
                time.sleep(.25)
            app.submit('Reply only READY_SANDBOX_APP. Do not call any tools.', 'bootstrap-app')
            app.bootstrap = True
        return app
    except BaseException:
        app.stop()
        raise
