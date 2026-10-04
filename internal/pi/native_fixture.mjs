// Test fixture only: real Pi SDK and bridge, fake model stream and control.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import { pathToFileURL } from 'node:url';
const [pkg, extension] = process.argv.slice(2);
const sdk = await import(pathToFileURL(path.join(pkg, 'dist/index.js')).href);
const { discoverAndLoadExtensions } = await import(pathToFileURL(path.join(pkg, 'dist/core/extensions/loader.js')).href);
const { createAssistantMessageEventStream } = await import(pathToFileURL(path.join(pkg, 'node_modules/@earendil-works/pi-ai/dist/utils/event-stream.js')).href);
const requests = [];
const control = http.createServer(async (req, res) => {
  let body = ''; for await (const chunk of req) body += chunk;
  requests.push({ url: req.url, body: body ? JSON.parse(body) : null });
  res.setHeader('Content-Type', 'application/json');res.end(JSON.stringify(req.url === '/v1/targets' ? { targets: [] } : { status: 'accepted' }));
});
await new Promise(resolve => control.listen(0, '127.0.0.1', resolve));
process.env.RA2A_CONTROL_URL = `http://127.0.0.1:${control.address().port}`;
process.env.RA2A_PI_NODE_ID = 'fixture-node';
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const errors = [];
const sessions = [];
const modelRuntime = await sdk.ModelRuntime.create({ authPath: path.join(process.env.HOME, 'auth.json'), modelsPath: null,
  modelsStorePath: path.join(process.env.HOME, 'models-cache.json'), allowModelNetwork: false, refreshOnCreate: false });
