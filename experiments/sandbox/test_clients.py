import json
from pathlib import Path
import tempfile
from unittest import TestCase, mock

import clients


class StartupSelectionTest(TestCase):
    def test_existing_app_rollout_is_never_registered_as_new_cli(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            sessions = home/'.codex/sessions'
            sessions.mkdir(parents=True)
            (sessions/'old-app.jsonl').write_text(json.dumps({
                'type': 'session_meta', 'payload': {'id': 'app-history'}})+'\n')
            process = mock.Mock()
            process.poll.return_value = None
            process.process.wait.return_value = 0
            daemon = mock.Mock()
            daemon.poll.return_value = 0

            def create_session(_process, _text):
                (sessions/'new-cli.jsonl').write_text(json.dumps({
                    'type': 'session_meta', 'payload': {'id': 'new-cli'}})+'\n')

            runtime, ledger = mock.Mock(), mock.Mock()
            runtime.spawn.side_effect = [daemon, process]
            ledger.call.return_value = {'allow': True, 'duplicate': False}
            with mock.patch.object(clients, 'terminal'), mock.patch.object(clients.time, 'sleep'), \
                    mock.patch.object(clients, 'submit', side_effect=create_session), \
                    mock.patch.object(clients, 'api') as api:
                result = clients.start(runtime, 'codex', {}, {'codex': '/native'}, home,
                                       ledger, home, configured=True)
            self.assertIs(result, process)
            api.assert_called_once_with(runtime, 'codex', '/sandbox/register', {'id': 'new-cli'})
