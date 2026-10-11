"""Diagnostic only. Must start through unshare -Urn; never changes host network."""
import json
import os
from pathlib import Path
import subprocess
import sys


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def main():
    own = os.readlink('/proc/self/ns/net')
    host = os.environ.get('RA2A_DISCOVERY_HOST_NETNS')
    if not host or own == host or os.getuid() != 0:
        raise RuntimeError('requires a private user+network namespace')
    binary = str(Path(sys.argv[1]).resolve())
    children, probes = [], []
    try:
        run('ip', 'link', 'add', 'probe-br', 'type', 'bridge')
        run('ip', 'link', 'set', 'probe-br', 'up')
        for index in range(1, 4):
            child = subprocess.Popen(['unshare', '--net', 'sleep', '40'])
            children.append(child)
            # Wait for the child to actually enter its distinct network view.
            import time
            deadline = time.monotonic() + 2
            while os.readlink(f'/proc/{child.pid}/ns/net') == own:
                if time.monotonic() > deadline:
                    raise RuntimeError('child namespace not ready')
                time.sleep(.01)
            run('ip', 'link', 'add', f'v{index}', 'type', 'veth', 'peer', 'name', f'p{index}')
            run('ip', 'link', 'set', f'p{index}', 'netns', str(child.pid))
            run('ip', 'link', 'set', f'v{index}', 'master', 'probe-br')
            run('ip', 'link', 'set', f'v{index}', 'up')
            prefix = ['nsenter', '-t', str(child.pid), '-n']
            run(*prefix, 'ip', 'link', 'set', 'lo', 'up')
            run(*prefix, 'ip', 'addr', 'add', f'198.18.0.{index}/24', 'dev', f'p{index}')
            run(*prefix, 'ip', 'link', 'set', f'p{index}', 'up')
            routes = run(*prefix, 'ip', 'route')
            if 'default' in routes:
                raise RuntimeError('unexpected external route')
            env = dict(os.environ, RA2A_DISCOVERY_NETNS=os.readlink(f'/proc/{child.pid}/ns/net'))
            env.pop('LD_PRELOAD', None)
            probes.append(subprocess.Popen([*prefix, binary, str(index)], env=env,
                                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True))
        results = []
        for process in probes:
            out, err = process.communicate(timeout=25)
            results.append({'exit': process.returncode, 'stdout': out, 'stderr': err})
        print(json.dumps({'outer_netns': own, 'host_netns': host, 'results': results}))
        if any(r['exit'] != 0 for r in results):
            raise RuntimeError('a private LAN probe failed')
    finally:
        for process in probes + children:
            if process.poll() is None:
                process.terminate()
            process.wait(timeout=5)


if __name__ == '__main__':
    main()
