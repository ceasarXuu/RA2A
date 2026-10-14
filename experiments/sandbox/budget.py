"""Trusted per-run ledger. Only worker mount namespaces receive its socket."""
import json
import os
from pathlib import Path
import socketserver
import threading
from http.server import BaseHTTPRequestHandler


KINDS = ('codex-cli', 'codex-app', 'opencode', 'pi')
CODES = ('delivered', 'busy', 'not_found', 'unreachable', 'unsupported', 'start_required', 'unknown')

def validate(body):
    if not isinstance(body, dict) or body.get('kind') not in KINDS:
        raise ValueError('invalid budget kind')
    identity = body.get('id')
    if not isinstance(identity, str) or not 0 < len(identity) <= 128:
        raise ValueError('invalid budget identity')

def outcome(result):
    if not isinstance(result, dict) or result.get('code') not in CODES:
        raise ValueError('invalid receipt outcome')
    if 'turnId' in result and (not isinstance(result['turnId'], str) or len(result['turnId']) > 128):
        raise ValueError('invalid turn identity')
    return {k: result[k] for k in ('code', 'turnId') if k in result}


class Ledger:
    def __init__(self, path, limit=10):
        self.path, self.limit = Path(path), limit
        self.lock, self.records = threading.Lock(), {}
        self.failed = False
        if self.path.exists():
            if self.path.is_symlink():
                raise ValueError('ledger symlink rejected')
            for line in self.path.read_text().splitlines():
                entry = json.loads(line)
                validate(entry)
                key = (entry['kind'], entry['id'])
                if entry.get('state') not in ('reserved', 'finished'):
                    raise ValueError('invalid ledger state')
                if key in self.records and self.records[key]['state'] == 'finished':
                    raise ValueError('ledger state regression')
                if entry['state'] == 'finished':
                    entry['result'] = outcome(entry.get('result'))
                self.records[key] = entry
        self.file = os.fdopen(os.open(self.path, os.O_WRONLY | os.O_APPEND | os.O_CREAT |
                                     os.O_NOFOLLOW, 0o600), 'a')

    def write(self, entry):
        try:
            self.file.write(json.dumps(entry)+'\n')
            self.file.flush()
            os.fsync(self.file.fileno())
        except OSError:
            self.failed = True
            raise
        self.records[(entry['kind'], entry['id'])] = entry

    def call(self, route, body):
        validate(body)
        kind, identity = body['kind'], body['id']
        with self.lock:
            if self.failed:
                raise ValueError('ledger unavailable; paid input stopped')
            key = (kind, identity)
            if route == '/reserve':
                if key in self.records:
                    return {'allow': True, 'duplicate': True,
                            'result': self.records[key].get('result')}
                used = sum(k == kind for k, _ in self.records)
                if used >= self.limit:
                    return {'allow': False, 'duplicate': False,
                            'error': f'{kind} 本轮接收预算已用完：{used}/{self.limit}；输入已拦截，未送入模型'}
                self.write({'kind': kind, 'id': identity, 'state': 'reserved'})
                return {'allow': True, 'duplicate': False}
            if route != '/finish' or key not in self.records:
                raise ValueError('reservation required')
            result = outcome(body.get('result'))
            if self.records[key]['state'] == 'finished':
                return {'ok': True}
            # Never persist text, exception strings, credentials or model output.
            self.write({**self.records[key], 'state': 'finished',
                        'result': {k: result[k] for k in ('code', 'turnId') if k in result}})
            return {'ok': True}

    def summary(self):
        with self.lock:
            return {kind: {'reserved': sum(k == kind for k, _ in self.records),
                           'limit': self.limit} for kind in KINDS}

    def close(self):
        self.file.close()


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


def serve(socket_path, ledger):
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            try:
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 < length <= 4096:
                    raise ValueError('invalid request size')
                value = ledger.call(self.path, json.loads(self.rfile.read(length)))
                code = 200
            except (ValueError, OSError, KeyError):
                value, code = {'allow': False, 'error': 'budget unavailable; no paid input allowed'}, 503
            raw = json.dumps(value).encode()
            self.send_response(code)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def log_message(self, *_):
            pass
    server = Server(str(socket_path), Handler)
    os.chmod(socket_path, 0o600)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server
