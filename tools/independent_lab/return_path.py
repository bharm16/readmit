"""Owned opaque byte return path from an internal Docker lab to one private host.

The relay neither terminates TLS nor creates/changes HL7 or ACK bytes. Docker
exec's stdio carries bounded multiplexed frames; only the recorded host/port is
reachable. Product listener readiness and TLS verification happen at execution.
"""
import ipaddress
import json
import os
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import threading
import time

HEADER=struct.Struct('!cII')
PRIVATE=[ipaddress.ip_network(value) for value in ('10.0.0.0/8','172.16.0.0/12','192.168.0.0/16')]


def validate(host,port,transport):
    try:address=ipaddress.ip_address(host)
    except ValueError:address=None
    if not isinstance(address,ipaddress.IPv4Address) or not any(address in network for network in PRIVATE):
        raise ValueError('return path must name one private non-loopback IPv4 address')
    if type(port) is not int or not 1024<=port<=65535 or transport not in ('plain','tls','mutual-tls'):
        raise ValueError('return path needs an explicit port and plain, tls or mutual-tls transport')
    return {'host':str(address),'port':port,'transport':transport,'server_name':'localhost','route':'owned-docker-exec-reverse','internal_host':'fixture','internal_port':7001}


def send(stream,guard,kind,identifier,payload=b''):
    if kind not in (b'O',b'D',b'C',b'R') or len(payload)>65536:raise ValueError('invalid relay frame')
    with guard:
        stream.write(HEADER.pack(kind,identifier,len(payload))+payload);stream.flush()


def frames(stream):
    def take(size):
        result=b''
        while len(result)<size:
            part=stream.read(size-len(result))
            if not part:
                if not result:return None
                raise ValueError('truncated relay frame')
            result+=part
        return result
    while True:
        header=take(HEADER.size)
        if header is None:return
        kind,identifier,length=HEADER.unpack(header)
        if kind not in (b'O',b'D',b'C',b'R') or length>65536:raise ValueError('invalid relay frame')
        payload=take(length) if length else b''
        if payload is None:raise ValueError('truncated relay frame')
        yield kind,identifier,payload


def bridge(incoming,outgoing,listener=None,target=None,ready=None):
    """Carry concurrent byte streams, with bounded active connections."""
    sockets={};guard=threading.RLock();writer=threading.Lock();stopping=threading.Event()
    def close(identifier):
        with guard:connection=sockets.pop(identifier,None)
        if connection:
            try:connection.shutdown(socket.SHUT_RDWR)
            except OSError:pass
            connection.close()
    def read(connection,identifier):
        try:
            while data:=connection.recv(65536):send(outgoing,writer,b'D',identifier,data)
        except (OSError,ValueError):pass
        finally:
            close(identifier)
            if not stopping.is_set():
                try:send(outgoing,writer,b'C',identifier)
                except (OSError,ValueError):pass
    def register(connection,identifier):
        with guard:
            if stopping.is_set() or len(sockets)>=32:
                connection.close();return False
            sockets[identifier]=connection
        threading.Thread(target=read,args=(connection,identifier),daemon=True).start()
        return True
    def accept():
        identifier=0
        while not stopping.is_set():
            try:connection,_=listener.accept()
            except OSError:return
            identifier+=1
            with guard:
                if len(sockets)>=32:connection.close();continue
                sockets[identifier]=connection
            try:send(outgoing,writer,b'O',identifier)
            except (OSError,ValueError):close(identifier);return
            threading.Thread(target=read,args=(connection,identifier),daemon=True).start()
    if listener:
        threading.Thread(target=accept,daemon=True).start()
        send(outgoing,writer,b'R',0)
    try:
        for kind,identifier,payload in frames(incoming):
            if kind==b'R':
                if ready:ready()
            elif kind==b'O' and target:
                try:connection=socket.create_connection(target,timeout=10);connection.settimeout(None)
                except OSError:send(outgoing,writer,b'C',identifier);continue
                if not register(connection,identifier):send(outgoing,writer,b'C',identifier)
            elif kind==b'D':
                with guard:connection=sockets.get(identifier)
                if connection:
                    try:connection.sendall(payload)
                    except OSError:
                        close(identifier);send(outgoing,writer,b'C',identifier)
            elif kind==b'C':close(identifier)
            else:raise ValueError('invalid relay operation')
    finally:
        stopping.set()
        if listener:listener.close()
        with guard:identifiers=list(sockets)
        for identifier in identifiers:close(identifier)


def remote():
    listener=socket.socket();listener.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    listener.bind(('0.0.0.0',7001));listener.listen(32)
    bridge(sys.stdin.buffer,sys.stdout.buffer,listener=listener)


def host(state,container):
    from .session import write
    configuration=json.loads((state/'control/return-path.json').read_text())
    cfg=validate(configuration['host'],configuration['port'],configuration['transport'])
    identity=configuration['generation']
    child=None
    def stopped(*_):
        signal.signal(signal.SIGTERM,signal.SIG_IGN);signal.signal(signal.SIGINT,signal.SIG_IGN)
        raise SystemExit(0)
    signal.signal(signal.SIGTERM,stopped);signal.signal(signal.SIGINT,stopped)
    def ready():write(state/'control/return-ready.json',dict(cfg,generation=identity,ready_at_ns=time.time_ns(),container=container))
    code=None
    try:
        child=subprocess.Popen(['docker','exec','-i',container,'python','-m','independent_lab.return_path','remote'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        bridge(child.stdout,child.stdin,target=(cfg['host'],cfg['port']),ready=ready)
    finally:
        signal.signal(signal.SIGTERM,signal.SIG_IGN);signal.signal(signal.SIGINT,signal.SIG_IGN)
        if child:
            try:child.stdin.close()
            except OSError:pass
            try:code=child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.terminate()
                try:code=child.wait(timeout=5)
                except subprocess.TimeoutExpired:child.kill();code=child.wait()
            child.stdout.close();child.stderr.close()
        write(state/'control/return-stopped.json',{'generation':identity,'remote_process_stopped':code==0,'remote_exit_code':code})
        (state/'control/return-ready.json').unlink(missing_ok=True)


def start(state,container):
    (state/'control/return-ready.json').unlink(missing_ok=True)
    (state/'control/return-stopped.json').unlink(missing_ok=True)
    process=None
    try:
        process=subprocess.Popen([sys.executable,'-m','independent_lab.return_path','host',str(state),container],stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        for _ in range(200):
            if (state/'control/return-ready.json').exists():return process
            if process.poll() is not None:raise ValueError('owned return relay did not start')
            time.sleep(.05)
        raise ValueError('owned return relay readiness timed out')
    except BaseException:
        if process:
            process.terminate()
            try:process.wait(timeout=15)
            except subprocess.TimeoutExpired:process.kill();process.wait()
        raise


if __name__=='__main__':
    if sys.argv[1]=='remote':remote()
    elif sys.argv[1]=='host':host(Path(sys.argv[2]),sys.argv[3])
    else:raise ValueError('unknown return relay operation')
