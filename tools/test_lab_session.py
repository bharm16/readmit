"""Operator lifecycle safety at the public command boundary (no live lab)."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class SessionControllerTests(unittest.TestCase):
    def test_stale_controller_cannot_reset_owned_generation(self):
        with tempfile.TemporaryDirectory() as root:
            state = Path(root).resolve()
            (state / 'owner.json').write_text(json.dumps({'schema': 'readmit-independent-lab/v1', 'project': 'readmit-lab-' + 'a' * 16, 'state': str(state)}))
            (state / 'control').mkdir()
            (state / 'control/session.json').write_text(json.dumps({'schema': 'readmit-lab-session/v1', 'generation': 'lab-current', 'status': 'ready'}))
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('integration_lab.py')), 'session-reset', str(state), '--generation', 'lab-stale'], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('stale session generation', result.stderr)
            self.assertEqual(json.loads((state / 'control/session.json').read_text())['status'], 'ready')

    def test_concurrent_controller_is_refused_before_target_effects(self):
        import fcntl
        with tempfile.TemporaryDirectory() as root:
            state = Path(root).resolve()
            (state / 'owner.json').write_text(json.dumps({'schema':'readmit-independent-lab/v1','project':'readmit-lab-'+'b'*16,'state':str(state)}))
            (state / 'control').mkdir()
            with (state / 'control/session.lock').open('w') as held:
                fcntl.flock(held, fcntl.LOCK_EX)
                result = subprocess.run([sys.executable, str(Path(__file__).with_name('integration_lab.py')), 'session-start', str(state)], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('another session controller is active', result.stderr)
                self.assertFalse((state/'control/session.json').exists())


    def test_reference_controller_cannot_replace_interrupted_session(self):
        with tempfile.TemporaryDirectory() as root:
            state=Path(root).resolve()
            (state/'control').mkdir()
            (state/'owner.json').write_text(json.dumps({'schema':'readmit-independent-lab/v1','project':'readmit-lab-'+'c'*16,'state':str(state)}))
            (state/'control/session-intent.json').write_text('{}')
            for action in ('up','qualify','down'):
                result=subprocess.run([sys.executable,str(Path(__file__).with_name('integration_lab.py')),action,str(state)],capture_output=True,text=True)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('owned session requires explicit',result.stderr)


    def test_export_records_pending_transition_as_interrupted(self):
        for initial in (False,True):
            with self.subTest(initial=initial), tempfile.TemporaryDirectory() as root:
                state=Path(root).resolve()
                for name in ('control','evidence','session-evidence','secrets'):(state/name).mkdir()
                (state/'owner.json').write_text(json.dumps({'schema':'readmit-independent-lab/v1','project':'readmit-lab-'+'d'*16,'state':str(state)}))
                (state/'lab.env').write_text('LAB_DB_PASSWORD=private-test-password')
                if not initial:(state/'evidence/runtime.json').write_text('{}')
                (state/'control/session-intent.json').write_text(json.dumps({'generation':'lab-next','action':'start' if initial else 'revision'}))
                if not initial:
                    (state/'control/session.json').write_text(json.dumps({'schema':'readmit-lab-session/v1','generation':'lab-old','status':'ready'}))
                result=subprocess.run([sys.executable,str(Path(__file__).with_name('integration_lab.py')),'session-export',str(state),'--generation','lab-next' if initial else 'lab-old'],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                exported=json.loads((state/'session-export/session-state.json').read_text())
                self.assertEqual(exported['status'],'interrupted')
                self.assertEqual(exported['intent']['generation'],'lab-next')

    def test_host_stop_does_not_upgrade_a_crashed_tunnel_receipt(self):
        import os
        import socket
        with tempfile.TemporaryDirectory() as root:
            state=Path(root).resolve()
            generation='lab-stopping'
            for name in ('control','session-evidence/'+generation,'bin'):(state/name).mkdir(parents=True)
            scripts=Path(__file__).resolve().parent
            (state/'owner.json').write_text(json.dumps({'schema':'readmit-independent-lab/v1','project':'readmit-lab-'+'e'*16,'state':str(state),'config_root':str(scripts/'independent_lab')}))
            (state/'control/session.json').write_text(json.dumps({'schema':'readmit-lab-session/v1','generation':generation,'status':'ready'}))
            with socket.socket(socket.AF_UNIX) as listener:listener.bind(str(state/'control/tunnel.sock'))
            # Docker is the external boundary: its completed worker has quiesced
            # the target. Host listener cleanup still must not claim success.
            docker=state/'bin/docker'
            docker.write_text('#!'+sys.executable+'\nimport json\nfrom pathlib import Path\np=Path('+repr(str(state/'control/session.json'))+')\nv=json.loads(p.read_text());v["status"]="stopping";p.write_text(json.dumps(v))\n')
            docker.chmod(0o700)
            result=subprocess.run([sys.executable,str(scripts/'integration_lab.py'),'session-stop',str(state),'--generation',generation],env=dict(os.environ,PATH=str(state/'bin')),capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(json.loads((state/'control/session.json').read_text())['status'],'interrupted')

    def test_tunnel_stop_closes_live_stream_before_completion_receipt(self):
        import os
        import socket
        import time
        with tempfile.TemporaryDirectory() as root:
            state=Path(root).resolve();(state/'control').mkdir();(state/'bin').mkdir()
            docker=state/'bin/docker'
            docker.write_text('#!'+sys.executable+'\nimport os\nwhile data:=os.read(0,65536):os.write(1,data)\n')
            docker.chmod(0o700)
            process=subprocess.Popen([sys.executable,'-m','independent_lab.tunnel','serve',str(state),'owned-fixture'],env=dict(os.environ,PATH=str(state/'bin'),PYTHONPATH=str(Path(__file__).resolve().parent)),stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
            try:
                deadline=time.monotonic()+5
                while not (state/'control/tunnel.json').exists():
                    if time.monotonic()>deadline:self.fail('tunnel did not start')
                    time.sleep(.01)
                port=json.loads((state/'control/tunnel.json').read_text())['ports']['engine']
                from independent_lab import tunnel
                tunnel.check(state)
                self.assertIsNone(process.poll())
                with socket.create_connection(('127.0.0.1',port),timeout=5) as client:
                    client.sendall(b'original stream bytes');self.assertEqual(client.recv(100),b'original stream bytes')
                    with socket.socket(socket.AF_UNIX) as control:control.connect(str(state/'control/tunnel.sock'))
                    self.assertEqual(client.recv(100),b'')
                self.assertEqual(process.wait(timeout=10),0)
                receipt=json.loads((state/'control/tunnel-stopped.json').read_text())
                self.assertTrue(receipt['listeners_closed'])
                with self.assertRaises(OSError):socket.create_connection(('127.0.0.1',port),timeout=1)
            finally:
                if process.poll() is None:process.terminate();process.wait(timeout=5)
                process.stderr.close()

    def test_interrupted_tunnel_stop_releases_only_its_stale_control_socket(self):
        import socket
        from independent_lab import tunnel
        with tempfile.TemporaryDirectory() as root:
            state=Path(root);(state/'control').mkdir()
            stale=state/'control/tunnel.sock'
            with socket.socket(socket.AF_UNIX) as listener:listener.bind(str(stale))
            unrelated=state/'control/unrelated';unrelated.write_text('keep')
            tunnel.stop(state)
            self.assertFalse(stale.exists())
            self.assertEqual(unrelated.read_text(),'keep')
            receipt=json.loads((state/'control/tunnel-stopped.json').read_text())
            self.assertEqual(receipt['state'],'interrupted')
            self.assertFalse(receipt['listeners_closed'])


    def test_offline_export_rejects_changed_and_added_acquisitions(self):
        import hashlib
        with tempfile.TemporaryDirectory() as root:
            export = Path(root)
            data = b'{"status":"interrupted"}\n'
            (export/'session-state.json').write_bytes(data)
            (export/'manifest.json').write_text(json.dumps({'schema':'readmit-lab-session-export/v1','state':'interrupted','files':{'session-state.json':{'sha256':hashlib.sha256(data).hexdigest(),'bytes':len(data)}}}))
            command=[sys.executable,str(Path(__file__).with_name('integration_lab.py')),'session-verify',str(export)]
            self.assertEqual(subprocess.run(command,capture_output=True).returncode,0)
            (export/'session-state.json').write_bytes(b'{"status":"passed"}\n')
            result=subprocess.run(command,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('session export member changed',result.stderr)
            (export/'session-state.json').write_bytes(data)
            (export/'extra.json').write_text('{}')
            self.assertNotEqual(subprocess.run(command,capture_output=True).returncode,0)



if __name__ == '__main__':
    unittest.main()
