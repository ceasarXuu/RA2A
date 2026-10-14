"""Exercise private relay HTTP without credentials or external connections."""
import importlib.util
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import MagicMock, patch

spec = importlib.util.spec_from_file_location('relay', Path(__file__).with_name('relay.py'))
relay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(relay)


class RelayTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='r2-relay-')
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'relay.sock'
        self.server = relay.host_server(self.path, ['api.example.test'])
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def request(self, address, method='CONNECT'):
        with socket.socket(socket.AF_UNIX) as client:
            client.settimeout(3)
            client.connect(str(self.path))
            client.sendall(f'{method} {address} HTTP/1.1\r\nHost: ignored\r\n\r\n'.encode())
            return client.recv(4096)

    def test_non_allowlisted_destination_never_resolves_or_connects(self):
        with patch.object(relay.socket, 'getaddrinfo') as resolve, patch.object(relay.socket, 'create_connection') as connect:
            self.assertIn(b'403', self.request('denied.example.test:443'))
            resolve.assert_not_called()
            connect.assert_not_called()
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)

    def test_non443_and_nonconnect_rejected_before_network(self):
        with patch.object(relay.socket, 'getaddrinfo') as resolve, patch.object(relay.socket, 'create_connection') as connect:
            for address, method in [('api.example.test:80', 'CONNECT'),
                                    ('api.example.test:443', 'GET'),
                                    ('api.example.test:444', 'CONNECT')]:
                self.assertIn(b'403', self.request(address, method))
            resolve.assert_not_called()
            connect.assert_not_called()

    def test_private_only_dns_refused(self):
        with patch.object(relay.socket, 'create_connection') as connect:
            for addresses in [['127.0.0.1'], ['10.1.2.3'], ['::1']]:
                records = [(socket.AF_INET6 if ':' in address else socket.AF_INET,
                            socket.SOCK_STREAM, 6, '', (address, 443)) for address in addresses]
                with patch.object(relay.socket, 'getaddrinfo', return_value=records):
                    self.assertIn(b'403', self.request('api.example.test:443'))
            connect.assert_not_called()

    def test_destination_userinfo_path_and_secret_are_denied(self):
        with patch.object(relay.socket, 'create_connection') as connect:
            for address in ['user:secret@api.example.test:443', 'api.example.test:443/private']:
                result = self.request(address)
                self.assertIn(b'403', result)
                self.assertNotIn(b'secret', result)
            connect.assert_not_called()

    def test_direct_connection_pins_prevalidated_public_address(self):
        records = [(socket.AF_INET, socket.SOCK_STREAM, 6, '', (address, 443))
                   for address in ('192.168.1.1', '8.8.8.8')]
        # No real TCP connection: the fixture inspects the selected destination.
        with patch.object(relay.socket, 'getaddrinfo', return_value=records), \
             patch.object(relay.socket, 'create_connection', return_value=MagicMock()) as connect, \
             patch.object(relay, 'pump'):
            self.assertIn(b'200', self.request('api.example.test:443'))
            self.assertEqual(connect.call_args.args[0], ('8.8.8.8', 443),
                             'resolving the hostname again permits DNS rebinding after validation')


if __name__ == '__main__':
    unittest.main()
