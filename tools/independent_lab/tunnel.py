"""Owned loopback access to an internal Docker network, with no target egress.

Docker internal bridges do not consistently publish ports. Each byte stream is
carried unchanged by docker exec into the fixture; TLS stays end to end.
"""
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import subprocess
import sys
import threading


def relay(host, port):
    if (host,port) not in [('engine',6662),('engine',6661),('fixture',9443)]:
        raise ValueError('undeclared tunnel target')
    with socket.create_connection((host,port),timeout=10) as target:
        target.settimeout(None)
        def upload():
            try:
                while data := os.read(0,65536): target.sendall(data)
                target.shutdown(socket.SHUT_RDWR)
            except OSError: pass
        threading.Thread(target=upload,daemon=True).start()
        while data := target.recv(65536):
            sys.stdout.buffer.write(data);sys.stdout.buffer.flush()


def serve(state, container):
    from .session import write
    control=state/'control/tunnel.sock'
    if control.exists():raise ValueError('tunnel controller already exists')
    listeners=[];children=set();clients=set();guard=threading.Lock();stopping=threading.Event()
    reverse=None;controller=None;control_identity=None;startup_complete=False;error_class=''
    reverse_expected=(state/'control/return-path.json').exists()
    select=selectors.DefaultSelector()
    def terminate(*_):
        signal.signal(signal.SIGTERM,signal.SIG_IGN);signal.signal(signal.SIGINT,signal.SIG_IGN)
        raise SystemExit(0)
    signal.signal(signal.SIGTERM,terminate);signal.signal(signal.SIGINT,terminate)
    def connection(client, host, port):
        child=None
        try:
            with guard:
                if stopping.is_set():return
                child=subprocess.Popen(['docker','exec','-i',container,'python','-m','independent_lab.tunnel','relay',host,str(port)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
                children.add(child);clients.add(client)
            def upload():
                try:
                    while data:=client.recv(65536):child.stdin.write(data);child.stdin.flush()
                    child.stdin.close()
                except (OSError,ValueError):pass
            threading.Thread(target=upload,daemon=True).start()
            while data:=os.read(child.stdout.fileno(),65536):client.sendall(data)
        except OSError:pass
        finally:
            client.close()
            if child:
                child.terminate();child.wait()
                with guard:children.discard(child);clients.discard(client)
    try:
        if reverse_expected:
            from . import return_path
            reverse=return_path.start(state,container)
        ports={}
        for host,port in [('engine',6661 if reverse else 6662),('fixture',9443)]:
            listener=socket.socket();listeners.append(listener)
            listener.bind(('127.0.0.1',0));listener.listen(32)
            ports['engine-v2v2' if port==6661 else host]=listener.getsockname()[1]
            select.register(listener,selectors.EVENT_READ,(host,port))
        controller=socket.socket(socket.AF_UNIX);controller.bind(str(control))
        info=control.lstat();control_identity=(info.st_dev,info.st_ino)
        os.chmod(control,0o600);controller.listen(1)
        select.register(controller,selectors.EVENT_READ,None)
        write(state/'control/tunnel.json',{'ports':ports,'transport':'docker-exec-loopback','container':container})
        startup_complete=True
        while True:
            for key,_ in select.select():
                client,_=key.fileobj.accept()
                if key.data is None:
                    client.settimeout(2)
                    try:command=client.recv(16)
                    except OSError:client.close();continue
                    if command==b'status':
                        client.sendall(json.dumps({'ready':reverse is None or reverse.poll() is None,'return_path':reverse is not None}).encode())
                        client.close();continue
                    client.close()
                    if command==b'':return
                    continue
                threading.Thread(target=connection,args=(client,*key.data),daemon=True).start()
    except BaseException as failure:
        error_class=type(failure).__name__
        raise
    finally:
        # Once cleanup starts, a repeated termination must not interrupt it.
        signal.signal(signal.SIGTERM,signal.SIG_IGN);signal.signal(signal.SIGINT,signal.SIG_IGN)
        for listener in listeners:listener.close()
        if controller:controller.close()
        select.close()
        with guard:
            stopping.set();pending=list(children)
            for client in clients:
                try:client.shutdown(socket.SHUT_RDWR)
                except OSError:pass
                client.close()
        for child in pending:
            child.terminate()
            try:child.wait(timeout=5)
            except subprocess.TimeoutExpired:child.kill();child.wait()
        reverse_stopped=not reverse_expected
        if reverse:
            reverse.terminate()
            try:reverse.wait(timeout=15)
            except subprocess.TimeoutExpired:reverse.kill();reverse.wait()
        if reverse_expected:
            receipt=state/'control/return-stopped.json'
            config=json.loads((state/'control/return-path.json').read_text())
            if receipt.exists():
                retained=json.loads(receipt.read_text())
                reverse_stopped=retained.get('generation')==config['generation'] and retained.get('remote_process_stopped') is True
        write(state/'control/tunnel-stopped.json',{'listeners_closed':reverse_stopped,'owned_relay_clients_stopped':len(pending),'startup_complete':startup_complete,'error_class':error_class})
        (state/'control/tunnel.json').unlink(missing_ok=True)
        if control_identity and control.exists():
            info=control.lstat()
            if (info.st_dev,info.st_ino)==control_identity:control.unlink()


def start(state, project):
    import time
    if (state/'control/tunnel.sock').exists():
        raise ValueError('existing tunnel requires explicit stop')
    (state/'control/tunnel-stopped.json').unlink(missing_ok=True)
    process=subprocess.Popen([sys.executable,'-m','independent_lab.tunnel','serve',str(state),project+'-fixture-1'],cwd=Path(__file__).resolve().parent.parent,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
    for _ in range(700):
        if (state/'control/tunnel.json').exists():return json.loads((state/'control/tunnel.json').read_text())['ports']
        if process.poll() is not None:raise ValueError('loopback tunnel failed to start')
        time.sleep(.05)
    process.terminate()
    try:process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid,signal.SIGKILL);process.wait()
    raise ValueError('loopback tunnel start timed out; owned child stopped')


def check(state):
    with socket.socket(socket.AF_UNIX) as client:
        client.settimeout(3);client.connect(str(state/'control/tunnel.sock'));client.sendall(b'status')
        data=b''
        while block:=client.recv(1024):
            data+=block
            if len(data)>1024:raise ValueError('invalid tunnel readiness response')
    if json.loads(data).get('ready') is not True:raise ValueError('owned return relay is unavailable')


def stop(state):
    import time
    path=state/'control/tunnel.sock'
    if path.exists():
        try:
            with socket.socket(socket.AF_UNIX) as client:client.connect(str(path))
        except ConnectionRefusedError:
            # No listener owns this private pathname. The operator lock excludes
            # a concurrent restart; unlink only this session's stale socket.
            import stat
            if not stat.S_ISSOCK(path.lstat().st_mode):raise ValueError('tunnel control is not a socket')
            path.unlink()
            (state/'control/tunnel.json').unlink(missing_ok=True)
            from .session import write
            write(state/'control/tunnel-stopped.json',{'listeners_closed':False,'state':'interrupted','stale_control_socket_removed':True})
            return
        for _ in range(400):
            if not path.exists():return
            time.sleep(.05)
        raise ValueError('loopback tunnel did not stop')


if __name__=='__main__':
    if sys.argv[1]=='relay':relay(sys.argv[2],int(sys.argv[3]))
    elif sys.argv[1]=='serve':serve(Path(sys.argv[2]),sys.argv[3])
    else:raise ValueError('unknown tunnel operation')
