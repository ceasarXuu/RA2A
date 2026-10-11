"""Serial real-client matrix, native evidence, bounded budget and owned cleanup."""
import ctypes
import json
import os
import re
import signal
from pathlib import Path
import time
import uuid

import budget
import clients
import evidence
from runtime import Runtime


KINDS = ('codex', 'opencode', 'pi')


def execute(root):
    # Prevent sibling Agent processes from reading controller roots/fds/env.
    if ctypes.CDLL(None).prctl(4, 0, 0, 0, 0):
        raise RuntimeError('controller proc protection unavailable')
    root = Path(root)
    config = json.loads((root/'private.json').read_text())
    ledger = budget.Ledger(root/'ledger.jsonl')
    server = budget.serve(root/'budget.sock', ledger)
    report = {'runId': config['runId'], 'scope': 'real-clients-private-LAN',
              'clients': config['manifest'], 'cases': [], 'matrixStatus': 'FAIL',
              'manual': 'MANUAL_PENDING', 'credentialWriteback': False}
    runtime = Runtime(root, config['bins'], config['nodes'], config['hostNetns'])
    endpoints, environments, processes, failed = {}, {}, {}, set()
    try:
        runtime.start()
        for kind in KINDS:
            if config['manifest'][kind]['status'] != 'READY':
                continue
            runtime.add_mount(kind, Path(__file__).parent, '/suite')
            runtime.add_mount(kind, root/'model.sock', '/run/model.sock')
            env = {'HTTP_PROXY': 'http://127.0.0.1:7890', 'HTTPS_PROXY': 'http://127.0.0.1:7890',
                   'NO_PROXY': '127.0.0.1,localhost,198.18.0.0/24',
                   'RA2A_PI_SDK': config['bins'].get('pi_sdk', ''),
                   'RA2A_PI_NODE_ID': 'sandbox-'+config['runId']+'-'+kind,
                   'RA2A_SANDBOX_CODEX_BIN': config['bins'].get('codex', ''),
                   **config['private_env'].get(kind, {})}
            environments[kind] = env
            version_args = [config['bins'][kind], '--version'] if kind != 'pi' else [config['bins']['node'], config['bins']['pi'], '--version']
            version = runtime.spawn(kind, version_args, env)
            version.process.wait(timeout=15)
            raw = Path(version.logs[0].name).read_text().strip()
            if version.poll() == 0 and re.fullmatch(r'[a-zA-Z0-9 ._-]{1,100}', raw):
                report['clients'][kind]['nativeVersion'] = raw
            else:
                report['clients'][kind]['status'] = 'BLOCKED_VERSION'; continue
            runtime.spawn(kind, ['python3', '/suite/relay.py', '/run/model.sock'])
            runtime.start_node(kind, 'sandbox-'+config['runId']+'-'+kind,
                               config['pin'], root/'budget.sock', env)
            try:
                processes[kind] = clients.start(runtime, kind, env, config['bins'],
                                               config['nodes'][kind]['home'], ledger, Path(config['source']))
                endpoints[kind] = clients.endpoint(runtime, kind)
                report['clients'][kind]['endpoint'] = endpoints[kind]['address']
            except (OSError, RuntimeError, TimeoutError, ValueError) as error:
                report['clients'][kind].update(status='BLOCKED_ADAPTER', reason=type(error).__name__)
        # All four planned types are represented; unsupported App isn't forged.
        report['clients']['app'] = {'status': 'BLOCKED_ADAPTER',
                                    'reason': 'safe actual-owner App bridge not integrated'}
        for sender in (*KINDS, 'app'):
            for receiver in (*KINDS, 'app'):
                case = {'case': f'basic-{sender}-{receiver}', 'sender': sender, 'receiver': receiver}
                if sender not in endpoints or receiver not in endpoints:
                    case['status'] = 'BLOCKED_ADAPTER'
                elif receiver in failed:
                    case.update(status='NOT_RUN_DEPENDENCY', reason='earlier receiver failure; no further paid input')
                else:
                    send(runtime, config, endpoints, sender, receiver, case)
                    if case['status'] != 'PASS': failed.add(receiver)
                report['cases'].append(case)
        # Genuine tool ring: preserve planned edges, don't silently replace App.
        ring = [('codex', 'opencode'), ('opencode', 'pi'), ('pi', 'app'), ('app', 'codex')]
        for sender, receiver in ring:
            case = {'case': f'tool-{sender}-{receiver}', 'sender': sender, 'receiver': receiver}
            if sender not in endpoints or receiver not in endpoints or sender in failed or receiver in failed:
                case['status'] = 'BLOCKED_ADAPTER'
            else:
                reply = 'REPLY_'+uuid.uuid4().hex
                extra = 'Before the final ACK call ra2a send_message exactly once to '+endpoints[sender]['address']
                extra += ' with text '+json.dumps(reply+' Reply only ACK_'+reply+'. Do not call tools.')
                extra += '. Your from address is '+endpoints[receiver]['address']+'. Never retry. '
                send(runtime, config, endpoints, sender, receiver, case, extra)
                home = config['nodes'][sender]['home']
                receipt = wait_native(runtime, sender, home, endpoints[sender]['id'], reply,
                                      'ACK_'+reply, endpoints[receiver]['address'], 90)
                case['businessReply'] = receipt
                if receipt['status'] != 'PASS':
                    case['status'] = 'FAIL'
            report['cases'].append(case)
        # Active follow-up reuses one budgeted base and immediately sends a second.
        for receiver in (k for k in endpoints if k not in failed):
            case = {'case': 'active-'+receiver, 'sender': receiver, 'receiver': receiver}
            follow = {'case': 'followup-'+receiver, 'sender': receiver, 'receiver': receiver}
            send(runtime, config, endpoints, receiver, receiver, case,
                 'Before your final ACK call RA2A list_targets exactly four times, serially. ', wait=False)
            time.sleep(1)
            case['busyAtFollowup'] = clients.endpoint(runtime, receiver).get('status') == 'busy'
            if not case['busyAtFollowup']:
                case['status'] = 'FAIL'; follow['status'] = 'NOT_RUN_NOT_ACTIVE'
                report['cases'] += [case, follow]; continue
            combined = 'ACK_'+case.get('marker', '')+'\n{ack}'
            send(runtime, config, endpoints, receiver, receiver, follow,
                 'If previous input has not finished, final reply exactly '+json.dumps(combined)+'. Otherwise only the new ACK.',
                 alternate=combined)
            case['native'] = wait_native(runtime, receiver, config['nodes'][receiver]['home'], endpoints[receiver]['id'],
                                        case.get('marker', ''), ['ACK_'+case.get('marker', ''),
                                        'ACK_'+case.get('marker', '')+'\nACK_'+follow.get('marker', '')], endpoints[receiver]['address'], 90)
            case['status'] = case['native']['status'] if case['native']['status'] != 'PENDING' else 'INCOMPLETE_TIMEOUT'
            report['cases'] += [case, follow]
        for kind in (k for k in endpoints if k not in failed):
            case = {'case': 'resume-'+kind, 'sender': kind, 'receiver': kind}
            try:
                original = endpoints[kind]['id']
                case['originalSession'] = original
                processes[kind] = clients.resume(runtime, kind, environments[kind], config['bins'],
                    config['nodes'][kind]['home'], original, processes[kind])
                endpoints[kind] = clients.endpoint(runtime, kind)
                case['resumedSession'] = endpoints[kind]['id']
                if endpoints[kind]['id'] != original:
                    raise RuntimeError('resume changed native identity')
                send(runtime, config, endpoints, kind, kind, case)
            except Exception as error:
                case.update(status='FAIL', errorClass=type(error).__name__)
                if isinstance(error, clients.OwnerWithdrawalTimeout):
                    case['reason'] = error.code
            if case['status'] != 'PASS':
                failed.add(kind)
            report['cases'].append(case)
        for receiver in (k for k in endpoints if k not in failed):
            sources = [k for k in endpoints if k != receiver]
            case = {'case': 'native-postwrite-ack-loss-'+receiver, 'receiver': receiver}
            if not sources:
                case.update(status='NOT_RUN', reason='requires a distinct real LAN sender')
            else:
                send(runtime, config, endpoints, sources[0], receiver, case, ack_loss=True)
            report['cases'].append(case)
        report['cases'] += [{'case': phase+'-'+kind, 'status': 'BLOCKED_ADAPTER'}
                            for kind in KINDS if kind not in endpoints
                            for phase in ('active', 'followup', 'resume', 'native-postwrite-ack-loss')]
        report['cases'] += [{'case': phase+'-'+kind, 'status': 'NOT_RUN_DEPENDENCY'}
                            for kind in failed for phase in ('active', 'followup', 'resume', 'native-postwrite-ack-loss')
                            if not any(r['case'] == phase+'-'+kind for r in report['cases'])]
        report['cases'].append({'case': 'codex-owner-transfer', 'status': 'BLOCKED_ADAPTER',
                               'reason': 'actual writer App bridge integration pending'})
        if config.get('manual'):
            manual(runtime, config, endpoints, processes, ledger, report)
        else:
            report['manualInstructions'] = ['运行时添加 --manual，自动阶段后按提示观察各沙盒终端。',
                '每类 Agent 仅可提交一次指定标记，先保留预算再输入；禁止绕过控制器直接键入模型提示。']
        report['matrixStatus'] = 'PASS' if all(r['status'] == 'PASS' for r in report['cases']) else 'INCOMPLETE'
    except Exception as error:
        report['errorClass'] = type(error).__name__
    finally:
        server.shutdown()
        server.server_close()
        report['budget'] = ledger.summary()
        report['budgetNotices'] = [k+' 本轮额度已用完；含启动/工具回信/人工输入，失败和unknown不退款，后续输入已拦截。'
                                  for k, v in report['budget'].items() if v['reserved'] >= v['limit']]
        ledger.close()
        try:
            runtime.stop()
            report['ownedResources'] = 'REAPED'
        except RuntimeError:
            report['ownedResources'] = 'CLEANUP_INCOMPLETE'
        (root/'summary.json').write_text(json.dumps(report, indent=2))
        rows = ['# Linux 真实 Agent 矩阵验收', '', '整体状态：'+report['matrixStatus'], '',
                '收件 accepted 与原生最终完成分别列示；阻塞/未执行项不算通过。', '']
        rows += ['- '+r['case']+'：'+r['status'] for r in report['cases']]
        rows += ['', '## 本轮预算', '']+[f'- {k}：{v["reserved"]}/{v["limit"]}' for k,v in report['budget'].items()]
        rows += report['budgetNotices']
        rows += ['', '人工：'+report['manual'], '资源：'+report.get('ownedResources', 'UNKNOWN')]
        (root/'report.md').write_text('\n'.join(rows)+'\n')
    return report


