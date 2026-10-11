"""Controller sends once; unknown receipts never cause replay."""
import sys
from pathlib import Path
import unittest
import tempfile
from unittest.mock import MagicMock
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import runner
import budget


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.config = {'nodes': {'pi': {'home': '/private/pi'}}}
        self.endpoints = {'opencode': {'address': 'ra2a://sandbox-oc/ses_a', 'id': 'ses_a'},
                          'pi': {'address': 'ra2a://sandbox-pi/pi.a', 'id': 'pi.a'}}

    def test_real_send_uses_explicit_source_and_single_input(self):
        case = {}
        with patch.object(runner.clients, 'api', return_value={'code': 200, 'body': {'status': 'accepted'}}) as api, \
             patch.object(runner, 'wait_native', return_value={'status': 'PASS', 'users': [{}], 'finals': [{}]}):
            runner.send(None, self.config, self.endpoints, 'opencode', 'pi', case)
        self.assertEqual(api.call_count, 1)
        body = api.call_args.args[3]
        self.assertEqual(body['from'], self.endpoints['opencode']['address'])
        self.assertEqual(body['to'], self.endpoints['pi']['address'])
        self.assertEqual(body['messageId'], case['marker'])
        self.assertEqual(body['text'].split()[0], case['marker'])
        self.assertIn('Do not call any tools.', body['text'])
        self.assertEqual(case['status'], 'PASS')

    def test_postwrite_unknown_is_audited_without_resending(self):
        case = {}
        with patch.object(runner.clients, 'api', side_effect=[{'code': 200, 'body': {'armed': True}},
                    {'code': 200, 'body': {'status': 'error', 'error': 'DELIVERY_UNKNOWN'}}]) as api, \
             patch.object(runner, 'wait_native', return_value={'status': 'PASS'}) as native:
            runner.send(None, self.config, self.endpoints, 'opencode', 'pi', case, ack_loss=True)
        self.assertEqual([c.args[2] for c in api.call_args_list], ['/sandbox/withhold', '/sandbox/send-timeout'])
        self.assertEqual(api.call_args_list[0].args[3]['id'], case['marker'])
        self.assertEqual(native.call_count, 1)
        self.assertEqual(case['replayCount'], 0)

    def test_transport_exception_stops_without_native_or_retry(self):
        case = {}
        with patch.object(runner.clients, 'api', side_effect=RuntimeError('private failure')) as api, \
             patch.object(runner, 'wait_native') as native:
            runner.send(None, self.config, self.endpoints, 'opencode', 'pi', case)
        self.assertEqual(api.call_count, 1)
        native.assert_not_called()
        self.assertEqual(case['status'], 'FAIL')
        self.assertNotIn('private failure', str(case))

    def test_manual_exited_tui_is_blocked_without_paid_input(self):
        process, ledger, report = MagicMock(), MagicMock(), {}
        process.poll.return_value = 0
        runner.manual(None, {}, {'pi': self.endpoints['pi']}, {'pi': process}, ledger, report)
        ledger.call.assert_not_called()
        process.write.assert_not_called()
        self.assertEqual(report['manual'], 'INCOMPLETE')
        self.assertEqual(report['manualObservations']['pi']['status'], 'BLOCKED_ADAPTER')

    def test_manual_app_budget_full_never_submits(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = budget.Ledger(Path(directory)/'ledger', limit=0)
            self.addCleanup(ledger.close)
            app = MagicMock(); app.poll.return_value = None
            report = {}
            with patch('builtins.input', return_value=''), patch.object(runner.os, 'killpg'):
                runner.manual(None, {}, {'app': {}}, {'app': app}, ledger, report)
            app.submit.assert_not_called()
            self.assertEqual(report['manualObservations']['app']['status'], 'NOT_RUN_BUDGET')
            self.assertEqual(ledger.summary()['codex-app']['reserved'], 0)

    def test_manual_app_has_one_controller_reservation(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = budget.Ledger(Path(directory)/'ledger', limit=1)
            self.addCleanup(ledger.close)
            app = MagicMock(); app.poll.return_value = None
            def submit(text):
                self.assertIn('ACK_MANUAL_', text)
                result = ledger.call('/reserve', {'kind': 'codex-app', 'id': 'app-submit'})
                self.assertTrue(result['allow'])
            app.submit.side_effect = submit
            report = {}
            with patch('builtins.input', side_effect=['', 'PASS']), patch.object(runner.os, 'killpg'):
                runner.manual(None, {}, {'app': {}}, {'app': app}, ledger, report)
            self.assertEqual(report['manual'], 'PASS')
            self.assertEqual(ledger.summary()['codex-app']['reserved'], 1)
