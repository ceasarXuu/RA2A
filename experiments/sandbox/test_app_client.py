import json
from pathlib import Path
import tempfile
import unittest

from app_client import AppProcess, auth_copy
from budget import Ledger


class Owned:
    def __init__(self):
        self.stops = 0
    def stop(self):
        self.stops += 1
    def poll(self):
        return None


class AppInputTest(unittest.TestCase):
    def test_authorization_copy_withdrawal_preserves_host_and_restart(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            original = root/'host.auth'
            original.write_bytes(b'disposable display authorization')
            first, second = root/'first', root/'second'
            first.mkdir(mode=0o700)
            second.mkdir(mode=0o700)
            copied = auth_copy(first, original)
            self.assertEqual(copied.stat().st_mode & 0o777, 0o600)
            copied.write_bytes(b'')
            self.assertEqual(copied.read_bytes(), b'')
            self.assertEqual(original.read_bytes(), b'disposable display authorization')
            restarted = auth_copy(second, original)
            self.assertNotEqual(copied, restarted)
            self.assertEqual(restarted.read_bytes(), original.read_bytes())
            with self.assertRaises(FileExistsError):
                auth_copy(first, original)

    def test_missing_editor_does_not_reserve(self):
        with tempfile.TemporaryDirectory() as root:
            ledger = Ledger(Path(root)/'ledger')
            try:
                app = AppProcess(Owned(), -1, -1, ledger)
                app.evaluate = lambda _: False
                with self.assertRaises(RuntimeError):
                    app.submit('prompt')
                self.assertEqual(ledger.summary()['codex-app']['reserved'], 0)
            finally:
                ledger.close()

    def test_duplicate_or_exhausted_reservation_never_replays_input(self):
        with tempfile.TemporaryDirectory() as root:
            ledger = Ledger(Path(root)/'ledger', limit=1)
            try:
                app = AppProcess(Owned(), -1, -1, ledger)
                app.protected = True
                app.evaluate = lambda _: True
                calls = []
                app.rpc = lambda *args: calls.append(args)
                app.submit('first', identity='a')
                self.assertEqual(len(calls), 3)
                with self.assertRaises(RuntimeError):
                    app.submit('duplicate', identity='a')
                with self.assertRaises(RuntimeError):
                    app.submit('over-limit', identity='b')
                self.assertEqual(len(calls), 3)
                records = [json.loads(s) for s in (Path(root)/'ledger').read_text().splitlines()]
                self.assertTrue(all('text' not in record for record in records))
            finally:
                ledger.close()

    def test_select_session_only_uses_rendered_link(self):
        app = AppProcess(Owned(), -1, -1, None)
        expressions = []
        app.evaluate = lambda e: expressions.append(e) or True
        app.select_session('thread-id')
        self.assertIn('querySelectorAll', expressions[0])
        self.assertNotIn('thread/resume', expressions[0])


if __name__ == '__main__':
    unittest.main()
