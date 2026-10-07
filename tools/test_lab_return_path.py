"""The operator's return path is explicit and bounded before any effect."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class ReturnPathTests(unittest.TestCase):
    def test_public_or_ambiguous_return_destination_is_refused_before_start(self):
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary).resolve();(state/'control').mkdir()
            (state/'owner.json').write_text(json.dumps({'schema':'readmit-independent-lab/v1','project':'readmit-lab-'+'a'*16,'state':str(state)}))
            for host in ('1.1.1.1','0.0.0.0','localhost','127.0.0.1'):
                command=[sys.executable,str(Path(__file__).with_name('integration_lab.py')),'session-start',str(state),'--route','v2v2','--defect','field','--return-host',host,'--return-port','49355','--return-transport','mutual-tls']
                result=subprocess.run(command,capture_output=True,text=True)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('return path must name one private non-loopback IPv4 address',result.stderr)
                self.assertFalse((state/'control/session-intent.json').exists())

    def test_failed_docker_exec_does_not_claim_remote_shutdown_verified(self):
        import os
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary).resolve();(state/'control').mkdir();(state/'bin').mkdir()
            (state/'control/return-path.json').write_text(json.dumps({'host':'10.1.2.3','port':49355,'transport':'mutual-tls','generation':'lab-offline'}))
            docker=state/'bin/docker';docker.write_text('#!'+sys.executable+'\nraise SystemExit(7)\n');docker.chmod(0o700)
            subprocess.run([sys.executable,'-m','independent_lab.return_path','host',str(state),'owned-fixture'],env=dict(os.environ,PATH=str(state/'bin'),PYTHONPATH=str(Path(__file__).resolve().parent)),check=True,capture_output=True)
            receipt=json.loads((state/'control/return-stopped.json').read_text())
            self.assertFalse(receipt['remote_process_stopped'])
            self.assertEqual(receipt['remote_exit_code'],7)
            self.assertFalse((state/'control/return-ready.json').exists())

    def startup_fixture(self, state, bind_collision):
        import os
        (state/'control').mkdir();(state/'bin').mkdir()
        (state/'control/return-path.json').write_text(json.dumps({'host':'10.1.2.3','port':49355,'transport':'mutual-tls','generation':'lab-startup'}))
        script='#!'+sys.executable+'\nimport os,select,struct,time\nfrom pathlib import Path\nroot=Path('+repr(str(state)) +')\n(root/"spawned").write_text("started")\n'
        if bind_collision:
            script+='(root/"control/tunnel.sock").write_text("bystander")\nos.write(1,struct.pack("!cII",b"R",0,0))\n'
        script+='deadline=time.monotonic()+6\nwhile time.monotonic()<deadline:\n if select.select([0],[],[],.05)[0] and os.read(0,65536)==b"":\n  (root/"remote-exited").write_text("stdin closed");raise SystemExit(0)\nraise SystemExit(9)\n'
        docker=state/'bin/docker';docker.write_text(script);docker.chmod(0o700)
        return dict(os.environ,PATH=str(state/'bin'),PYTHONPATH=str(Path(__file__).resolve().parent))

    def test_startup_bind_failure_stops_owned_reverse_children_and_preserves_bystander(self):
        import time
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary).resolve();environment=self.startup_fixture(state,True)
            result=subprocess.run([sys.executable,'-m','independent_lab.tunnel','serve',str(state),'owned-fixture'],env=environment,capture_output=True,timeout=10)
            self.assertNotEqual(result.returncode,0)
            deadline=time.monotonic()+2
            while not (state/'remote-exited').exists() and time.monotonic()<deadline:time.sleep(.01)
            self.assertTrue((state/'remote-exited').exists(),'startup leaked the reverse child')
            self.assertEqual((state/'control/tunnel.sock').read_text(),'bystander')

    def test_termination_during_return_startup_stops_its_children(self):
        import time
        with tempfile.TemporaryDirectory() as temporary:
            state=Path(temporary).resolve();environment=self.startup_fixture(state,False)
            process=subprocess.Popen([sys.executable,'-m','independent_lab.tunnel','serve',str(state),'owned-fixture'],env=environment,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            try:
                deadline=time.monotonic()+3
                while not (state/'spawned').exists() and time.monotonic()<deadline:time.sleep(.01)
                self.assertTrue((state/'spawned').exists())
                process.terminate();process.wait(timeout=5)
                self.assertTrue((state/'remote-exited').exists(),'termination leaked the reverse child')
                self.assertFalse((state/'control/return-ready.json').exists())
            finally:
                if process.poll() is None:process.kill();process.wait()

    def test_owned_reverse_stream_preserves_bytes_bidirectionally(self):
        import socket
        import threading
        from independent_lab.return_path import bridge
        carrier_a,carrier_b=socket.socketpair()
        relay=socket.socket();relay.bind(('127.0.0.1',0));relay.listen(4)
        destination=socket.socket();destination.bind(('127.0.0.1',0));destination.listen(4)
        target=destination.getsockname();source=relay.getsockname()
        streams=[carrier_a.makefile('rb'),carrier_a.makefile('wb'),carrier_b.makefile('rb'),carrier_b.makefile('wb')]
        errors=[]
        def run(*args,**kwargs):
            try:bridge(*args,**kwargs)
            except OSError:pass
            except Exception as error:errors.append(error)
        remote=threading.Thread(target=run,args=(streams[0],streams[1]),kwargs={'listener':relay},daemon=True)
        host=threading.Thread(target=run,args=(streams[2],streams[3]),kwargs={'target':target},daemon=True)
        remote.start();host.start()
        try:
            for value in (b'opaque TLS bytes\x00\xff',b'next independent connection'):
                with socket.create_connection(source,timeout=2) as engine:
                    destination.settimeout(2)
                    product,_=destination.accept()
                    with product:
                        product.settimeout(2)
                        engine.sendall(value);self.assertEqual(product.recv(100),value)
                        product.sendall(b'unchanged return '+value);self.assertEqual(engine.recv(100),b'unchanged return '+value)
            self.assertEqual(errors,[])
        finally:
            for carrier in (carrier_a,carrier_b):
                try:carrier.shutdown(socket.SHUT_RDWR)
                except OSError:pass
            remote.join(2);host.join(2)
            for stream in streams:
                try:stream.close()
                except OSError:pass
            carrier_a.close();carrier_b.close();destination.close()
            self.assertFalse(remote.is_alive());self.assertFalse(host.is_alive())


if __name__=='__main__':unittest.main()
