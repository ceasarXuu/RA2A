import json
from pathlib import Path
import tempfile
import unittest
import shutil
import subprocess

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

    @unittest.skipUnless(shutil.which('node'), 'Node required for DOM selection fixture')
    def test_sidebar_selection_clicks_only_visible_exact_native_id(self):
        app = AppProcess(Owned(), -1, -1, None)
        expressions = []
        app.evaluate = lambda e: expressions.append(e) or True
        app.select_session('target')
        script = '''let clicks=[];
const rows=[['local:target-other',true],['local:target',false],['local:target',true],['target',true]];
global.document={querySelectorAll(selector){
 if(selector!=='[data-app-action-sidebar-thread-id]')throw Error('wrong control');
 return rows.map(([id,visible],index)=>({
 getAttribute(name){return name==='data-app-action-sidebar-thread-id'?id:null},
 getClientRects(){return visible?[{}]:[]},click(){clicks.push(index)}}));
}};
const result=eval(process.argv[1]);
if(result!==true||JSON.stringify(clicks)!=='[2]')process.exit(1);
'''
        result = subprocess.run([shutil.which('node'), '-e', script, expressions[0]],
                                capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, 'sidebar native-ID DOM fixture failed')


class ExactFinalVisibilityTest(unittest.TestCase):
    def test_request_substring_is_not_a_rendered_final(self):
        import shutil
        import subprocess
        from unittest import mock
        node = shutil.which('node')
        self.assertIsNotNone(node)
        app = AppProcess.__new__(AppProcess)
        expression = []
        app.evaluate = lambda value: expression.append(value) or False
        app.visible('ACK_TEST')
        script = r"""
const request='[RA2A message] Reply only ACK_TEST. Do not call tools.';
global.document={body:{innerText:request},querySelectorAll(){return [
 {innerText:request,getClientRects(){return [{}]}},
 {innerText:'ACK_TEST',getClientRects(){return []}}
]}};
if(eval(process.argv[1])!==false)process.exit(1);
document.querySelectorAll=()=>[{innerText:'ACK_TEST',getClientRects(){return [{}]}}];
if(eval(process.argv[1])!==true)process.exit(2);
"""
        result=subprocess.run([node,'-e',script,expression[0]],capture_output=True,timeout=5)
        self.assertEqual(result.returncode,0,'input substring/hidden reply mistaken for rendered final')

    def test_combined_native_final_and_auth_marker(self):
        app = AppProcess.__new__(AppProcess)
        expressions=[];app.evaluate=lambda value: expressions.append(value) or True
        self.assertTrue(app.visible('ACK_BASE\nACK_FOLLOWUP'))
        self.assertTrue(app.visible('Sign in to ChatGPT'))
        self.assertIn('ACK_BASE',expressions[0])
        self.assertIn('document.body.innerText.includes',expressions[1])


if __name__ == '__main__':
    unittest.main()
