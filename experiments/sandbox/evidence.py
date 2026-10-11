"""Extract marker/receipt/final evidence without retaining thinking or secrets."""
import json
import hashlib
import re
from pathlib import Path


def texts(content):
    if isinstance(content, str):
        return content
    return '\n'.join(item.get('text', '') for item in content or [] if item.get('type') in ('text', 'input_text', 'output_text'))


def matches_input(text, marker, source):
    header, separator, body = text.partition('\n\n')
    return (header.splitlines()[:2] == ['[RA2A message]', 'from: '+source]
            and bool(separator) and (envelope_id(header) == marker or body.split()[:1] == [marker]))


def model_error(error):
    """Fixed diagnostic enums only; never export response headers/body/message."""
    error = error if isinstance(error, dict) else {}
    data = error.get('data', {})
    data = data if isinstance(data, dict) else {}
    allowed = {'APIError', 'AuthError', 'ContextOverflowError', 'MessageAbortedError', 'UnknownError'}
    result = {'type': error.get('name') if error.get('name') in allowed else 'OTHER_NATIVE_ERROR'}
    status = data.get('statusCode')
    if type(status) is int and 100 <= status <= 599:
        result['httpStatus'] = status
    if type(data.get('isRetryable')) is bool:
        result['retryable'] = data['isRetryable']
    message = str(data.get('message', '')).lower()
    for category, pattern in [('AUTH_MISSING', r'missing.{0,20}(api.?key|credential)'),
                              ('AUTH_REJECTED', r'unauthorized|invalid api.?key|authentication failed'),
                              ('MODEL_NOT_AVAILABLE', r'model.{0,40}(not found|unavailable|unsupported)'),
                              ('RATE_LIMIT', r'rate limit|too many requests'),
                              ('NETWORK_FAILURE', r'enotfound|econnreset|tls|proxy|timed? ?out')]:
        if re.search(pattern, message):
            result['category'] = category
            break
    return result


def native(home, kind, marker, ack, from_address, oc_messages=None):
    kind = 'codex' if kind == 'app' else kind
    accepted = [ack] if isinstance(ack, str) else ack
    users, finals, errors, diagnostics = [], [], [], []
    if kind == 'opencode':
        for row in oc_messages or []:
            info = row['info']
            text = texts(row['parts'])
            if info['role'] == 'user' and matches_input(text, marker, from_address):
                users.append({'id': info['id'], 'messageId': envelope_id(text)})
            if info['role'] == 'assistant' and info.get('error') and any(info.get('parentID') == u['id'] for u in users):
                errors.append('NATIVE_MODEL_ERROR')
                diagnostics.append(model_error(info['error']))
            if info['role'] == 'assistant' and text in accepted and info.get('time', {}).get('completed'):
                finals.append({'id': info['id'], 'parent': info['parentID'], 'ack': text})
        finals = [r for r in finals if any(r['parent'] == u['id'] for u in users)]
    else:
        base = Path(home)/('.codex/sessions' if kind == 'codex' else '.pi/agent/sessions')
        for path in base.rglob('*.jsonl'):
            active, candidate, turn, expected_turn, parents, user_id = False, None, None, None, {}, None
            for number, line in enumerate(path.read_text().splitlines(), 1):
                try:
                    row = json.loads(line)
                except ValueError:
                    continue
                parents[row.get('id')] = row.get('parentId')
                data = row.get('payload', {}) if kind == 'codex' else row.get('message', {})
                if kind == 'codex' and row.get('type') != 'response_item':
                    if data.get('type') == 'task_started':
                        turn = data.get('turn_id')
                        active, candidate = False, None
                    if active and candidate and expected_turn and data.get('turn_id') == expected_turn and row.get('type') == 'event_msg' and data.get('type') == 'task_complete':
                        finals.append({**candidate, 'completionLine': number, 'turnId': data.get('turn_id')})
                        candidate = None
                    continue
                role = data.get('role')
                text = texts(data.get('content'))
                if role == 'user':
                    matches = matches_input(text, marker, from_address)
                    active = matches or (active and kind == 'codex' and turn == expected_turn)
                    candidate = None
                    if matches:
                        expected_turn, user_id = turn, row.get('id')
                        users.append({'turnId': expected_turn, 'id': row.get('id'), 'line': number, 'source': from_address, 'recordSHA256': hashlib.sha256(line.encode()).hexdigest(), 'messageId': envelope_id(text)})
                if active and role == 'assistant' and data.get('stopReason') in ('error', 'aborted'):
                    errors.append('NATIVE_MODEL_ERROR')
                if active and role == 'assistant' and text in accepted:
                    ancestor, seen = row.get('parentId'), set()
                    while ancestor and ancestor != user_id and ancestor not in seen:
                        seen.add(ancestor); ancestor = parents.get(ancestor)
                    stop = (kind == 'codex' and data.get('phase') == 'final_answer') or (data.get('stopReason') == 'stop' and ancestor == user_id)
                    if stop:
                        final = {'id': row.get('id'), 'line': number, 'ack': text, 'recordSHA256': hashlib.sha256(line.encode()).hexdigest()}
                        if kind == 'codex':
                            candidate = final
                        else:
                            finals.append(final)
    return {'users': users, 'finals': finals,
            'errorClasses': errors, 'nativeErrors': diagnostics, 'status': 'FAIL' if errors else 'PASS' if len(users) == len(finals) == 1 else 'PENDING' if not finals else 'FAIL'}


def envelope_id(text):
    for line in text.splitlines():
        if line.startswith('message-id: '):
            return line[len('message-id: '):].strip()
    return None


def combined_completion(base, follow):
    """One OpenCode final acknowledges both users; never fabricate two finals."""
    b, f = base.get('native', {}), follow.get('native', {})
    expected = 'ACK_'+base.get('marker', '')+'\nACK_'+follow.get('marker', '')
    return (base.get('busyAtFollowup') is True and b.get('status') == 'PENDING'
            and len(b.get('users', [])) == 1 and not b.get('errorClasses')
            and follow.get('status') == f.get('status') == 'PASS'
            and len(f.get('users', [])) == len(f.get('finals', [])) == 1
            and b['users'][0]['id'] != f['users'][0]['id']
            and f['finals'][0].get('parent') == f['users'][0]['id']
            and f['finals'][0].get('ack') == expected)
