"""One-command, no-credential Linux sandbox discovery; not a paid matrix."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


PROJECT = Path(__file__).resolve().parents[2]


def run(argv, env, output, timeout=60):
    with output.open('w') as log:
        result = subprocess.run(argv, cwd=PROJECT, env=env, stdout=log,
                                stderr=subprocess.STDOUT, timeout=timeout)
    return result.returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path,
                        help='new private evidence directory; existing paths rejected')
    parser.add_argument('--app', action='store_true', help='also observe real App without auth/network')
    options = parser.parse_args()
    root = options.output.resolve()
    root.mkdir(mode=0o700, parents=True, exist_ok=False)
    # The host display bridge alone needs the original DISPLAY/XAUTHORITY.
    # Never pass this environment to Agents: display_probe clears it at bwrap.
    env = os.environ.copy()
    env.pop('LD_PRELOAD', None)
    env['GOPROXY'] = 'off'
    env['GOTOOLCHAIN'] = 'local'
    source = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=PROJECT, env=env,
                            capture_output=True, text=True, check=True).stdout.strip()
    dirty = subprocess.run(['git', 'status', '--porcelain'], cwd=PROJECT, env=env,
                           capture_output=True, text=True, check=True).stdout != ''
    report = {'sourceCommit': source, 'sourceDirty': dirty, 'scope': 'no-auth-preflight', 'credentialsImported': False,
              'paidInputs': 0, 'matrixStatus': 'NOT_RUN', 'checks': {}}
    tools = ['unshare', 'nsenter', 'ip', 'bwrap', 'Xephyr', 'xauth',
             'xdpyinfo', 'dbus-run-session', 'python3']
    if options.app:
        tools += ['xwininfo', 'import-im6.q16']
    go = shutil.which('go') or str(Path.home()/'sdk/go/bin/go')
    missing = [tool for tool in tools if shutil.which(tool) is None]
    if not Path(go).is_file():
        missing.append('go')
    report['missingDependencies'] = missing
    try:
        if missing:
            raise RuntimeError('missing existing dependencies: '+', '.join(missing))
        binary = root/'netprobe'
        if run([go, 'build', '-o', str(binary), './experiments/sandbox'],
               env, root/'build.log'):
            raise RuntimeError('network probe build failed; see build.log')
        report['networkProbeSHA256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
        env['RA2A_DISCOVERY_HOST_NETNS'] = os.readlink('/proc/self/ns/net')
        code = run(['unshare', '--user', '--map-root-user', '--net', 'python3',
                    'experiments/sandbox/network_probe.py', str(binary)],
                   env, root/'network.log')
        report['checks']['privateThreeNodeLAN'] = {'exit': code, 'status': 'PASS' if code == 0 else 'FAIL'}
        if code:
            raise RuntimeError('private LAN probe failed; see network.log')
        code = run(['python3', 'experiments/sandbox/display_probe.py', str(root/'display')],
                   env, root/'display.log')
        report['checks']['privateDisplayAndDBus'] = {'exit': code, 'status': 'PASS' if code == 0 else 'FAIL'}
        if code:
            raise RuntimeError('private display probe failed; see display.log')
        if options.app:
            code = run(['python3', 'experiments/sandbox/display_probe.py', str(root/'app'), '--app'],
                       env, root/'app.log')
            raw = json.loads((root/'app/result.json').read_text())
            report['checks']['realAppObservation'] = {
                'exit': code, 'windowObserved': raw.get('windowObserved', False),
                'backendIPCObserved': raw.get('backendIPCObserved', False),
                'status': 'NOT_RUN_NO_CREDENTIALS_OR_NETWORK',
                'resourcesReaped': raw.get('clientReaped', False) and raw.get('serverReaped', False),
                'evidence': 'app/result.json'}
            if code or not raw.get('windowObserved'):
                raise RuntimeError('real App window not observed; see app/result.json')
        report['preflightStatus'] = 'PASS_WITH_LIMITATIONS'
    except (OSError, RuntimeError, subprocess.TimeoutExpired, ValueError) as error:
        report['preflightStatus'] = 'FAIL'
        report['error'] = str(error)
    finally:
        (root/'summary.json').write_text(json.dumps(report, indent=2))
        rows = ['# Linux 沙盒预检查', '', '范围：无凭据、无模型调用；不是完整矩阵。', '',
                '状态：'+report.get('preflightStatus', 'FAIL'), '']
        for name, check in report['checks'].items():
            rows.append('- '+name+'：'+check['status'])
        if report.get('error'):
            rows += ['', '停止原因：'+report['error']]
        rows += ['', '尚未执行：凭据复用、模型出口、接收预算、真实四类 Agent 矩阵与人工继续输入。',
                 'App 窗口存活不等于已认证、backend 就绪或完成 UI 验收。']
        (root/'report.md').write_text('\n'.join(rows)+'\n')
    print(json.dumps({'preflightStatus': report['preflightStatus'], 'report': str(root/'report.md'),
                      'matrixStatus': 'NOT_RUN', 'paidInputs': 0}))
    return 0 if report['preflightStatus'] == 'PASS_WITH_LIMITATIONS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
