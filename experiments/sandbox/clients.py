"""Real TUI startup; all model-driving bootstrap inputs reserve a slot."""
import json
from pathlib import Path
import subprocess
import threading
import time
import tomllib


class OwnerWithdrawalTimeout(RuntimeError):
    """The prior native owner remains reachable; reopening was not attempted."""
    code = 'NATIVE_OWNER_STILL_HELD'


def withdrawal_timeout(kind, home):
    if kind != 'codex':
        return 12
    config = tomllib.loads((Path(home)/'.codex/config.toml').read_text())
    delay = config.get('thread_unload_delay_secs', 60)  # Official 0.162.1 default.
    if type(delay) is not int or delay < 0:
        raise ValueError('invalid private native thread unload delay')
    # Poll the actual endpoint; this allowance does not force native withdrawal.
    return min(delay + 12, 180)


API_CODE = '''import json,sys,urllib.request,urllib.error
url,body=sys.argv[1:];body=None if body=='null' else body.encode()
try:
 r=urllib.request.build_opener(urllib.request.ProxyHandler({})).open(urllib.request.Request(url,data=body,headers={'Content-Type':'application/json'}),timeout=12)
 print(json.dumps({'code':r.status,'body':json.loads(r.read() or b'{}')}))
except urllib.error.HTTPError as e: print(json.dumps({'code':e.code,'body':{'error':'native/control HTTP rejected'}}))
'''


def api(runtime, kind, path, body=None, port=47321):
    result = subprocess.run([*runtime._prefix(kind), 'python3', '-c', API_CODE,
                             f'http://127.0.0.1:{port}'+path, json.dumps(body)],
                            env={'PATH': '/usr/bin:/bin'}, capture_output=True, text=True, timeout=16)
    if result.returncode:
        raise RuntimeError('private API unavailable')
    return json.loads(result.stdout)


def terminal(process):
    # Answer terminal protocol queries, never insert model/user prompt text.
    def drain():
        while process.master is not None and process.poll() is None:
            try:
                output = process.read(.2)
                if getattr(process, "echo", False):
                    import sys
                    sys.stdout.buffer.write(output); sys.stdout.buffer.flush()
                if b'\x1b[6n' in output:
                    process.write(b'\x1b[1;1R')
                if b'\x1b[c' in output:
                    process.write(b'\x1b[?1;2c')
                if b'\x1b[>c' in output:
                    process.write(b'\x1b[>0;0;0c')
                for query, color in ((b'10', b'ffff/ffff/ffff'), (b'11', b'0000/0000/0000')):
                    if b'\x1b]'+query+b';?' in output:
                        process.write(b'\x1b]'+query+b';rgb:'+color+b'\x1b\\')
            except OSError:
                break
    threading.Thread(target=drain, daemon=True).start()


def submit(process, text):
    """Caller reserves one input first; separate paste from submit without retry."""
    process.write(text)
    time.sleep(.5)  # Codex 0.162.1 Enter paste suppression lasts 120 ms.
    process.write('\r')


def configure(home, kind, source, bins):
    home = Path(home)
    (home/'work').mkdir(mode=0o700, exist_ok=True)
    if kind == 'pi':
        directory = home/'.pi/agent/extensions'
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        (directory/'ra2a.js').write_bytes((source/'internal/pi/extension.mjs').read_bytes())
    elif kind == 'opencode':
        path = home/'.config/opencode/opencode.json'
        config = json.loads(path.read_text())
        config['mcp'] = {'ra2a': {'type': 'local', 'command': [bins['ra2a'], 'mcp'], 'enabled': True}}
        config['permission'] = {'*': 'deny', 'ra2a_*': 'allow'}
        path.write_text(json.dumps(config))
    else:
        path = home/'.codex/config.toml'
        with path.open('a') as stream:
            stream.write('\n[projects."/home/sandbox/work"]\ntrust_level = "trusted"\n[mcp_servers.ra2a]\ncommand = '+json.dumps(bins['ra2a'])+'\nargs = ["mcp"]\n')


