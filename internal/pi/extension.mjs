// RA2A Pi bridge: receipt belongs to this extension, not Pi's model queue.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import { randomBytes } from 'node:crypto';
import { Type } from 'typebox';
import { Text } from '@earendil-works/pi-tui';

export default function (pi) {
  const directory = process.env.RA2A_PI_SESSION_DIR || path.join(os.homedir(), '.config', 'ra2a', 'pi-sessions');
  const leasePath = path.join(directory, `attachment.${process.pid}.json`);
  const token = randomBytes(32).toString('hex');
  const control = new URL(process.env.RA2A_CONTROL_URL || `http://${process.env.RA2A_CONTROL_ADDRESS || '127.0.0.1:47321'}`);
  if (control.protocol !== 'http:' || control.hostname !== '127.0.0.1') throw new Error('RA2A control must use IPv4 loopback HTTP');
  let current, server, heartbeat, nodeID;
  const endpoint = ctx => `pi.${ctx.sessionManager.getSessionId()}`;
  const writeLease = () => {
    if (!current || !server?.listening) return;
    const record = { sessionID: endpoint(current), pid: process.pid, expires: Date.now() + 3000,
      url: `http://127.0.0.1:${server.address().port}`, token, status: current.isIdle() ? 'ready' : 'busy',
      title: current.sessionManager.getSessionName() || 'Pi' };
    fs.writeFileSync(leasePath + '.next', JSON.stringify(record), { mode: 0o600 });
    fs.renameSync(leasePath + '.next', leasePath);
  };
  const stop = () => {
    current = undefined;
    clearInterval(heartbeat);
    server?.close(); server = undefined;
    for (const file of [leasePath, leasePath + '.next']) {
      try { fs.unlinkSync(file); } catch (e) { if (e.code !== 'ENOENT') console.error(`RA2A lease cleanup failed (${e.code}); record expires by TTL`); }
    }
  };
  pi.registerEntryRenderer('ra2a-received', entry => new Text(`[RA2A received: ${entry.data.id}]\n${entry.data.text}`, 0, 0));
  pi.on('session_start', async (_, ctx) => {
    stop();
    nodeID = process.env.RA2A_PI_NODE_ID || JSON.parse(fs.readFileSync(path.join(directory, '..', 'config.json'), 'utf8')).nodeId;
    if (!nodeID) throw new Error('RA2A node identity is unavailable');
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    current = ctx;
    server = http.createServer((request, response) => {
      const reply = (status, value) => { response.writeHead(status, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(value)); };
      if (request.method !== 'POST' || request.url !== '/receive' || request.headers.authorization !== `Bearer ${token}`) {
        reply(403, { error: 'unauthorized' }); return;
      }
      let body = '';
      request.on('data', chunk => { body += chunk; if (Buffer.byteLength(body) > 1048576) request.destroy(); });
      request.on('error', () => {});
      request.on('end', () => {
        try {
          const message = JSON.parse(body), owner = current;
          if (!owner || message.sessionID !== endpoint(owner)) { reply(409, { error: 'session changed' }); return; }
          if (typeof message.id !== 'string' || !message.id || typeof message.text !== 'string') { reply(400, { error: 'invalid message' }); return; }
          pi.appendEntry('ra2a-received', { id: message.id, text: message.text });
          if (!owner.sessionManager.getEntries().some(e => e.customType === 'ra2a-received' && e.data?.id === message.id)) throw new Error('receipt entry missing');
          reply(200, { status: 'received_by_bridge', id: message.id, sessionID: message.sessionID });
          setImmediate(() => {
            if (current !== owner) { owner.ui.notify(`RA2A ${message.id}: received but session closed before submission`, 'error'); return; }
            pi.sendUserMessage(message.text, { deliverAs: 'followUp', expandPromptTemplates: false });
          });
        } catch (e) { reply(500, { error: e.message }); }
      });
    });
    await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
    let renewalFailed = false;
    const renew = () => {
      try { writeLease(); renewalFailed = false; } catch (e) {
        // Discovery expires naturally; a lease lock must never terminate Pi.
        if (!renewalFailed) console.error(`RA2A lease renewal failed (${e.code}); discovery expires until renewal succeeds`);
        renewalFailed = true;
      }
    };
    renew();
    heartbeat = setInterval(renew, 1000);
  });
  pi.on('session_shutdown', stop);
  const call = async (route, body, signal) => {
    const response = await fetch(new URL(route, control), { method: body ? 'POST' : 'GET',
      headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined,
      signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(15000)]) : AbortSignal.timeout(15000), redirect: 'error' });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `RA2A HTTP ${response.status}`);
    return { content: [{ type: 'text', text: JSON.stringify(result) }], details: result };
  };
  pi.registerTool({ name: 'ra2a_list_targets', label: 'RA2A targets', description: 'List complete RA2A target addresses.',
    parameters: Type.Object({}), execute: async (_id, _params, signal) => call('/v1/targets', undefined, signal) });
  pi.registerTool({ name: 'ra2a_send_message', label: 'RA2A send', description: 'Send text to a complete RA2A address. Success is receipt, not task completion; never automatically retry unknown delivery.',
    parameters: Type.Object({ to: Type.String(), text: Type.String() }),
    execute: async (_id, params, signal, _update, ctx) => {
      if (!current || endpoint(ctx) !== endpoint(current)) throw new Error('Pi session is not attached');
      return call('/v1/send', { ...params, from: `ra2a://${nodeID}/${endpoint(ctx)}` }, signal);
    } });
}
