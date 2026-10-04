# Linux/macOS native PTY fixture. Uses only an isolated profile and fake history.
import os,pty,fcntl,termios,struct,subprocess,pathlib,json,select,time,uuid,urllib.request
binary,extension,tmp=map(pathlib.Path,__import__('sys').argv[1:])
log=tmp/'events.jsonl';session=tmp/'A.jsonl';aid=str(uuid.uuid4())
session.write_text(json.dumps({'type':'session','version':3,'id':aid,'timestamp':'2026-10-05T00:00:00.000Z','cwd':str(tmp)})+'\n'+json.dumps({'type':'message','id':'abcdef12','parentId':None,'timestamp':'2026-10-05T00:00:00.000Z','message':{'role':'user','content':[{'type':'text','text':'isolated history fixture'}],'timestamp':1791158400000}})+'\n')
helper=tmp/'helper.mjs';helper.write_text("import fs from 'node:fs';export default function(pi){pi.on('session_start',(e,c)=>fs.appendFileSync(process.env.PROBE_LOG,JSON.stringify({reason:e.reason,id:c.sessionManager.getSessionId()})+'\\n'));pi.registerCommand('probe-new',{handler:async(_,c)=>{await c.newSession();}});pi.registerCommand('probe-resume',{handler:async(_,c)=>{await c.switchSession(process.env.PROBE_SESSION);}});}")
env=dict(os.environ);env.update({'PI_OFFLINE':'1','RA2A_PI_NODE_ID':'fixture-node','PROBE_LOG':str(log),'PROBE_SESSION':str(session),'TERM':'xterm-256color'})
m,s=pty.openpty();fcntl.ioctl(s,termios.TIOCSWINSZ,struct.pack('HHHH',30,120,0,0))
p=subprocess.Popen([str(binary),'--offline','--no-skills','--no-prompt-templates','--no-context-files','--no-approve','--extension',str(helper),'--session',str(session)],cwd=tmp,env=env,stdin=s,stdout=s,stderr=s,start_new_session=True);os.close(s);buf=b''
leasefile=pathlib.Path(env['RA2A_PI_SESSION_DIR'])/f'attachment.{p.pid}.json'
def drain(seconds):
 global buf
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  if select.select([m],[],[],0.05)[0]:
   try:b=os.read(m,65536)
   except OSError:break
   buf+=b
   if b'\x1b[6n' in b:os.write(m,b'\x1b[1;1R')
def lease():return json.loads(leasefile.read_text())
def send(record,marker,target=None):
 request=urllib.request.Request(record['url']+'/receive',data=json.dumps({'sessionID':target or record['sessionID'],'id':marker,'text':'[RA2A message]\nmessage-id: '+marker+'\n\nNative TUI receipt'}).encode(),headers={'Authorization':'Bearer '+record['token']},method='POST')
 with urllib.request.urlopen(request,timeout=2) as reply:return json.load(reply)
try:
 drain(2);a=lease();assert a['sessionID']=='pi.'+aid
 assert send(a,'TUI_01')['status']=='received_by_bridge';drain(.5)
 os.write(m,b'/probe-new\r');drain(1);b=lease();assert b['sessionID']!=a['sessionID']
 try:send(b,'OLD_TARGET',a['sessionID']);raise AssertionError('old target accepted')
 except urllib.error.HTTPError as e:assert e.code==409
 os.write(m,b'/probe-resume\r');drain(1);resumed=lease();assert resumed['sessionID']==a['sessionID']
 assert send(resumed,'TUI_02')['status']=='received_by_bridge';drain(.5)
 assert b'RA2A received:' in buf and b'TUI_01' in buf and b'TUI_02' in buf,'native receipt not rendered'
 entries=[json.loads(line) for line in session.read_text().splitlines()];receipts=[e for e in entries if e.get('customType')=='ra2a-received']
 assert [r['data']['id'] for r in receipts]==['TUI_01','TUI_02']
 print(json.dumps({'pass':True,'nativePi':'1.0.0','resume':True,'receiptRendered':True,'wrongTargetRejected':True,'receiptsInOriginalHistory':2,'pid':p.pid}))
finally:
 p.terminate();p.wait(timeout=5);os.close(m);(tmp/'terminal.log').write_bytes(buf)
# An abrupt native exit withdraws its record after the documented lease timeout.
time.sleep(3.1)
if leasefile.exists():assert lease()['expires']<int(time.time()*1000)