async function open(manager) {
  const result = await discoverAndLoadExtensions([], process.cwd(), process.env.PI_CODING_AGENT_DIR, sdk.createEventBus());
  assert.equal(result.errors.length, 0, JSON.stringify(result.errors));
  assert.equal(result.extensions.length, 1, 'installed bridge must be auto-discovered');
  const loader = { getExtensions: () => result, getSkills: () => ({ skills: [], diagnostics: [] }),
    getPrompts: () => ({ prompts: [], diagnostics: [] }), getThemes: () => ({ themes: [], diagnostics: [] }),
    getAgentsFiles: () => ({ agentsFiles: [] }), getSystemPrompt: () => '', getSystemPromptSource: () => undefined,
    getAppendSystemPrompt: () => [], getAppendSystemPromptSources: () => [], extendResources: () => {}, reload: async () => {} };
  const { session } = await sdk.createAgentSession({ cwd: process.cwd(), agentDir: process.env.PI_CODING_AGENT_DIR,
    modelRuntime, resourceLoader: loader, sessionManager: manager,
    settingsManager: sdk.SettingsManager.inMemory({ compaction: { enabled: false }, retry: { enabled: false } }), tools: [] });
  await session.bindExtensions({ onError: e => errors.push(e) });
  sessions.push(session);
  return session;
}
function lease() {
  return JSON.parse(fs.readFileSync(path.join(process.env.RA2A_PI_SESSION_DIR, `attachment.${process.pid}.json`), 'utf8'));
}
async function send(record, id, token = record.token, sessionID = record.sessionID) {
  const started = Date.now();
  const response = await fetch(record.url + '/receive', { method: 'POST', headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ sessionID, id, text: `[RA2A message]\nmessage-id: ${id}\n\nfixture` }) });
  return { status: response.status, value: await response.json(), elapsed: Date.now() - started };
}
let release;
try {
  const manager = sdk.SessionManager.create(process.cwd(), path.join(process.env.HOME, 'sessions'));
  const a = await open(manager), initial = lease();
  assert.equal(initial.sessionID, `pi.${a.sessionId}`);
  // Unauthorized and switch races must not create receipt entries.
  assert.equal((await send(initial, 'unauthorized', 'wrong')).status, 403);
  assert.equal((await send(initial, 'wrong-session', initial.token, 'pi.wrong')).status, 409);
  assert.equal(manager.getEntries().filter(e => e.customType === 'ra2a-received').length, 0);
  if (process.argv[4] === 'external') {
    console.log(JSON.stringify({ready:true}));
    await new Promise(resolve=>process.stdin.once('data',resolve));
    process.stdin.pause();
    assert(manager.getEntries().some(e=>e.customType==='ra2a-received'&&e.data.id==='go-adapter'));
  }
  // Receipt is a host extension boundary, even if native input later fails auth.
  assert.equal((await send(initial, 'no-auth')).value.status, 'received_by_bridge');
  await pause(100);
  assert(errors.some(e => e.event === 'send_user_message'));
  assert.equal(a.messages.filter(e => e.role === 'user').length, 0);
  assert(manager.getEntries().some(e => e.customType === 'ra2a-received' && e.data.id === 'no-auth'));
  modelRuntime.registerProvider('fixture', { api: 'openai-completions', baseUrl: 'http://127.0.0.1:1', apiKey: 'isolated-fixture-key',
    models: [{id:'fixture',name:'fixture',reasoning:false,input:['text'],cost:{input:0,output:0,cacheRead:0,cacheWrite:0},contextWindow:100000,maxTokens:100}] });
  await a.setModel(modelRuntime.getModel('fixture','fixture'));
  let held = false, calls = 0;
  a.agent.streamFunction = model => {
    const stream = createAssistantMessageEventStream();calls++;
    const message = { role: 'assistant', api: model.api, provider: model.provider, model: model.id,
      content: [{ type: 'text', text: 'fixture done' }], usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: 'stop', timestamp: Date.now() };
    stream.push({ type: 'start', partial: message });
    (async () => { if (held) {held=false; await new Promise(resolve => {release=resolve;});}
      stream.push({ type: 'done', reason: 'stop', message });stream.end(message); })();
    return stream;
  };
  for (let round = 1; round <= 22; round++) {
    const id = `round-${round}`;
    assert.equal((await send(lease(), id)).value.id, id);
    await pause(5);await a.waitForIdle();
    assert(a.messages.some(e => e.role === 'user' && e.content.some(p => p.text?.includes(`message-id: ${id}\n`))),JSON.stringify({calls,errors,messages:a.messages}));
  }
  held=true;
  const base=a.prompt('held work');
  for(let i=0;i<100 && !release;i++)await pause(5);
  assert(release,'fake model did not enter held state');
  const before=calls;
  const active=await send(lease(),'while-busy');
  assert.equal(active.value.status,'received_by_bridge');assert(active.elapsed<1000);
  assert.equal(calls,before,'receipt waited for or restarted model');
  await pause(10);assert(a.pendingMessageCount>0); // native followUp is queued independently.
  release();await base;await a.waitForIdle();
  assert(a.messages.some(e=>e.role==='user'&&e.content.some(p=>p.text?.includes('message-id: while-busy'))));
  // Outbound tools use current native context, never a caller-selected from.
  const tools=a.extensionRunner.getAllRegisteredTools().map(t=>t.definition);
  const listing=tools.find(t=>t.name==='ra2a_list_targets');
  const sender=tools.find(t=>t.name==='ra2a_send_message');
  assert(listing&&sender);
  await listing.execute('list',{},undefined,undefined,a.extensionRunner.createContext());
  await sender.execute('send',{to:'ra2a://peer/target',text:'test'},undefined,undefined,a.extensionRunner.createContext());
  assert.equal(requests.at(-1).body.from,`ra2a://fixture-node/${initial.sessionID}`);
  assert(!Object.hasOwn(requests.at(-1).body,'token'));
  // shutdown removes routing rights; resume reconstructs same native ID.
  await a.extensionRunner.emit({type:'session_shutdown',reason:'resume'});
  assert(!fs.existsSync(path.join(process.env.RA2A_PI_SESSION_DIR,`attachment.${process.pid}.json`)));
  const b=await open(sdk.SessionManager.create(process.cwd(),path.join(process.env.HOME,'sessions')));
  assert.notEqual(lease().sessionID,initial.sessionID);
  const stale=await send(lease(),'old-target',lease().token,initial.sessionID);assert.equal(stale.status,409);
  await b.extensionRunner.emit({type:'session_shutdown',reason:'resume'});
  const resumed=await open(sdk.SessionManager.open(manager.getSessionFile()));
  assert.equal(lease().sessionID,initial.sessionID);
  console.log(JSON.stringify({pass:true,nativeVersion:sdk.VERSION,receipt22:22,heldReceiptMS:active.elapsed,
    resumeID:resumed.sessionId,rawUserMessages:a.messages.filter(e=>e.role==='user').length,authFailureReported:true,sourceExact:true}));
} finally {
  for(const session of sessions){await session.extensionRunner.emit({type:'session_shutdown',reason:'exit'});session.dispose();}
  control.closeAllConnections();await new Promise(resolve=>control.close(resolve));
}
