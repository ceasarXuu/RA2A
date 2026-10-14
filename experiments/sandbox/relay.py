"""Allowlisted HTTPS-only host relay and isolated loopback/UDS broker."""
import ipaddress
import os
import select
import socket
import socketserver
import sys
import threading
from urllib.parse import urlsplit


def pump(left, right):
    while True:
        ready, _, _ = select.select([left, right], [], [], 120)
        if not ready:
            return
        for source in ready:
            data = source.recv(65536)
            if not data:
                return
            (right if source is left else left).sendall(data)


class UnixServer(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


def host_server(path, domains, upstream=''):
    allowed = set(domains)
    class Handler(socketserver.StreamRequestHandler):
        def handle(self):
            stage = 'validation'
            try:
                self.request.settimeout(15)
                line = self.rfile.readline(4097)
                method, address, version = line.decode('ascii').strip().split()
                target = urlsplit('https://'+address)
                if method != 'CONNECT' or version != 'HTTP/1.1' or target.hostname not in allowed or target.port != 443:
                    raise ValueError('HTTPS destination denied')
                if target.username or target.password or target.path:
                    raise ValueError('invalid destination')
                size = len(line)
                while True:
                    header = self.rfile.readline(4097)
                    size += len(header)
                    if size > 16384 or not header:
                        raise ValueError('invalid headers')
                    if header == b'\r\n':
                        break
                proxy = urlsplit(upstream) if upstream else None
                if proxy and (proxy.scheme != 'http' or proxy.username or proxy.password):
                    raise ValueError('unsupported upstream proxy')
                if proxy:
                    # Explicit trusted upstream resolves provider DNS; host DNS is
                    # not proof of the upstream destination. Only allowlist:443.
                    destination = (proxy.hostname, proxy.port or 80)
                else:
                    stage = 'dns'
                    addresses = socket.getaddrinfo(target.hostname, 443, type=socket.SOCK_STREAM)
                    public = [item[4][0] for item in addresses if ipaddress.ip_address(item[4][0]).is_global]
                    if not public:
                        raise ValueError('non-public destination refused')
                    destination = (public[0], 443)
                stage = 'upstream'
                with socket.create_connection(destination, timeout=15) as connection:
                    if proxy:
                        connection.sendall(f'CONNECT {address} HTTP/1.1\r\nHost: {address}\r\n\r\n'.encode())
                        response = b''
                        while b'\r\n\r\n' not in response and len(response) <= 16384:
                            chunk = connection.recv(1)
                            if not chunk:
                                break
                            response += chunk
                        if not response.startswith(b'HTTP/1.1 200') and not response.startswith(b'HTTP/1.0 200'):
                            raise ValueError('upstream CONNECT rejected')
                    self.server.metrics['tunnels'] += 1
                    stage = 'tunnel'
                    self.request.sendall(b'HTTP/1.1 200 Connection Established\r\n\r\n')
                    pump(self.request, connection)
            except (OSError, ValueError, UnicodeError):
                self.server.metrics[stage] = self.server.metrics.get(stage, 0)+1
                # No destination/header/token or arbitrary exception in logs.
                try:
                    self.request.sendall(b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n')
                except OSError:
                    pass
    server = UnixServer(str(path), Handler)
    server.metrics = {'tunnels': 0}
    os.chmod(path, 0o600)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def broker(path, port=7890):
    class Handler(socketserver.BaseRequestHandler):
        def handle(self):
            with socket.socket(socket.AF_UNIX) as connection:
                connection.connect(path)
                pump(self.request, connection)
    class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
        allow_reuse_address = True
        daemon_threads = True
    with Server(('127.0.0.1', port), Handler) as server:
        server.serve_forever()


if __name__ == '__main__':
    # Broker is invoked only through Runtime.spawn in a private node network.
    broker(sys.argv[1])