def send(runtime, config, endpoints, sender, receiver, case, extra='', wait=True, ack_loss=False, alternate=None):
    marker = 'R2S_'+uuid.uuid4().hex
    case['marker'] = marker
    message = marker+' Reply only ACK_'+marker+'. No extra text. '+('Do not call any tools. ' if not extra else '')+extra.replace('{ack}', 'ACK_'+marker)
    ack = ['ACK_'+marker, alternate.replace('{ack}', 'ACK_'+marker)] if alternate else 'ACK_'+marker
    try:
        started = time.monotonic()
        route = '/v1/send'
        if ack_loss:
            clients.api(runtime, receiver, '/sandbox/withhold', {'id': marker})
            route = '/sandbox/send-timeout'
        reply = clients.api(runtime, sender, route, {'from': endpoints[sender]['address'],
                            'to': endpoints[receiver]['address'], 'text': message, 'messageId': marker})
        case['receiptMS'] = round((time.monotonic()-started)*1000)
        case['transport'] = reply
        if ack_loss:
            if reply['body'].get('error') != 'DELIVERY_UNKNOWN':
                case['status'] = 'FAIL'; return
            receipt = wait_native(runtime, receiver, config['nodes'][receiver]['home'], endpoints[receiver]['id'],
                                  marker, ack, endpoints[sender]['address'], 90)
            case.update(native=receipt, status=receipt['status'] if receipt['status'] != 'PENDING' else 'INCOMPLETE_TIMEOUT', replayCount=0)
            return
        if reply['code'] != 200:
            case['status'] = 'FAIL'
            return
        case['status'] = 'RECEIPT_ONLY'
        if wait:
            receipt = wait_native(runtime, receiver, config['nodes'][receiver]['home'], endpoints[receiver]['id'],
                                  marker, ack, endpoints[sender]['address'], 90)
            case['native'] = receipt
            case['status'] = receipt['status'] if receipt['status'] != 'PENDING' else 'FAIL'
    except Exception as error:
        case.update(status='FAIL', errorClass=type(error).__name__)


