"""No-model budget acceptance tests, including the actual private UDS transport."""
import concurrent.futures
import importlib.util
import json
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('budget', Path(__file__).with_name('budget.py'))
budget = importlib.util.module_from_spec(spec)
spec.loader.exec_module(budget)


class BudgetTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='r2-budget-')
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'ledger.jsonl'
        self.ledger = budget.Ledger(self.path)
        self.addCleanup(lambda: self.ledger.close() if not self.ledger.file.closed else None)

    def call(self, kind, identity, **extra):
        return self.ledger.call('/reserve', {'kind': kind, 'id': identity, **extra})

    def test_per_kind_shared_across_sessions(self):
        for kind in ('codex-cli', 'opencode', 'pi'):
            for index in range(10):
                result = self.call(kind, f'input-{index}', session=f'session-{index % 3}')
                self.assertTrue(result['allow'])
            blocked = self.call(kind, 'eleventh', session='new-session')
            self.assertFalse(blocked['allow'])
            self.assertIn('10/10', blocked['error'])
            self.assertEqual(self.ledger.summary()[kind]['reserved'], 10)
        self.assertEqual(self.ledger.summary()['codex-app']['reserved'], 0)

    def test_twenty_concurrent_unique_inputs_only_ten_allowed(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=20) as pool:
            results = list(pool.map(lambda n: self.call('pi', str(n)), range(20)))
        self.assertEqual(sum(result['allow'] for result in results), 10)
        self.assertEqual(len(self.path.read_text().splitlines()), 10)

    def test_twenty_concurrent_same_identity_reserves_once(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=20) as pool:
            results = list(pool.map(lambda _: self.call('opencode', 'same'), range(20)))
        self.assertTrue(all(result['allow'] for result in results))
        self.assertEqual(sum(not result['duplicate'] for result in results), 1)
        self.assertEqual(self.ledger.summary()['opencode']['reserved'], 1)
        self.assertEqual(len(self.path.read_text().splitlines()), 1)

    def test_reopen_retains_budget_and_prior_receipt(self):
        for index in range(10):
            self.call('codex-cli', str(index))
        self.ledger.call('/finish', {'kind': 'codex-cli', 'id': '0',
                                    'result': {'code': 'delivered', 'turnId': 'turn-a'}})
        self.ledger.close()
        reopened = budget.Ledger(self.path)
        self.addCleanup(reopened.close)
        self.assertFalse(reopened.call('/reserve', {'kind': 'codex-cli', 'id': 'new'})['allow'])
        prior = reopened.call('/reserve', {'kind': 'codex-cli', 'id': '0'})
        self.assertTrue(prior['duplicate'])
        self.assertEqual(prior['result'], {'code': 'delivered', 'turnId': 'turn-a'})
        pending = reopened.call('/reserve', {'kind': 'codex-cli', 'id': '1'})
        self.assertTrue(pending['duplicate'])
        self.assertIsNone(pending['result'])  # Go delivery wrapper must treat this as unknown.

    def test_corrupt_json_rejected_on_reopen(self):
        self.ledger.close()
        self.path.write_text('{broken json\n')
        with self.assertRaises(ValueError):
            budget.Ledger(self.path)

    def test_structurally_corrupt_ledger_rejected_on_reopen(self):
        self.ledger.close()
        self.path.write_text(json.dumps({'kind': 'unrecognized', 'id': 'a', 'state': 'reserved'}) + '\n')
        with self.assertRaises(ValueError):
            candidate = budget.Ledger(self.path)
            candidate.close()

    def test_fsync_failure_permanently_fails_closed(self):
        with patch.object(budget.os, 'fsync', side_effect=OSError('injected disk failure')):
            with self.assertRaises(OSError):
                self.call('pi', 'paid-slot')
        for kind in budget.KINDS:
            with self.assertRaises(ValueError):
                self.call(kind, 'new')
        with self.assertRaises(ValueError):
            self.ledger.call('/finish', {'kind': 'pi', 'id': 'paid-slot', 'result': {'code': 'failed'}})
        self.assertIn('paid-slot', self.path.read_text())

    def test_sensitive_finish_body_is_not_persisted(self):
        self.call('pi', 'one')
        self.ledger.call('/finish', {'kind': 'pi', 'id': 'one', 'text': 'secret-body',
                                    'auth': 'secret-auth', 'result': {'code': 'delivered',
                                    'text': 'secret-model-output', 'token': 'secret-token'}})
        raw = self.path.read_text()
        self.assertNotIn('secret-', raw)
        self.assertEqual(self.call('pi', 'one')['result'], {'code': 'delivered'})

    def test_uds_real_http_receipt_and_permissions(self):
        socket_path = Path(self.tmp.name) / 'budget.sock'
        server = budget.serve(socket_path, self.ledger)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.assertEqual(socket_path.stat().st_mode & 0o777, 0o600)
        payload = json.dumps({'kind': 'pi', 'id': 'uds-one'}).encode()
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
            client.settimeout(3)
            client.connect(str(socket_path))
            client.sendall(b'POST /reserve HTTP/1.0\r\nContent-Length: ' + str(len(payload)).encode() +
                           b'\r\nContent-Type: application/json\r\n\r\n' + payload)
            chunks = []
            while True:
                part = client.recv(4096)
                if not part:
                    break
                chunks.append(part)
        headers, body = b''.join(chunks).split(b'\r\n\r\n', 1)
        self.assertIn(b'200', headers.split(b'\r\n', 1)[0])
        self.assertEqual(json.loads(body), {'allow': True, 'duplicate': False})
        self.assertEqual(self.ledger.summary()['pi']['reserved'], 1)


if __name__ == '__main__':
    unittest.main()
