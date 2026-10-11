"""Resume argument and discovery gates, without launching a client or model."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import MagicMock, patch

spec = importlib.util.spec_from_file_location('clients', Path(__file__).with_name('clients.py'))
clients = importlib.util.module_from_spec(spec)
spec.loader.exec_module(clients)


class ResumeTests(unittest.TestCase):
    def test_pi_resume_preserves_native_header_and_private_path(self):
        with tempfile.TemporaryDirectory(prefix='r2-resume-') as directory:
            home = Path(directory)
            native_id = '01a10897-6200-716e-9cda-e6b0d87ed09c'
            path = home / '.pi/agent/sessions/--home-sandbox-work--/native.jsonl'
            path.parent.mkdir(parents=True)
            path.write_text(json.dumps({'type': 'session', 'version': 3, 'id': native_id,
                                       'cwd': '/home/sandbox/work'}) + '\n')
            before = path.read_bytes()
            runtime, old = MagicMock(), MagicMock()
            bins = {'node': '/native/node', 'pi': '/native/cli.js'}
            with patch.object(clients, 'terminal'), patch.object(clients, 'api', return_value={'body': []}):
                clients.resume(runtime, 'pi', {'PI_OFFLINE': '1'}, bins, home, 'pi.' + native_id, old)
            old.stop.assert_called_once()
            arguments = runtime.spawn.call_args.args
            self.assertEqual(arguments[0], 'pi')
            self.assertEqual(arguments[1], ['/native/node', '--import', '/suite/network.mjs',
                             '/native/cli.js', '--session',
                             '/home/sandbox/.pi/agent/sessions/--home-sandbox-work--/native.jsonl'])
            self.assertEqual(path.read_bytes(), before)

    def test_discovery_stale_endpoint_is_not_ready_after_resume(self):
        # No process or HTTP call: old discovery remains while new Pi initializes.
        stale = {'body': [{'id': 'sandbox-pi', 'status': 'ready', 'sessionsStale': True,
                          'sessions': [{'id': 'pi.native-a', 'agent': 'pi', 'status': 'ready',
                                        'address': 'ra2a://sandbox-pi/pi.native-a'}]}]}
        with patch.object(clients, 'api', return_value=stale), patch.object(clients.time, 'sleep'):
            with self.assertRaises(RuntimeError):
                clients.endpoint(MagicMock(), 'pi', timeout=.001)

    def test_resume_waits_for_old_attachment_withdrawal(self):
        runtime, old = MagicMock(), MagicMock()
        rows = {'body': [{'sessions': [{'id': 'same'}]}]}
        with patch.object(clients, 'api', return_value=rows), \
             patch.object(clients.time, 'monotonic', side_effect=[0, 13]):
            with self.assertRaisesRegex(RuntimeError, 'did not withdraw'):
                clients.resume(runtime, 'pi', {}, {}, '/unused', 'same', old)
        old.stop.assert_called_once()
        runtime.spawn.assert_not_called()

    def test_codex_waits_past_twelve_seconds_but_observes_withdrawal(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            (home/'.codex').mkdir()
            (home/'.codex/config.toml').write_text('thread_unload_delay_secs = 60\n')
            rows = {'body': [{'sessions': [{'id': 'same'}]}]}
            runtime, old = MagicMock(), MagicMock()
            with patch.object(clients, 'api', side_effect=[rows, rows, {'body': []}]), \
                 patch.object(clients.time, 'monotonic', side_effect=[0, 13, 61]), \
                 patch.object(clients.time, 'sleep'), patch.object(clients, 'terminal'):
                clients.resume(runtime, 'codex', {}, {'codex': '/native/codex'}, home, 'same', old)
            self.assertEqual(runtime.spawn.call_args.args[1],
                             ['/native/codex', 'resume', 'same', '--no-alt-screen'])
            old.stop.assert_called_once()

    def test_codex_config_delay_is_bounded_and_invalid_config_does_not_stop(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            (home/'.codex').mkdir()
            config = home/'.codex/config.toml'
            config.write_text('')
            self.assertEqual(clients.withdrawal_timeout('codex', home), 72)
            config.write_text('thread_unload_delay_secs = 1')
            self.assertEqual(clients.withdrawal_timeout('codex', home), 13)
            config.write_text('thread_unload_delay_secs = 900')
            self.assertEqual(clients.withdrawal_timeout('codex', home), 180)
            config.write_text('thread_unload_delay_secs = -1')
            old = MagicMock()
            with self.assertRaises(ValueError):
                clients.resume(MagicMock(), 'codex', {}, {}, home, 'same', old)
            old.stop.assert_not_called()


if __name__ == '__main__':
    unittest.main()
