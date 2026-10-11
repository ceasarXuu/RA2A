import json
from pathlib import Path
import tempfile
import unittest

from app_client import AppProcess
from budget import Ledger


class Owned:
    def __init__(self):
        self.stops = 0
    def stop(self):
        self.stops += 1
    def poll(self):
        return None


class AppInputTest(unittest.TestCase):
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
