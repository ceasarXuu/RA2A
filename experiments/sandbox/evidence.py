"""Extract marker/receipt/final evidence without retaining thinking or secrets."""
import json
import hashlib
from pathlib import Path


def texts(content):
    if isinstance(content, str):
        return content
    return '\n'.join(item.get('text', '') for item in content or [] if item.get('type') in ('text', 'input_text', 'output_text'))


def matches_input(text, marker, source):
    header, separator, body = text.partition('\n\n')
    return (header.splitlines()[:2] == ['[RA2A message]', 'from: '+source]
            and bool(separator) and (envelope_id(header) == marker or body.split()[:1] == [marker]))


def native(home, kind, marker, ack, from_address, oc_messages=None):
    accepted = [ack] if isinstance(ack, str) else ack
    users, finals, errors = [], [], []
    if kind == 'opencode':
        for row in oc_messages or []:
            info = row['info']
            text = texts(row['parts'])
            if info['role'] == 'user' and matches_input(text, marker, from_address):
                users.append({'id': info['id'], 'messageId': envelope_id(text)})
            if info['role'] == 'assistant' and info.get('error') and any(info.get('parentID') == u['id'] for u in users):
                errors.append('NATIVE_MODEL_ERROR')
            if info['role'] == 'assistant' and text in accepted and info.get('time', {}).get('completed'):
                finals.append({'id': info['id'], 'parent': info['parentID']})
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
            'errorClasses': errors, 'status': 'FAIL' if errors else 'PASS' if len(users) == len(finals) == 1 else 'PENDING' if not finals else 'FAIL'}


def envelope_id(text):
    for line in text.splitlines():
        if line.startswith('message-id: '):
            return line[len('message-id: '):].strip()
    return None
