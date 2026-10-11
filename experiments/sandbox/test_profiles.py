import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('profiles', Path(__file__).with_name('profiles.py'))
profiles = importlib.util.module_from_spec(spec)
spec.loader.exec_module(profiles)


class ProfileTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.host = self.base / 'host'
        self.host.mkdir()

    def write(self, relative, value):
        path = self.host / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(value if isinstance(value, str) else json.dumps(value))
        return path

    def binary(self, kind):
        rel = '.codex/packages/standalone/current/bin/codex' if kind == 'codex' else '.opencode/bin/opencode'
        path = self.write(rel, 'native fixture')
        path.chmod(0o700)

    def test_codex_copy_minimal_and_no_writeback(self):
        self.binary('codex')
        self.write('.codex/config.toml', 'model="selected"\n[mcp_servers.unsafe]\ncommand="host-command"\n')
        original = self.write('.codex/auth.json', {'auth_mode': 'chatgpt', 'tokens': {
            'access_token': 'secret-a', 'refresh_token': 'secret-r', 'id_token': 'secret-i'}})
        before = original.read_bytes()
        result = profiles.prepare(self.base / 'run', ['codex'], self.host)
        target = Path(result['nodes']['codex']['home'])
        self.assertEqual(result['manifest']['codex']['status'], 'READY')
        self.assertNotIn('secret', json.dumps(result['manifest']))
        self.assertNotIn('mcp_servers', (target / '.codex/config.toml').read_text())
        self.assertEqual((target / '.codex/auth.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual(target.stat().st_mode & 0o777, 0o700)
        (target / '.codex/auth.json').write_text('{}')
        self.assertEqual(original.read_bytes(), before)

    def test_opencode_recent_is_not_selection(self):
        self.binary('opencode')
        self.write('.config/opencode/opencode.json', {})
        self.write('.local/state/opencode/model.json', {'recent': [{'providerID': 'a', 'modelID': 'b'}]})
        result = profiles.prepare(self.base / 'run', ['opencode'], self.host)
        self.assertEqual(result['manifest']['opencode']['status'], 'BLOCKED_AUTH')
        self.assertNotIn('opencode', result['nodes'])

    def test_selected_provider_only(self):
        self.binary('opencode')
        self.write('.config/opencode/opencode.json', {'model': 'a/model'})
        self.write('.cache/opencode/models.json', {'a': {'api': 'https://user:secret-url@api.example.test/v1?key=secret-query', 'models': {'model': {}}}})
        self.write('.local/share/opencode/auth.json', {'a': {'type': 'api', 'key': 'a-secret'},
                                                     'b': {'type': 'api', 'key': 'b-secret'}})
        result = profiles.prepare(self.base / 'run', ['opencode'], self.host)
        auth = json.loads((Path(result['nodes']['opencode']['home']) / '.local/share/opencode/auth.json').read_text())
        self.assertEqual(list(auth), ['a'])
        self.assertNotIn('secret', json.dumps(result['manifest']))

    def test_native_recent_default_requires_catalog_match(self):
        self.binary('opencode')
        self.write('.config/opencode/opencode.json', {})
        self.write('.local/state/opencode/model.json', {'recent': [{'providerID': 'a', 'modelID': 'model'}]})
        self.write('.cache/opencode/models.json', {'a': {'api': 'https://user:secret-url@api.example.test/v1?key=secret-query', 'models': {'model': {}}}})
        self.write('.local/share/opencode/auth.json', {'a': {'type': 'api', 'key': 'secret'}})
        result = profiles.prepare(self.base / 'run', ['opencode'], self.host)
        self.assertEqual(result['manifest']['opencode']['model'], 'model')
        self.assertEqual(result['manifest']['opencode']['hosts'], ['api.example.test'])

    def test_existing_root_refused(self):
        root = self.base / 'run'
        root.mkdir()
        (root / 'preserve').write_text('data')
        with self.assertRaises(profiles.BlockedAuth):
            profiles.prepare(root, [], self.host)
        self.assertEqual((root / 'preserve').read_text(), 'data')


if __name__ == '__main__':
    unittest.main()
