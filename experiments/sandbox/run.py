"""Run the Linux real-client RA2A test suite with ten receptions per Agent kind."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
from runtime import supervise

import profiles
import relay


PROJECT = Path(__file__).resolve().parents[2]


def provenance():
    files = subprocess.check_output(['git', 'ls-files', '-z', '*.go', 'go.mod', 'go.sum',
                                     'experiments/sandbox/*.py', '*.mjs'], cwd=PROJECT).split(b'\0')
    digest = hashlib.sha256()
    for name in sorted(n for n in files if n):
        digest.update(name+b'\0'+(PROJECT/os.fsdecode(name)).read_bytes())
    return {'sourceCommit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=PROJECT, text=True).strip(),
            'sourceDirty': bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=PROJECT)),
            'sourceInputSHA256': digest.hexdigest()}


def protected(home):
    names = ['.codex/auth.json', '.codex/config.toml', '.config/ra2a/config.json',
             '.config/opencode/opencode.json', '.local/share/opencode/auth.json',
             '.pi/agent/auth.json', '.pi/agent/settings.json', '.pi/agent/models-store.json']
    return {name: hashlib.sha256((home/name).read_bytes()).hexdigest()
            for name in names if (home/name).is_file()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, help='fresh persistent report directory')
    parser.add_argument('--manual', action='store_true', help='budgeted terminal observation after automation')
    parser.add_argument('--inner', type=Path, help=argparse.SUPPRESS)
    options = parser.parse_args()
    if options.inner:
        import runner
        runner.execute(options.inner)
        return 0
    if not options.output:
        parser.error('--output is required')
    output = options.output.resolve()
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    home = Path.home()
    before = protected(home)
    source = provenance()
    report = {'matrixStatus': 'NOT_RUN', 'errorClass': None}
    # This temporary tree alone contains credentials; never copy it into reports.
    temporary = tempfile.mkdtemp(prefix='r2s-')
    if True:
        root = Path(temporary)
        server = None
        try:
            prepared = profiles.prepare(root/'profiles', ['codex', 'opencode', 'pi'], home)
            bins = prepared['bins']
            env = os.environ.copy()
            env.pop('LD_PRELOAD', None)
            env.update(GOPROXY='off', GOTOOLCHAIN='local')
            go = shutil.which('go') or str(home/'sdk/go/bin/go')
            for name, package in [('worker', 'ra2a-sandbox-node'), ('ra2a', 'ra2a'), ('ocwrapper', 'oc-wrapper')]:
                target = root/name
                subprocess.run([go, 'build', '-o', str(target), './cmd/'+package], cwd=PROJECT,
                               env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                bins[name] = str(target)
            domains = {'models.dev'}
            for choice in prepared['manifest'].values():
                if isinstance(choice, dict):
                    domains.update(choice.get('hosts', []))
            upstream = os.getenv('HTTPS_PROXY') or os.getenv('https_proxy') or ''
            server = relay.host_server(root/'model.sock', domains, upstream)
            prepared['manifest']['binaryHashes'] = {k: hashlib.sha256(Path(v).read_bytes()).hexdigest()
                for k, v in bins.items() if Path(v).is_file()}
            run_id = secrets.token_hex(4)
            config = {**prepared, 'runId': run_id, 'pin': secrets.token_hex(3).upper(),
                      'hostNetns': os.readlink('/proc/self/ns/net'), 'source': str(PROJECT), 'manual': options.manual}
            private = root/'private.json'
            with private.open('x') as stream:
                os.fchmod(stream.fileno(), 0o600)
                json.dump(config, stream)
            with (root/'controller.stdout').open('xb') as out, (root/'controller.stderr').open('xb') as err:
                owned = supervise(['unshare', '--user', '--map-root-user', '--net', '--pid', '--fork', '--mount-proc',
                            sys.executable, str(Path(__file__).resolve()), '--inner', str(root)],
                           env={'PATH': '/usr/sbin:/usr/bin:/bin', 'PYTHONDONTWRITEBYTECODE': '1'},
                           timeout=1800, stdout=None if options.manual else out, stderr=err,
                           stdin=None if options.manual else subprocess.DEVNULL)
            if owned['timed_out'] or owned['returncode']:
                raise RuntimeError('owned sandbox supervisor failed')
            report = json.loads((root/'summary.json').read_text())
            for name in ('ledger.jsonl', 'report.md'):
                if (root/name).is_file():
                    shutil.copyfile(root/name, output/name)
        except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as error:
            report.update(errorClass=type(error).__name__, matrixStatus='NOT_RUN')
        finally:
            if server:
                server.shutdown()
                server.server_close()
            if (root/'ledger.jsonl').is_file():
                shutil.copyfile(root/'ledger.jsonl', output/'ledger.jsonl')
                if 'budget' not in report:
                    import budget
                    recovery = budget.Ledger(root/'ledger.jsonl')
                    report['budget'] = recovery.summary(); recovery.close()
            report['modelRelay'] = dict(server.metrics) if server else {}
            report['officialAuthConfigUnchanged'] = protected(home) == before
            report['credentialCopies'] = 'PRESERVED_PRIVATE_ON_INCOMPLETE_CLEANUP'
            if report.get('ownedResources') == 'REAPED':
                shutil.rmtree(root)
                report['credentialCopies'] = 'REMOVED_WITH_OWNED_TEMP_TREE'
            else:
                report['privateRecoveryDirectory'] = str(root)
            report.update(source)
            report['sourceChangedDuringRun'] = provenance()['sourceInputSHA256'] != source['sourceInputSHA256']
    (output/'summary.json').write_text(json.dumps(report, indent=2))
    if not (output/'report.md').exists():
        (output/'report.md').write_text('# Linux矩阵套件\n\n状态：'+report['matrixStatus']+'\n错误类别：'+str(report['errorClass'])+'\n')
    print(json.dumps({'matrixStatus': report['matrixStatus'], 'report': str(output/'report.md'),
                      'officialAuthConfigUnchanged': report['officialAuthConfigUnchanged']}))
    return 0 if report['matrixStatus'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