def wait_native(runtime, kind, home, session, marker, ack, from_address, timeout):
    end = time.monotonic()+timeout
    result = {'status': 'PENDING'}
    while time.monotonic() < end:
        messages = clients.api(runtime, kind, f'/session/{session}/message', port=4099)['body'] if kind == 'opencode' else None
        result = evidence.native(home, kind, marker, ack, from_address, messages)
        if result['status'] != 'PENDING':
            return result
        time.sleep(.5)
    return result


def manual(runtime, config, endpoints, processes, ledger, report):
    report['manualObservations'] = {}
    for kind in endpoints:
        process = processes[kind]
        if process.poll() is not None:
            report['manualObservations'][kind] = {'status': 'BLOCKED_ADAPTER', 'reason': 'native TUI exited'}
            continue
        print('人工观察 '+kind+'：将显示沙盒终端；确认可见后按回车提交唯一标记。', flush=True)
        process.echo = True
        os.killpg(process.pid, signal.SIGWINCH)
        input()
        marker = 'MANUAL_'+uuid.uuid4().hex
        reservation = ledger.call('/reserve', {'kind': 'codex-cli' if kind == 'codex' else kind, 'id': marker})
        if not reservation['allow']:
            print(reservation['error'], flush=True)
            report['manualObservations'][kind] = {'status': 'NOT_RUN_BUDGET', 'reason': reservation['error']}
        else:
            clients.submit(process, 'Reply only ACK_'+marker+'. Do not use tools.')
            answer = input('观察：输入是否出现、回复是否出现、完成后输入栏是否可继续？全满足输入 PASS：')
            report['manualObservations'][kind] = {'status': 'PASS' if answer.strip() == 'PASS' else 'FAIL'}
        process.echo = False
    report['manual'] = 'PASS' if report['manualObservations'] and all(x['status'] == 'PASS' for x in report['manualObservations'].values()) else 'INCOMPLETE'
