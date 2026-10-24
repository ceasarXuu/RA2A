"""No-credential macOS preflight and 4x4 diagnostic matrix dry run.

This never imports Auth, never launches a model client for a prompt and never
uses the production RA2A service. It proves the macOS-specific isolation
primitives, private client homes, the real mDNS/DTLS/CoAP path with a
test-scoped service type, the receive budget, and a 4x4 delivery matrix.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid

import budget
import relay

PROJECT = Path(__file__).resolve().parents[2]
HOME = Path.home()
KINDS = ('codex-cli', 'codex-app', 'opencode', 'pi')
NODE_SPECS = (('codex-cli', 'r2s-mac-codex'), ('opencode', 'r2s-mac-opencode'), ('pi', 'r2s-mac-pi'))
PROTECTED = ('.codex/auth.json', '.codex/config.toml', '.config/ra2a/config.json',
             '.config/opencode/opencode.json', '.local/share/opencode/auth.json',
             '.pi/agent/auth.json', '.pi/agent/settings.json', '.pi/agent/models-store.json')
SENSITIVE = ('.codex', '.config/ra2a', '.config/opencode', '.local/share/opencode',
             '.local/state/opencode', '.cache/opencode', '.pi')

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def protected(home):
    return {name: digest(home/name) for name in PROTECTED if (home/name).is_file()}

def quote(value):
    return '"' + str(value).replace('\\', '\\\\').replace('"', '\\"') + '"'

def sandbox_profile(root, home):
    denied = ' '.join('(subpath '+quote(home/name)+')' for name in SENSITIVE)
    return ('(version 1)\n'
            '(allow default)\n'
            f'(deny file-read* {denied})\n'
            f'(deny file-write* {denied})\n'
            f'(allow file-read* (subpath {quote(root)}))\n'
            f'(allow file-write* (subpath {quote(root)}))\n')

def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]

def http_json(port, path, body=None, timeout=10):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(f'http://127.0.0.1:{port}{path}', data=data,
                                     headers={'Content-Type': 'application/json'} if data else {},
                                     method='POST' if data else 'GET')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=timeout) as response:
        return json.loads(response.read() or b'{}')

def make_dirs(root, label):
    base = root/'nodes'/label
    for name in ('home', 'tmp', 'work', 'xdg/config', 'xdg/data', 'xdg/state', 'xdg/cache',
                 'pi-sessions', 'oc-sessions'):
        (base/name).mkdir(mode=0o700, parents=True, exist_ok=True)
    return base

def client_env(root, label):
    base = make_dirs(root, 'client-'+label)
    path = ':'.join([str(HOME/'.local/bin'), str(HOME/'.opencode/bin'),
                     str(HOME/'.local/share/mise/installs/node/lts/bin'),
                     '/opt/homebrew/bin', '/usr/local/bin', '/usr/bin', '/bin', '/usr/sbin', '/sbin'])
    return {'HOME': str(base/'home'), 'TMPDIR': str(base/'tmp'),
            'XDG_CONFIG_HOME': str(base/'xdg/config'), 'XDG_DATA_HOME': str(base/'xdg/data'),
            'XDG_STATE_HOME': str(base/'xdg/state'), 'XDG_CACHE_HOME': str(base/'xdg/cache'),
            'CODEX_HOME': str(base/'home'/'.codex'), 'RA2A_PI_SESSION_DIR': str(base/'pi-sessions'),
            'RA2A_OC_SESSION_DIR': str(base/'oc-sessions'), 'PATH': path,
            'LANG': 'en_US.UTF-8', 'NO_COLOR': '1'}

def node_env(root, label, kind, node_id, port):
    base = make_dirs(root, label)
    return {'HOME': str(base/'home'), 'TMPDIR': str(base/'tmp'),
            'XDG_CONFIG_HOME': str(base/'xdg/config'), 'XDG_DATA_HOME': str(base/'xdg/data'),
            'XDG_STATE_HOME': str(base/'xdg/state'), 'XDG_CACHE_HOME': str(base/'xdg/cache'),
            'CODEX_HOME': str(base/'home'/'.codex'), 'RA2A_PI_SESSION_DIR': str(base/'pi-sessions'),
            'RA2A_OC_SESSION_DIR': str(base/'oc-sessions'),
            'RA2A_SANDBOX_ROOT': str(root), 'RA2A_SANDBOX_NODE_ID': node_id,
            'RA2A_SANDBOX_KIND': kind, 'RA2A_SANDBOX_PIN': 'T3ST42',
            'RA2A_SANDBOX_DIAGNOSTIC': '1',
            'RA2A_SANDBOX_APP_DIAGNOSTIC': '1' if kind == 'codex-cli' else '0',
            'RA2A_SANDBOX_DIAGNOSTIC_OUT': str(base/'receipts.jsonl'),
            'RA2A_SANDBOX_BUDGET_SOCKET': str(root/'budget.sock'),
            'RA2A_SANDBOX_CONTROL_ADDR': f'127.0.0.1:{port}',
            'RA2A_SANDBOX_SERVICE_NAME': '_ra2a-sandbox._udp',
            'RA2A_SANDBOX_BIND_ADDRESSES': '127.0.0.1',
            'RA2A_SANDBOX_ADVERTISE_ADDRESSES': '127.0.0.1',
            'PATH': '/usr/bin:/bin:/usr/sbin:/sbin'}

def wait_for_targets(port, deadline):
    last = None
    while time.monotonic() < deadline:
        try:
            last = http_json(port, '/v1/targets', timeout=3)
            if len(last.get('targets', [])) >= 3:
                return last
        except (OSError, ValueError):
            pass
        time.sleep(0.25)
    return last

def session_map(targets):
    sessions = {}
    for target in targets.get('targets', []):
        for item in target.get('sessions', []):
            sessions.setdefault(item.get('agent'), []).append(
                {'address': item['address'], 'node': target['id'], 'session': item})
    return sessions

def receipts(path):
    rows = []
    if path.is_file():
        for line in path.read_text().splitlines():
            try:
                rows.append(json.loads(line))
            except ValueError:
                continue
    return rows

def wait_receipt(path, message_id, deadline):
    while time.monotonic() < deadline:
        matches = [row for row in receipts(path)
                   if row.get('messageId') == message_id or row.get('marker') == message_id]
        if matches:
            return matches
        time.sleep(0.1)
    return []

def version_probe(profile, env, cwd, executable):
    result = subprocess.run(['sandbox-exec', '-p', profile, executable, '--version'],
                            env=env, cwd=cwd, capture_output=True, text=True, timeout=30)
    text = (result.stdout + result.stderr).strip().splitlines()
    return {'exit': result.returncode, 'version': text[0][:160] if text else '',
            'status': 'PASS' if result.returncode == 0 else 'FAIL'}

def version_major(text):
    match = re.match(r'^(?:opencode\s+)?v?([0-9]+)\.', str(text).strip())
    return int(match.group(1)) if match else None

def startup_probe(profile, env, cwd, executable, port, log_path):
    log = log_path.open('wb')
    process = subprocess.Popen(['sandbox-exec', '-p', profile, executable, 'serve',
                                '--port', str(port), '--hostname', '127.0.0.1'],
                               env=env, cwd=cwd, stdin=subprocess.DEVNULL,
                               stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    log.close()
    listening = False
    deadline = time.monotonic() + 12
    try:
        while time.monotonic() < deadline:
            if process.poll() is not None:
                break
            with socket.socket() as client:
                client.settimeout(0.5)
                if client.connect_ex(('127.0.0.1', port)) == 0:
                    listening = True
                    break
            time.sleep(0.2)
    finally:
        if process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGTERM)
                process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except OSError:
                    pass
                process.wait(timeout=5)
    tail = ''
    if log_path.is_file():
        lines = log_path.read_text(errors='replace').strip().splitlines()
        if lines:
            tail = lines[-1][:240]
            if any(word in tail.lower() for word in ('password', 'secret', 'token')):
                tail = '<redacted generated service output>'
        log_path.unlink(missing_ok=True)
    # A one-shot private server may generate a service password. Never retain
    # those ephemeral files when the preflight fails and keeps its root.
    for key in ('XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME'):
        base = Path(env.get(key, ''))/'opencode'
        if str(base) != 'opencode' and base.exists():
            shutil.rmtree(base, ignore_errors=True)
    for name in ('.config/opencode', '.local/share/opencode', '.local/state/opencode'):
        path = Path(env.get('HOME', ''))/name
        if path.exists():
            shutil.rmtree(path, ignore_errors=True)
    return {'status': 'PASS' if listening else 'FAIL', 'listening': listening,
            'exit': process.returncode, 'lastLogLine': tail, 'ephemeralStateRemoved': True}

def opencode_official_state(home):
    path = home/'.config/opencode/service.json'
    if not path.is_file():
        return {'status': 'INFO', 'present': False}
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError):
        return {'status': 'INFO', 'present': True, 'parseable': False}
    return {'status': 'INFO', 'present': True, 'parseable': True,
            'remoteConfigured': bool(data.get('remote')),
            'remoteAccepted': bool(data.get('remoteAccepted')),
            'passwordConfigured': bool(data.get('password'))}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path,
                        help='new report directory; existing paths rejected')
    options = parser.parse_args()
    output = options.output.resolve()
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    if sys.platform != 'darwin':
        raise SystemExit('this preflight is macOS-only')
    home = HOME
    before = protected(home)
    root = Path(tempfile.mkdtemp(prefix='r2s-mac-', dir='/private/tmp'))
    root.chmod(0o700)
    report = {'scope': 'macos-no-auth-preflight', 'credentialsImported': False,
              'paidInputs': 0, 'modelCalls': 0, 'matrixStatus': 'DRY_RUN_INCOMPLETE',
              'checks': {}, 'limitations': [
                  'macOS has no unprivileged network namespace; sandbox nodes share the host network.',
                  'mDNS uses the test-only _ra2a-sandbox._udp type and advertises 127.0.0.1; it still emits multicast on the host LAN.',
                  'The dry-run receivers are diagnostic adapters, not real clients; App private stdio remains BLOCKED_ADAPTER.',
                  'OpenCode v2 is rejected by RA2A ochost before server startup; the sandbox reports it as BLOCKED_ADAPTER.',
                  'The official ~/.config/opencode/service.json remote state is not projected into private homes, so remote-pairing conflicts are not reproduced.',
                  'sandbox-exec is process-level isolation, not a kernel network/mount namespace.']}
    source_commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=PROJECT, text=True).strip()
    source_dirty = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=PROJECT, text=True))
    report.update(sourceCommit=source_commit, sourceDirty=source_dirty)
    server = None
    ledger = None
    processes = []
    completed = False
    try:
        go = shutil.which('go')
        if not go:
            raise RuntimeError('go is unavailable')
        worker = root/'ra2a-sandbox-node'
        build_env = dict(os.environ, GOPROXY='off', GOTOOLCHAIN='local')
        build_env.pop('LD_PRELOAD', None)
        subprocess.run([go, 'build', '-o', str(worker), './cmd/ra2a-sandbox-node'],
                       cwd=PROJECT, env=build_env, check=True, timeout=180,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        report['checks']['workerBuild'] = {'status': 'PASS',
            'sha256': hashlib.sha256(worker.read_bytes()).hexdigest()}

        profile = sandbox_profile(root, home)
        probe_dir = root/'isolation'
        probe_dir.mkdir(mode=0o700)
        allowed = probe_dir/'allowed.txt'
        allow = subprocess.run(['sandbox-exec', '-p', profile, '/bin/sh', '-c',
                                f'printf ok > {quote(allowed)} && cat {quote(allowed)}'],
                               capture_output=True, text=True, timeout=10)
        deny_read = subprocess.run(['sandbox-exec', '-p', profile, '/bin/cat',
                                    str(home/'.codex/auth.json')],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        deny_write_path = home/'.codex'/('r2s-write-'+secrets.token_hex(4))
        deny_write = subprocess.run(['sandbox-exec', '-p', profile, '/usr/bin/touch',
                                     str(deny_write_path)],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        isolation_ok = (allow.returncode == 0 and allow.stdout.strip() == 'ok'
                        and deny_read.returncode != 0 and deny_write.returncode != 0
                        and not deny_write_path.exists())
        report['checks']['sandboxExec'] = {
            'status': 'PASS' if isolation_ok else 'FAIL',
            'privateWrite': allow.returncode == 0, 'officialReadDenied': deny_read.returncode != 0,
            'officialWriteDenied': deny_write.returncode != 0 and not deny_write_path.exists()}

        clients = {}
        for kind, executable in (('codex-cli', str(home/'.local/bin/codex')),
                                 ('opencode', str(home/'.opencode/bin/opencode')),
                                 ('pi', str(home/'.local/bin/pi'))):
            env = client_env(root, kind)
            try:
                probe = version_probe(profile, env, str(root/'nodes'/('client-'+kind)), executable)
                if kind == 'opencode':
                    probe['major'] = version_major(probe['version'])
                    probe['nativeStart'] = startup_probe(
                        profile, env, str(root/'nodes'/('client-'+kind)), executable,
                        free_port(), root/'opencode-serve.log')
                    if probe['major'] is not None and probe['major'] >= 2:
                        probe['status'] = 'BLOCKED_ADAPTER'
                        probe['reason'] = ('RA2A ochost rejects OpenCode v2 or later before '
                                           'starting the shared server; native opencode only works without --ra2a')
                    elif probe['nativeStart']['status'] != 'PASS':
                        probe['status'] = 'FAIL'
                clients[kind] = probe
            except (OSError, subprocess.TimeoutExpired) as error:
                clients[kind] = {'status': 'FAIL', 'error': type(error).__name__}
        statuses = [item.get('status') for item in clients.values()]
        probes_status = ('FAIL' if 'FAIL' in statuses else
                         'BLOCKED_ADAPTER' if 'BLOCKED_ADAPTER' in statuses else 'PASS')
        report['checks']['clientVersionProbes'] = {'status': probes_status, 'clients': clients}
        report['checks']['opencodeOfficialState'] = opencode_official_state(home)
        if probes_status == 'BLOCKED_ADAPTER':
            report['blockingAdapters'] = ['opencode']

        app_bundle = Path('/Applications/ChatGPT.app')
        app_native = app_bundle/'Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex'
        app = {'bundle': str(app_bundle), 'present': app_bundle.is_dir(), 'native': str(app_native),
               'realMatrix': 'BLOCKED_ADAPTER'}
        if app_native.is_file():
            env = client_env(root, 'codex-app')
            try:
                app['nativeVersion'] = version_probe(profile, env, str(root/'nodes'/'client-codex-app'), str(app_native))
            except (OSError, subprocess.TimeoutExpired) as error:
                app['nativeVersion'] = {'status': 'FAIL', 'error': type(error).__name__}
        app['status'] = 'PASS' if app['present'] and app.get('nativeVersion', {}).get('status') == 'PASS' else 'FAIL'
        report['checks']['app'] = app

        ledger = budget.Ledger(root/'ledger.jsonl')
        server = budget.serve(root/'budget.sock', ledger)
        node_ports = {}
        for kind, node_id in NODE_SPECS:
            port = free_port()
            node_ports[node_id] = port
            env = node_env(root, node_id, kind, node_id, port)
            log = (root/f'{node_id}.log').open('wb')
            process = subprocess.Popen(['sandbox-exec', '-p', profile, str(worker)], env=env,
                                       cwd=str(root/'nodes'/node_id), stdin=subprocess.DEVNULL,
                                       stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            log.close()
            processes.append((node_id, process))

        deadline = time.monotonic() + 30
        targets = wait_for_targets(node_ports['r2s-mac-codex'], deadline)
        matrix_deadline = time.monotonic() + 90
        if not targets or len(targets.get('targets', [])) < 3:
            raise RuntimeError('sandbox nodes did not discover three peers; see node logs')
        sessions = session_map(targets)
        if not all(kind in sessions for kind in KINDS):
            raise RuntimeError('sandbox targets did not publish all four Agent kinds')
        report['checks']['privateLAN'] = {
            'status': 'PASS', 'peerTargets': len(targets['targets']),
            'agents': sorted(sessions), 'service': '_ra2a-sandbox._udp',
            'addresses': '127.0.0.1'}
        report['checks']['discoveredSessions'] = {
            kind: [{'node': item['node'], 'address': item['address']} for item in rows]
            for kind, rows in sorted(sessions.items())}

        receipt_path = {node_id: root/'nodes'/node_id/'receipts.jsonl' for _, node_id in NODE_SPECS}
        cells = []
        for sender in KINDS:
            for receiver in KINDS:
                source, target = sessions[sender][0], sessions[receiver][0]
                message_id = 'R2S_DRY_'+uuid.uuid4().hex
                text = message_id+' '+sender+'->'+receiver
                cell = {'sender': sender, 'receiver': receiver, 'messageId': message_id,
                        'source': source['address'], 'target': target['address']}
                try:
                    http_json(node_ports[source['node']], '/v1/send', {
                        'to': target['address'], 'from': source['address'],
                        'messageId': message_id, 'text': text}, timeout=15)
                    found = wait_receipt(receipt_path[target['node']], message_id,
                                         min(matrix_deadline, time.monotonic()+10))
                    cell['receipts'] = len(found)
                    cell['status'] = 'PASS' if len(found) == 1 and found[0].get('marker') == message_id else 'FAIL'
                except (OSError, ValueError, urllib.error.HTTPError) as error:
                    cell.update(status='FAIL', error=type(error).__name__)
                cells.append(cell)
        passed = sum(cell['status'] == 'PASS' for cell in cells)
        report['matrixStatus'] = 'DRY_RUN_PASS' if passed == len(cells) else 'DRY_RUN_INCOMPLETE'
        report['checks']['matrixDryRun'] = {
            'status': 'PASS' if passed == len(cells) else 'FAIL',
            'passed': passed, 'total': len(cells), 'cells': cells}

        duplicate = next(cell for cell in cells if cell['sender'] == 'codex-cli' and cell['receiver'] == 'codex-app')
        duplicate_source_node = duplicate['source'].split('/')[2]
        duplicate_target_node = duplicate['target'].split('/')[2]
        http_json(node_ports[duplicate_source_node], '/v1/send', {
            'to': duplicate['target'], 'from': duplicate['source'],
            'messageId': duplicate['messageId'], 'text': 'R2S_DRY_DUPLICATE'}, timeout=15)
        after_duplicate = [row for row in receipts(receipt_path[duplicate_target_node])
                           if row.get('messageId') == duplicate['messageId']
                           or row.get('marker') == duplicate['messageId']]
        report['checks']['duplicate'] = {
            'status': 'PASS' if len(after_duplicate) == 1 else 'FAIL',
            'receipts': len(after_duplicate)}

        budget_checks = {}
        for kind in KINDS:
            used = ledger.summary()[kind]['reserved']
            for index in range(used, 10):
                ledger.call('/reserve', {'kind': kind, 'id': f'budget-fill-{kind}-{index}'})
            blocked = ledger.call('/reserve', {'kind': kind, 'id': f'budget-over-{kind}'})
            budget_checks[kind] = {'used': ledger.summary()[kind]['reserved'],
                                  'eleventhAllowed': bool(blocked.get('allow'))}
        report['checks']['budget'] = {
            'status': 'PASS' if all(not item['eleventhAllowed'] for item in budget_checks.values()) else 'FAIL',
            'buckets': budget_checks, 'summary': ledger.summary()}

        relay_server = relay.host_server(root/'model.sock', {'example.com'})
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
                client.settimeout(5)
                client.connect(str(root/'model.sock'))
                client.sendall(b'CONNECT evil.invalid:443 HTTP/1.1\r\nHost: evil.invalid:443\r\n\r\n')
                reply = client.recv(200)
            report['checks']['modelRelayDeny'] = {
                'status': 'PASS' if reply.startswith(b'HTTP/1.1 403') else 'FAIL',
                'denied': reply.startswith(b'HTTP/1.1 403')}
        finally:
            relay_server.shutdown()
            relay_server.server_close()

        after = protected(home)
        report['officialAuthConfigUnchanged'] = before == after
        completed = True
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, urllib.error.URLError) as error:
        report['errorClass'] = type(error).__name__
        report['error'] = str(error)[:300]
    finally:
        for _, process in processes:
            if process.poll() is None:
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                    process.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except OSError:
                        pass
                    process.wait(timeout=5)
        if server:
            server.shutdown()
            server.server_close()
        if ledger:
            ledger.close()
        for name in ('ledger.jsonl',):
            if (root/name).is_file():
                shutil.copyfile(root/name, output/name)
        for path in sorted(root.glob('*.log')):
            shutil.copyfile(path, output/path.name)
        for path in sorted((root/'nodes').glob('*/receipts.jsonl')):
            shutil.copyfile(path, output/(path.parent.name+'-receipts.jsonl'))
        required = [report['checks'].get(name, {}).get('status') == 'PASS'
                    for name in ('workerBuild', 'sandboxExec', 'privateLAN', 'matrixDryRun',
                                 'duplicate', 'budget', 'modelRelayDeny')]
        required += [report['checks'].get('clientVersionProbes', {}).get('status') == 'PASS',
                     report['checks'].get('app', {}).get('status') == 'PASS']
        report['preflightStatus'] = ('PASS_WITH_LIMITATIONS' if completed and all(required)
                                     and report.get('officialAuthConfigUnchanged') else 'FAIL')
        if report['preflightStatus'] == 'PASS_WITH_LIMITATIONS':
            shutil.rmtree(root, ignore_errors=True)
            report['credentialCopies'] = 'NONE'
        else:
            report['credentialCopies'] = 'NONE'
            report['privateRecoveryDirectory'] = str(root)
        report['sourceChangedDuringRun'] = subprocess.check_output(
            ['git', 'rev-parse', 'HEAD'], cwd=PROJECT, text=True).strip() != source_commit
        (output/'summary.json').write_text(json.dumps(report, indent=2, ensure_ascii=False))
        rows = ['# macOS 四端矩阵沙盒预检', '',
                '范围：无凭据导入、无模型调用；4x4 为真实 LAN 的诊断接收器空跑，不代表真实客户端验收。', '',
                '状态：'+report['preflightStatus'], '',
                '| 检查 | 状态 |', '| --- | --- |']
        for name, check in report['checks'].items():
            if isinstance(check, dict):
                rows.append(f'| {name} | {check.get("status", "INFO")} |')
        rows += ['', f'- 矩阵空跑：{report.get("matrixStatus")}',
                 f'- 正式认证/配置终点哈希不变：{report.get("officialAuthConfigUnchanged")}',
                 f'- 阻塞适配器：{", ".join(report.get("blockingAdapters", [])) or "无"}',
                 '- App 真实私有实例与真实 4 端付费矩阵仍未执行，继续标 BLOCKED_ADAPTER。', '',
                 '限制：']
        rows += ['- '+item for item in report['limitations']]
        (output/'report.md').write_text('\n'.join(rows)+'\n')
    print(json.dumps({'preflightStatus': report['preflightStatus'],
                      'matrixStatus': report['matrixStatus'],
                      'paidInputs': 0, 'report': str(output/'report.md')}))
    return 0 if report['preflightStatus'] == 'PASS_WITH_LIMITATIONS' else 1

if __name__ == '__main__':
    raise SystemExit(main())
