"""Minimal native record shapes; fixtures contain no thinking or real credentials."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('evidence', Path(__file__).with_name('evidence.py'))
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)
SOURCE, MARKER, ACK = 'ra2a://sandbox/source', 'TEST_MARKER_01', 'ACK_TEST_MARKER_01'
TEXT = f'[RA2A message]\nfrom: {SOURCE}\nmessage-id: transport-a\n\n{MARKER}'


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='r2-evidence-')
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name)

    def records(self, kind, rows):
        directory = self.home / ('.codex/sessions' if kind == 'codex' else '.pi/agent/sessions')
        directory.mkdir(parents=True, exist_ok=True)
        (directory / 'fixture.jsonl').write_text('\n'.join(json.dumps(row) for row in rows) + '\n')
        return evidence.native(self.home, kind, MARKER, ACK, SOURCE)

    def test_combined_opencode_final_keeps_parent_semantics(self):
        base = {'marker': 'BASE', 'busyAtFollowup': True,
                'native': {'status': 'PENDING', 'users': [{'id': 'base-user'}], 'finals': [], 'errorClasses': []}}
        follow = {'marker': 'FOLLOW', 'status': 'PASS', 'native': {'status': 'PASS',
                  'users': [{'id': 'follow-user'}], 'finals': [{'id': 'final', 'parent': 'follow-user',
                  'ack': 'ACK_BASE\nACK_FOLLOW'}]}}
        self.assertTrue(evidence.combined_completion(base, follow))
        self.assertEqual(base['native']['finals'], [])
        follow['native']['finals'][0]['ack'] = 'ACK_FOLLOW'
        self.assertFalse(evidence.combined_completion(base, follow))
        follow['native']['finals'][0]['ack'] = 'ACK_BASE\nACK_FOLLOW'
        follow['native']['finals'][0]['parent'] = 'unrelated-user'
        self.assertFalse(evidence.combined_completion(base, follow))
        follow['native']['finals'][0]['parent'] = 'follow-user'
        base['native']['users'].append({'id': 'duplicate-base'})
        self.assertFalse(evidence.combined_completion(base, follow))

    def test_codex_final_requires_completed_task(self):
        rows = [
            {'type': 'event_msg', 'payload': {'type': 'task_started', 'turn_id': 'turn-a'}},
            {'type': 'response_item', 'payload': {'type': 'message', 'role': 'user', 'content': TEXT}},
            {'type': 'response_item', 'payload': {'type': 'message', 'role': 'assistant', 'phase': 'final_answer', 'content': ACK}},
        ]
        self.assertEqual(self.records('codex', rows)['status'], 'PENDING')
        rows.append({'type': 'event_msg', 'payload': {'type': 'task_complete', 'turn_id': 'turn-a'}})
        result = self.records('codex', rows)
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(result['users'][0]['messageId'], 'transport-a')
        self.assertEqual(result['finals'][0]['turnId'], 'turn-a')

    def test_codex_other_turn_completion_is_not_receipt_completion(self):
        rows = [
            {'type': 'event_msg', 'payload': {'type': 'task_started', 'turn_id': 'turn-a'}},
            {'type': 'response_item', 'payload': {'role': 'user', 'content': TEXT}},
            {'type': 'response_item', 'payload': {'role': 'assistant', 'phase': 'final_answer', 'content': ACK}},
            {'type': 'event_msg', 'payload': {'type': 'task_complete', 'turn_id': 'turn-b'}},
        ]
        self.assertNotEqual(self.records('codex', rows)['status'], 'PASS')

    def test_codex_commentary_ack_is_not_final_answer(self):
        rows = [
            {'type': 'event_msg', 'payload': {'type': 'task_started', 'turn_id': 'turn-a'}},
            {'type': 'response_item', 'payload': {'role': 'user', 'content': TEXT}},
            {'type': 'response_item', 'payload': {'role': 'assistant', 'phase': 'commentary', 'content': ACK}},
            {'type': 'event_msg', 'payload': {'type': 'task_complete', 'turn_id': 'turn-a'}},
        ]
        self.assertNotEqual(self.records('codex', rows)['status'], 'PASS')

    def test_pi_parent_chain_and_native_stop(self):
        rows = [
            {'id': 'receipt', 'parentId': 'before', 'type': 'custom', 'customType': 'ra2a-received', 'data': {'id': 'transport-a', 'text': TEXT}},
            {'id': 'user-a', 'parentId': 'receipt', 'type': 'message', 'message': {'role': 'user', 'content': [{'type': 'text', 'text': TEXT}]}},
            {'id': 'final-a', 'parentId': 'user-a', 'type': 'message', 'message': {'role': 'assistant', 'content': [{'type': 'text', 'text': ACK}], 'stopReason': 'stop'}},
        ]
        result = self.records('pi', rows)
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(result['users'][0]['messageId'], 'transport-a')
        rows[-1]['message']['stopReason'] = 'toolUse'
        self.assertEqual(self.records('pi', rows)['status'], 'PENDING')

    def test_pi_unrelated_parent_is_not_completion(self):
        rows = [
            {'id': 'user-a', 'parentId': 'receipt', 'type': 'message', 'message': {'role': 'user', 'content': TEXT}},
            {'id': 'final-b', 'parentId': 'unrelated-user', 'type': 'message', 'message': {'role': 'assistant', 'content': ACK, 'stopReason': 'stop'}},
        ]
        self.assertNotEqual(self.records('pi', rows)['status'], 'PASS')

    def test_opencode_exact_parent_completion_and_ack(self):
        rows = [
            {'info': {'id': 'user-a', 'role': 'user'}, 'parts': [{'type': 'text', 'text': TEXT}]},
            {'info': {'id': 'final-a', 'role': 'assistant', 'parentID': 'user-a', 'time': {'completed': 1}},
             'parts': [{'type': 'text', 'text': ACK}]},
        ]
        result = evidence.native(self.home, 'opencode', MARKER, ACK, SOURCE, rows)
        self.assertEqual(result['status'], 'PASS')
        rows[-1]['info']['parentID'] = 'other-user'
        self.assertEqual(evidence.native(self.home, 'opencode', MARKER, ACK, SOURCE, rows)['status'], 'PENDING')
        rows[-1]['info']['parentID'] = 'user-a'
        rows[-1]['parts'][0]['text'] += '\nextra summary'
        self.assertEqual(evidence.native(self.home, 'opencode', MARKER, ACK, SOURCE, rows)['status'], 'PENDING')

    def test_wrong_from_envelope_does_not_count(self):
        # Source appearing in prose must not be accepted as the envelope sender.
        text = '[RA2A message]\nfrom: ra2a://wrong/source\nmessage-id: b\n\n' + MARKER + ' references ' + SOURCE
        rows = [
            {'info': {'id': 'u', 'role': 'user'}, 'parts': [{'type': 'text', 'text': text}]},
            {'info': {'id': 'f', 'role': 'assistant', 'parentID': 'u', 'time': {'completed': 1}},
             'parts': [{'type': 'text', 'text': ACK}]},
        ]
        result = evidence.native(self.home, 'opencode', MARKER, ACK, SOURCE, rows)
        self.assertNotEqual(result['status'], 'PASS')

    def test_followup_reference_is_not_another_base_input(self):
        followup = TEXT.replace(MARKER, 'FOLLOWUP_02 mentions '+MARKER)
        rows = [
            {'id': 'u', 'type': 'message', 'message': {'role': 'user', 'content': TEXT}},
            {'id': 'f', 'parentId': 'u', 'type': 'message', 'message': {'role': 'assistant', 'content': ACK, 'stopReason': 'stop'}},
            {'id': 'u2', 'parentId': 'f', 'type': 'message', 'message': {'role': 'user', 'content': followup}},
        ]
        result = self.records('pi', rows)
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(len(result['users']), 1)

    def test_native_error_diagnostics_never_export_sensitive_fields(self):
        error = {'name': 'APIError', 'data': {'statusCode': 401, 'isRetryable': False,
                 'message': 'unauthorized Bearer SECRET', 'responseHeaders': {'Authorization': 'SECRET'},
                 'responseBody': 'SECRET'}}
        rows = [{'info': {'id': 'u', 'role': 'user'}, 'parts': [{'type': 'text', 'text': TEXT}]},
                {'info': {'id': 'a', 'role': 'assistant', 'parentID': 'u', 'error': error}, 'parts': []}]
        result = evidence.native(self.home, 'opencode', MARKER, ACK, SOURCE, rows)
        self.assertEqual(result['status'], 'FAIL')
        self.assertEqual(result['nativeErrors'], [{'type': 'APIError', 'httpStatus': 401,
                                                  'retryable': False, 'category': 'AUTH_REJECTED'}])
        self.assertNotIn('SECRET', json.dumps(result))


if __name__ == '__main__':
    unittest.main()