def start(runtime, kind, env, bins, home, ledger, source):
    configure(home, kind, source, bins)
    if kind == 'codex':
        daemon = runtime.spawn(kind, [bins['codex'], 'app-server', 'daemon', 'start'], env)
        daemon.process.wait(timeout=30)
        if daemon.poll() != 0:
            raise RuntimeError('private official daemon could not start')
        process = runtime.spawn(kind, [bins['codex'], '--no-alt-screen'], env, pty=True)
        terminal(process)
        # TUI owns creation. Its first model input is an explicit budgeted slot.
        time.sleep(4)
        record = ledger.call('/reserve', {'kind': 'codex-cli', 'id': 'bootstrap-codex'})
        if not record['allow'] or record['duplicate']:
            raise RuntimeError('bootstrap budget unavailable; not replayed')
        submit(process, 'Reply only READY_SANDBOX. Do not call any tools.')
        for _ in range(40):
            paths = list((Path(home)/'.codex/sessions').rglob('*.jsonl'))
            for path in paths:
                for line in path.read_text().splitlines():
                    row = json.loads(line)
                    if row.get('type') == 'session_meta':
                        identity = row['payload']['id']
                        api(runtime, kind, '/sandbox/register', {'id': identity})
                        return process
            if process.poll() is not None:
                raise RuntimeError('CLI TUI exited')
            time.sleep(.5)
        raise RuntimeError('CLI native session was not created')
    if kind == 'opencode':
        env = {**env, 'RA2A_OPENCODE_BINARY': bins['opencode']}
        process = runtime.spawn(kind, [bins['ocwrapper'], '--ra2a'], env, pty=True)
    else:
        env = {**env, 'PATH': str(Path(bins['node']).parent)+':/usr/bin:/bin'}
        process = runtime.spawn(kind, [bins['node'], '--import', '/suite/network.mjs', bins['pi']], env, pty=True)
    terminal(process)
    return process


def endpoint(runtime, kind, timeout=35):
    stop = time.monotonic()+timeout
    while time.monotonic() < stop:
        try:
            rows = api(runtime, kind, '/v1/targets')['body']
            if isinstance(rows, dict):
                rows = rows.get('targets', [])
            for node in rows:
                if node.get('sessionsStale') is not False or node.get('status') != 'ready':
                    continue
                for session in node.get('sessions', []):
                    if session.get('address', '').startswith('ra2a://sandbox-'):
                        wanted = 'codex-cli' if kind == 'codex' else kind
                        if session.get('agent') == wanted and node['id'].endswith('-'+kind):
                            return session
        except (RuntimeError, ValueError, subprocess.TimeoutExpired):
            pass
        time.sleep(.5)
    raise RuntimeError('real attached endpoint not published')


def resume(runtime, kind, env, bins, home, session, old):
    allowance = withdrawal_timeout(kind, home)
    old.stop()
    # Require the old attachment to withdraw before accepting the same ID again.
    # A published ID alone cannot prove that the reopened TUI owns it.
    deadline = time.monotonic()+allowance
    while True:
        rows = api(runtime, kind, '/v1/targets')['body']
        if isinstance(rows, dict):
            rows = rows.get('targets', [])
        if not any(s.get('id') == session for n in rows for s in n.get('sessions', [])):
            break
        if time.monotonic() >= deadline:
            raise OwnerWithdrawalTimeout('old native attachment did not withdraw; resume not attempted')
        time.sleep(.5)
    if kind == 'codex':
        argv = [bins[kind], 'resume', session, '--no-alt-screen']
    elif kind == 'opencode':
        env = {**env, 'RA2A_OPENCODE_BINARY': bins[kind]}
        argv = [bins['ocwrapper'], '--ra2a', '--session', session]
    else:
        files = [p for p in (Path(home)/'.pi/agent/sessions').rglob('*.jsonl')
                 if json.loads(p.read_text().splitlines()[0]).get('id') == session.removeprefix('pi.')]
        if len(files) != 1:
            raise RuntimeError('native resume file not unambiguous')
        inside = '/home/sandbox/'+str(files[0].relative_to(home))
        argv = [bins['node'], '--import', '/suite/network.mjs', bins['pi'], '--session', inside]
    process = runtime.spawn(kind, argv, env, pty=True)
    terminal(process)
    return process
