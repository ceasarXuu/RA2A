"""No credentials or model calls. Includes opt-in real namespace isolation."""
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


spec = importlib.util.spec_from_file_location('runtime', Path(__file__).with_name('runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class RuntimeTests(unittest.TestCase):
    def test_individual_worker_mount_does_not_expose_parent(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            worker = root/'worker'
            worker.write_text('fake')
            r = runtime.Runtime(root, {'worker': str(worker)}, {}, 'host')
            self.assertEqual(r._binary_mounts(), [str(worker)])

    def test_codex_mount_selects_only_fixed_complete_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            release = root/'.codex/packages/app-server-daemon/releases/0.1-test'
            (release/'bin').mkdir(parents=True)
            binary = release/'bin/codex'
            binary.write_text('native fixture')
            (release/'codex-package.json').write_text('{}')
            (release/'codex-resources').mkdir()
            (release/'codex').symlink_to('bin/codex')
            r = runtime.Runtime(root, {'codex': str(binary)}, {}, 'host')
            self.assertEqual(r._binary_mounts(), [str(release)])
            (root/'.codex/auth.json').write_text('outside fixture credential')
            self.assertEqual(r._binary_mounts(), [str(release)])
            (release/'codex-resources/escape').symlink_to(root/'.codex/auth.json')
            with self.assertRaisesRegex(ValueError, 'escapes'):
                r._binary_mounts()
            (release/'codex-resources/escape').unlink()
            (release/'bin/auth.json').write_text('inside fixture credential')
            with self.assertRaisesRegex(ValueError, 'credentials'):
                r._binary_mounts()

    def test_host_network_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            r = runtime.Runtime(Path(directory), {}, {}, os.readlink('/proc/self/ns/net'))
            with self.assertRaisesRegex(RuntimeError, 'private outer'):
                r.start()

    def test_supervise_natural_exit_keeps_logs_and_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            profile = root/'auth-fixture'
            profile.write_text('synthetic credential')
            with (root/'stdout').open('wb') as out, (root/'stderr').open('wb') as err:
                result = runtime.supervise(['/bin/sh', '-c', 'echo LOG_OK'],
                                           {'PATH': '/usr/bin:/bin'}, 2, out, err)
            self.assertEqual(result['returncode'], 0)
            self.assertFalse(result['timed_out'])
            self.assertEqual(profile.read_text(), 'synthetic credential')
            self.assertIn('LOG_OK', (root/'stdout').read_text())

    def test_supervise_manual_controller_can_receive_input(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'input').write_text('PASS\n')
            with (root/'input').open('rb') as source, (root/'output').open('wb') as out:
                result = runtime.supervise(['/bin/sh', '-c', 'read answer; test "$answer" = PASS'],
                                           {'PATH': '/usr/bin:/bin'}, 2, out, out, stdin=source)
            self.assertEqual(result['returncode'], 0)

    @unittest.skipUnless(os.environ.get('RA2A_TEST_LINUX_RUNTIME') == '1', 'opt-in namespace fixture')
    def test_supervise_timeout_terminates_namespace_descendants(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            # A namespace-local setsid process deliberately ignores group TERM;
            # killing namespace PID1 must still terminate this descendant.
            code = ('import os,subprocess,time; '
                    'subprocess.Popen(["python3","-c",'
                    '"import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(30)"],'
                    'start_new_session=True); '
                    'print(os.readlink("/proc/self/ns/pid"),flush=True); time.sleep(30)')
            with (root/'stdout').open('wb') as out, (root/'stderr').open('wb') as err:
                result = runtime.supervise(
                    ['unshare', '--user', '--map-root-user', '--pid', '--fork', '--mount-proc',
                     'python3', '-c', code], {'PATH': '/usr/bin:/bin'}, .5, out, err)
            self.assertTrue(result['timed_out'])
            namespace = (root/'stdout').read_text().strip()
            self.assertTrue(namespace.startswith('pid:['), (root/'stderr').read_text())
            # Namespace references disappear when its PID1 and descendants die;
            # host init may briefly retain an exited zombie (which holds no ns).
            deadline = __import__('time').monotonic()+3
            while True:
                matching = []
                for path in Path('/proc').glob('[0-9]*/ns/pid'):
                    try:
                        if os.readlink(path) == namespace:
                            matching.append(str(path))
                    except (FileNotFoundError, PermissionError):
                        pass
                if not matching or __import__('time').monotonic()>deadline:
                    break
                __import__('time').sleep(.05)
            self.assertEqual(matching, [])

    @unittest.skipUnless(os.environ.get('RA2A_TEST_LINUX_RUNTIME') == '1', 'opt-in namespace fixture')
    def test_native_namespace_pty_and_cleanup(self):
        source = str(Path(__file__).with_name('runtime.py').parent.resolve())
        code = r'''import os,sys,tempfile,pathlib,ctypes
sys.path.insert(0,sys.argv[1])
from runtime import Runtime
with tempfile.TemporaryDirectory(prefix='ra2a-runtime-test-') as directory:
 root=pathlib.Path(directory); profiles={}
 for k in ('codex','opencode','pi'):
  home=root/k/'home'; home.mkdir(parents=True); profiles[k]={'home':str(home)}
 ctypes.CDLL(None).prctl(4,0)
 with Runtime(root,{},profiles,sys.argv[2]) as r:
  uds=r.spawn('pi',['python3','-c',
   'import socket,pathlib; s=socket.socket(socket.AF_UNIX); s.bind("/tmp/native-fixture.sock"); '
   's.listen(1); pathlib.Path("/home/sandbox/control.sock").symlink_to("/tmp/native-fixture.sock"); '
   'c,_=s.accept(); c.sendall(b"UDS_SYMLINK_OK"); c.close(); s.close()'])
  deadline=time.monotonic()+3
  while not (root/'pi/home/control.sock').is_symlink():
   assert time.monotonic()<deadline; time.sleep(.01)
  client=r.spawn('pi',['python3','-c',
   'import socket; s=socket.socket(socket.AF_UNIX); s.connect("/home/sandbox/control.sock"); '
   'assert s.recv(32)==b"UDS_SYMLINK_OK"; print("SHARED_PRIVATE_TMP_OK")'],pty=True)
  udsproof=b''
  while client.poll() is None: udsproof+=client.read(.1)
  udsproof+=client.read(.1)
  assert client.poll()==0 and b'SHARED_PRIVATE_TMP_OK' in udsproof, udsproof
  assert not pathlib.Path('/tmp/native-fixture.sock').exists()
  other_tmp=r.spawn('codex',['/bin/sh','-c','test ! -e /tmp/native-fixture.sock'],pty=True)
  other_tmp.process.wait(timeout=2); assert other_tmp.poll()==0
  budget=root/'private-budget'; budget.write_text('FAKE_CONTROLLER_SECRET')
  node=r.spawn('pi',['python3','-c',
   'import ctypes,os,pathlib,time; ctypes.CDLL(None).prctl(4,0,0,0,0); '
   'pathlib.Path("/home/sandbox/node-pid").write_text(str(os.getpid())); '
   'pathlib.Path("/home/sandbox/node-pidns").write_text(os.readlink("/proc/self/ns/pid")); time.sleep(30)'],
   mounts=[(str(budget),'/run/budget.sock',True)])
  daemon=r.spawn('pi',['python3','-c',
   'import pathlib,os,time; pathlib.Path("/home/sandbox/daemon-pid").write_text(str(os.getpid())); time.sleep(30)'])
  other=r.spawn('codex',['python3','-c','import time; time.sleep(30)','OTHER_KIND_SENTINEL'])
  deadline=time.monotonic()+3
  while not (root/'pi/home/node-pid').exists() or not (root/'pi/home/daemon-pid').exists():
   assert time.monotonic()<deadline; time.sleep(.01)
  check=('import os,pathlib; '
   'p=pathlib.Path("/home/sandbox/node-pid").read_text(); '
   'daemon=pathlib.Path("/home/sandbox/daemon-pid").read_text(); '
   'assert pathlib.Path(os.readlink("/proc/"+daemon+"/exe")).name.startswith("python3"); '
   'assert pathlib.Path("/home/sandbox/node-pidns").read_text()==os.readlink("/proc/self/ns/pid"), "node namespace mismatch"; '
   'assert not any(b"OTHER_KIND_SENTINEL" in f.read_bytes() for f in pathlib.Path("/proc").glob("[0-9]*/cmdline") if f.parent.name not in (str(os.getpid()), str(os.getppid()))), "other kind visible"; '
   'paths=["/proc/1/root","/proc/"+p+"/root/run/budget.sock"]; '
   '\nfor value in paths:\n try: pathlib.Path(value).stat(); raise AssertionError("protected proc accessible")\n except PermissionError: pass\n'
   'print("PID_BOUNDARIES_OK")')
  probe=r.spawn('pi',['python3','-c',check],pty=True)
  proof=b''
  while probe.poll() is None: proof+=probe.read(.1)
  proof+=probe.read(.1)
  assert probe.poll()==0 and b'PID_BOUNDARIES_OK' in proof, proof
  p=r.spawn('pi',['/bin/sh','-c','test ! -e /home/zhangxu; echo PRIVATE_RUNTIME_OK; stty size; ip route'],pty=True)
  out=b''
  while p.poll() is None: out+=p.read(.1)
  out+=p.read(.1)
  assert p.poll()==0 and b'PRIVATE_RUNTIME_OK' in out and b'default' not in out and b'30 120' in out
 assert not r.processes and not r.children
 assert subprocess.run(['ip','link','show','matrix-br'],capture_output=True).returncode!=0
 print('namespace, PTY, private HOME, no default route and cleanup verified')
'''.replace('import os,sys,tempfile,pathlib,ctypes', 'import os,sys,tempfile,pathlib,ctypes,subprocess,time')
        env = {'PATH': '/usr/sbin:/usr/bin:/bin'}
        result = subprocess.run(['unshare', '--user', '--map-root-user', '--net', '--pid',
                                 '--fork', '--mount-proc', 'python3', '-c', code,
                                 source, os.readlink('/proc/self/ns/net')],
                                capture_output=True, text=True, env=env, timeout=20)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('cleanup verified', result.stdout)


if __name__ == '__main__':
    unittest.main()
