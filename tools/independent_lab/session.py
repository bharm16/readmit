"""Long-lived, generation-fenced operator sessions for the reference targets.

Host commands own process exclusion; the in-network worker owns target effects.
Neither controller sends an HL7 stimulus or writes an Appointment.
"""
import argparse
import base64
import contextlib
import hashlib
import json
import re
from pathlib import Path
import secrets
import subprocess
import time
import xml.etree.ElementTree as ET

SCHEMA = 'readmit-lab-session/v1'


def write(path, value):
    path = Path(path)
    temporary = path.with_suffix('.incomplete')
    temporary.write_text(json.dumps(value, indent=2) + '\n')
    temporary.chmod(0o600)
    temporary.replace(path)


@contextlib.contextmanager
def controller(state):
    import fcntl
    with (state / 'control/session.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('another session controller is active') from None
        yield


def read(state):
    value = json.loads((state / 'control/session.json').read_text())
    if value.get('schema') != SCHEMA or not re.fullmatch(r'lab-[a-z0-9-]{1,48}',value.get('generation','')):
        raise ValueError('unknown session state')
    return value


def host(action, state, generation, mode, lab, *, route=None, defect=None, return_host=None, return_port=None, return_transport=None):
    state, info = lab.owner(state)
    selected_return=None
    if any(value is not None for value in (return_host,return_port,return_transport)):
        from .return_path import validate
        selected_return=validate(return_host,return_port,return_transport)
    with controller(state):
        current = read(state) if (state / 'control/session.json').exists() else None
        if current is None and (state/'control/session-intent.json').exists():
            intent=json.loads((state/'control/session-intent.json').read_text())
            current={'schema':SCHEMA,'generation':intent['generation'],'status':'interrupted','intent':intent}
            if action not in ('teardown','export'):
                raise ValueError('interrupted session start; inspect retained intent and explicitly teardown its generation')
        if action != 'start':
            if not current or not generation or generation != current['generation']:
                raise ValueError('stale session generation; inspect session-status before acting')
        elif current:
            raise ValueError('session already exists; select its current generation explicitly')
        if (state/'control/session-intent.json').exists() or current and current['status'] in ('preparing','changing','resetting','stopping'):
            current['status']='interrupted'
            if (state/'control/session-intent.json').exists():
                current['intent']=json.loads((state/'control/session-intent.json').read_text())
            write(state/'control/session.json',current)
        if action == 'export':
            export(state)
            return
        if action in ('status','witness') and (state/'control/session-intent.json').exists():
            raise ValueError('interrupted session transition; select an explicit revision or teardown')
        lab.verify_configuration()
        if info.get('config_root') != str(lab.LAB):
            raise ValueError('lab configuration owner differs')
        if action in ('status','witness') and current.get('route')=='v2v2':
            from . import tunnel
            try:tunnel.check(state)
            except Exception:
                current['status']='interrupted';write(state/'control/session.json',current)
                raise
        if action == 'teardown':
            from . import tunnel
            tunnel.stop(state)
            if current['status'] != 'stopped':
                current['status']='interrupted'
                write(state/'control/session.json',current)
            lab.compose(state, 'down', '--volumes', '--remove-orphans')
            for kind, command in [('containers',['docker','ps','-aq']),('volumes',['docker','volume','ls','-q']),('networks',['docker','network','ls','-q'])]:
                if subprocess.check_output(command+['--filter','label=com.docker.compose.project='+info['project']],text=True).strip():
                    raise ValueError('owned '+kind+' remain after teardown')
            write(state/'session-evidence/teardown.json', {'schema':'readmit-lab-session-teardown/v1','project':info['project'],'generation':generation,'containers_absent':True,'volumes_absent':True,'networks_absent':True})
            return
        if action == 'start':
            if (state / 'evidence/qualification.json').exists():
                raise ValueError('reference qualification already owns this lab')
            (state / 'session-evidence').mkdir(mode=0o700, exist_ok=True)
        if action in ('start', 'revision'):
            if current and current['status'] == 'stopped':
                raise ValueError('stopped session cannot be restarted; initialize a fresh lab')
            route=route or (current or {}).get('route','v2fhir')
            defect=defect or (current or {}).get('defect','duplicate')
            if route=='v2v2':
                if selected_return is None and (state/'control/return-path.json').exists():
                    selected_return=json.loads((state/'control/return-path.json').read_text())
                if selected_return is None:raise ValueError('v2v2 session requires an explicit private return path')
                selected_return={key:value for key,value in selected_return.items() if key!='generation'}
            elif selected_return is not None:raise ValueError('return path requires the v2v2 route')
            next_generation = 'lab-' + secrets.token_hex(12)
            write(state/'control/session-intent.json', {'action':action,'previous':generation,'generation':next_generation,'mode':mode,'started_at_ns':time.time_ns()})
            lab.record_runtime(state)
            from . import tunnel
            if action == 'revision':
                tunnel.stop(state)
            if selected_return:
                write(state/'control/return-path.json',dict(selected_return,generation=next_generation))
            else:(state/'control/return-path.json').unlink(missing_ok=True)
            ports = tunnel.start(state, info['project'])
            base = 'https://127.0.0.1:' + str(ports['fixture'])
            connection = {'schema':'readmit-lab-connection/v1', 'mllp':{'host':'127.0.0.1','port':ports['engine-v2v2'] if route=='v2v2' else ports['engine'],'tls':False}, 'fhir':{'base':base+'/fhir','token_endpoint':base+'/token','discovery':base+'/.well-known/smart-configuration','authorities':str(state/'secrets/clients/ca.pem'),'server_name':'localhost','client_id':'observer','private_key':str(state/'secrets/clients/observer-key.pem'),'scopes':['system/*.rs']}}
            if selected_return:
                credentials=state/'secrets/clients'
                connection['return_listener']=dict(selected_return,authorities=str(credentials/'ca.pem'),certificate=str(credentials/'return-server.pem'),private_key=str(credentials/'return-server-key.pem'),client_certificate=str(credentials/'return-client.pem'),client_private_key=str(credentials/'return-client-key.pem'))
            write(state/'connection.json', connection)
            write(state/'control/connection.json', {'token_endpoint':base+'/token'})
            lab.compose(state, 'run', '--rm', 'qualifier', 'python', '-m', 'independent_lab.session', 'prepare', next_generation, '--mode', mode, '--route', route, '--defect', defect)
            lab.compose(state, 'exec', '-T', 'engine', 'bash', '-c', 'java -cp "/opt/lab:$(cat /opt/lab/classpath)" ChannelFactory --all /lab-channels')
            lab.compose(state, 'run', '--rm', 'qualifier', 'python', '-m', 'independent_lab.session', action, next_generation, '--mode', mode, '--previous', generation or '', '--route', route, '--defect', defect)
            public_connection={'schema':'readmit-lab-connection-evidence/v1','mllp':connection['mllp'],'fhir':{key:value for key,value in connection['fhir'].items() if key not in ('private_key','authorities')}}
            public_connection['fhir']['ca_sha256']=hashlib.sha256((state/'secrets/clients/ca.pem').read_bytes()).hexdigest()
            if selected_return:
                import ssl
                public_connection['return_listener']={key:value for key,value in selected_return.items() if key!='generation'}
                for name,label in [('ca.pem','ca'),('return-server.pem','server_certificate'),('return-client.pem','client_certificate')]:
                    certificate=ssl.PEM_cert_to_DER_cert((state/'secrets/clients'/name).read_text())
                    public_connection['return_listener'][label+'_sha256']=hashlib.sha256(certificate).hexdigest()
                tunnel.check(state)
                write(state/'session-evidence'/next_generation/'return-path.json',json.loads((state/'control/return-ready.json').read_text()))
            write(state/'session-evidence'/next_generation/'connection.json', public_connection)
            (state/'control/session-intent.json').unlink()
        else:
            lab.compose(state, 'run', '--rm', 'qualifier', 'python', '-m', 'independent_lab.session', action, generation)
        if action == 'stop':
            from . import tunnel
            try:
                tunnel.stop(state)
            except Exception:
                current=read(state);current['status']='interrupted'
                write(state/'control/session.json',current)
                raise
            receipt=state/'control/tunnel-stopped.json'
            if receipt.exists():
                (state/'session-evidence'/generation/'tunnel-stop.json').write_bytes(receipt.read_bytes())
            completed=receipt.exists() and json.loads(receipt.read_text()).get('listeners_closed') is True
            current=read(state);current['status']='stopped' if completed else 'interrupted'
            write(state/'session-evidence'/generation/('state-'+str(time.time_ns())+'.json'),current)
            write(state/'control/session.json',current)
        print(json.dumps(read(state), indent=2))


def worker(action, generation, mode, previous, route="v2fhir", defect="duplicate"):
    from . import qualify as q
    if not re.fullmatch(r'lab-[a-z0-9-]{1,48}', generation):
        raise ValueError('invalid session generation')
    root = Path('/session-evidence')
    control = Path('/control/session.json')
    directory = root/generation
    if control.exists() and action not in ('prepare','start','revision'):
        route=json.loads(control.read_text()).get('route','v2fhir')
    identifier = q.IDS[route]
    if action == 'prepare':
        directory.mkdir(mode=0o700)
        cfg = {'name':'session','mode':mode,'defect':defect,'generation':generation,'route':route}
        if route=='v2v2':
            target=json.loads((q.CONTROL/'return-path.json').read_text())
            cfg.update(return_transport=target['transport'],return_host=target['host'],return_port=target['port'])
        source = (Path(__file__).parent/'channel.js').read_text()
        (q.CHANNELS/(route+'-session.js')).write_text(source.replace('__LAB_CONFIGURATION__', json.dumps(cfg, sort_keys=True)))
        return
    deadline = time.monotonic()+180
    while True:
        try:
            client = q.engine_client()
            metadata = q.api('/fhir/metadata', client='observer')
            break
        except Exception:
            if time.monotonic() >= deadline:
                raise RuntimeError('session readiness deadline exceeded')
            time.sleep(2)
    version = q.engine(client, '/server/version', accept='text/plain').decode().strip()
    status, health = q.request('https://fixture:9443/health')
    health = json.loads(health)
    lock = json.loads((Path(__file__).parent/'lock.json').read_text())
    if version != '4.6.0' or metadata.get('fhirVersion') != '4.0.1' or metadata.get('software',{}).get('version') != '8.6.0' or status != 200 or len(health.get('code_sha256',{})) != 2 or any(lock['config_sha256'].get(name) != digest for name,digest in health['code_sha256'].items()):
        raise ValueError('running session identities differ from reviewed lab')
    current = json.loads(control.read_text()) if control.exists() else None
    expected = previous if action in ('start','revision') else generation
    if current and current['generation'] != expected:
        raise ValueError('stale session generation')
    if action in ('start','revision'):
        if action == 'start' and current:
            raise ValueError('session already owned')
        if current:
            current['status']='changing'
            write(control,current)
        q.undeploy(client)
        reset = q.api('/admin/reset','POST',{'generation':expected} if expected else {})
        if current:
            write(root/expected/('reset-'+str(time.time_ns())+'.json'), reset)
            write(root/expected/'closed.json', dict(current,status='reset',reset=reset))
        cfg = {'name':'session','mode':mode,'defect':defect,'generation':generation,'route':route}
        q.api('/admin/revision','POST',cfg)
        current = dict(cfg,schema=SCHEMA,status='preparing',started_at_ns=time.time_ns())
        write(control,current)
        # The booking target is absent. Only the SIU's declared participants exist.
        baseline = json.loads((q.FIXTURES/'baseline.json').read_text())
        prerequisites = [item for item in baseline if item['resourceType'] in ('Practitioner','Location')]
        prerequisites.append({'resourceType':'Patient','id':'lab-patient-01','identifier':[{'system':'urn:readmit:lab:patient','value':'lab-patient-01'}],'name':[{'family':'Lark','given':['Fictional']}]})
        receipts=[]
        if route=='v2v2':prerequisites=[]
        for obj in prerequisites:
            obj['meta']={'tag':[{'system':'urn:readmit:independent-lab','code':generation}]}
            q.api('/fhir/'+obj['resourceType']+'/'+obj['id'],'PUT',obj,retain=receipts)
        write(directory/'prerequisites.json',receipts)
        baseline=q.snapshot(generation)
        if q.resources(baseline,'Appointment'):
            raise ValueError('session target state was populated before product execution')
        for kind, identifier_value in ([('Patient','lab-patient-01'),('Practitioner','lab-practitioner-01'),('Location','lab-location-01')] if route=='v2fhir' else []):
            values=q.resources(baseline,kind)
            if len(values)!=1 or values[0].get('id')!=identifier_value or not values[0].get('meta',{}).get('versionId'):
                raise ValueError('declared prerequisite state was not observed')
        write(directory/'baseline.json',baseline)
        xml=(q.CHANNELS/(route+'-session.xml')).read_bytes()
        q.engine(client,'/channels/'+identifier+'?override=true','PUT',xml,accept='text/plain')
        q.engine(client,'/channels/'+identifier+'/_deploy?returnErrors=true','POST')
        deadline=time.monotonic()+30
        while b'<state>STARTED</state>' not in q.engine(client,'/channels/'+identifier+'/status'):
            if time.monotonic()>deadline: raise ValueError('session channel did not start')
            time.sleep(.5)
        exported=q.engine(client,'/channels/'+identifier)
        # Read back the exact deployed mapping; OIE may add serializer metadata.
        tree=ET.fromstring(exported)
        script=tree.findtext('.//destinationConnectors/connector/properties/script')
        if script != (q.CHANNELS/(route+'-session.js')).read_text():
            raise ValueError('deployed channel script differs from selected revision')
        (directory/'channel.xml').write_bytes(exported)
        write(directory/'capability.json',metadata)
        (directory/'runtime.json').write_bytes((q.EVIDENCE/'runtime.json').read_bytes())
        stimuli=directory/'stimuli';stimuli.mkdir(mode=0o700)
        provenance=[]
        for name in ('siu-s12.hl7','siu-s13.hl7'):
            source=(q.FIXTURES/name).read_bytes()
            derived=source.replace(b'__GENERATION__',generation.encode())
            (stimuli/name).write_bytes(derived)
            provenance.append({'source':'testdata/integration-lab/'+name,'source_sha256':hashlib.sha256(source).hexdigest(),'source_base64':base64.b64encode(source).decode(),'derived_sha256':hashlib.sha256(derived).hexdigest(),'transformation':'replace __GENERATION__ in ZLG with active lab generation; Readmit run correlation is separate'})
        write(directory/'source-provenance.json',provenance)
        current.update(status='ready',channel_sha256=hashlib.sha256(exported).hexdigest(),oie_version=version,hapi_software=metadata['software'],fhir_version=metadata['fhirVersion'],fixture_code_sha256=health['code_sha256'])
        write(directory/'readiness.json',current)
        write(control,current)
    elif action in ('status','witness'):
        remote=q.api('/admin/state')
        channel=q.engine(client,'/channels/'+identifier)
        started=b'<state>STARTED</state>' in q.engine(client,'/channels/'+identifier+'/status')
        ready=current['status']=='ready' and not remote['resetting'] and remote['generation']==generation and started and hashlib.sha256(channel).hexdigest()==current['channel_sha256']
        if not ready:
            current['status']='interrupted' if current['status']=='ready' else current['status']
            write(control,current)
        if action=='witness':
            write(directory/('witness-'+str(time.time_ns())+'.json'),{'schema':'readmit-lab-witness/v1','generation':generation,'ready':ready,'observed_at_ns':time.time_ns(),'resources':q.snapshot(generation)})
        if not ready and action=='status': raise ValueError('session is not ready; retained state is inspection-only')
    elif action in ('reset','stop'):
        current['status']='stopping' if action=='stop' else 'resetting'
        write(control,current)
        q.undeploy(client)
        reset=q.api('/admin/reset','POST',{'generation':generation})
        write(directory/('reset-'+str(time.time_ns())+'.json'),reset)
        current.update(status='stopping' if action=='stop' else 'reset',reset=reset)
        write(directory/('state-'+str(time.time_ns())+'.json'),current)
        write(control,current)
    else:
        raise ValueError('unknown session action')


def export(state):
    """Copy only retained acquisitions, never private connection/credential state."""
    import re
    source=state/'session-evidence'
    files={}
    for path in sorted(source.rglob('*')):
        if path.is_symlink():raise ValueError('session evidence cannot contain links')
        if path.is_file():
            raw=path.read_bytes()
            if len(raw)>64*1024*1024:raise ValueError('session evidence member exceeds bound')
            files[path.relative_to(source).as_posix()]=raw
    files['session-state.json']=(state/'control/session.json').read_bytes()
    runtime=state/'evidence/runtime.json'
    if runtime.exists():
        files['runtime.json']=runtime.read_bytes()
    else:
        files['runtime-unavailable.json']=json.dumps({'schema':'readmit-lab-acquisition-unavailable/v1','reason':'runtime acquisition did not complete'}).encode()
    if sum(map(len,files.values()))>1024*1024*1024:raise ValueError('session evidence exceeds bound')
    needles=[path.read_bytes() for path in (state/'secrets').rglob('*') if path.is_file() and ('key' in path.name or 'password' in path.name or path.suffix=='.p12' or path.name=='clients.json')]
    needles += [line.split('=',1)[1].encode() for line in (state/'lab.env').read_text().splitlines() if line.startswith('LAB_DB_PASSWORD=')]
    for raw in files.values():
        if b'PRIVATE KEY-----' in raw or re.search(rb'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+',raw) or any(value and value in raw for value in needles):
            raise ValueError('credential-shaped material found; export refused')
    destination=state/'session-export'
    destination.mkdir(mode=0o700)
    manifest={'schema':'readmit-lab-session-export/v1','state':read(state)['status'],'files':{}}
    for name,raw in files.items():
        target=destination/name;target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
        with target.open('xb') as output:output.write(raw)
        manifest['files'][name]={'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
    write(destination/'manifest.json',manifest)
    verify_export(destination)
    print('Exported credential-free session acquisitions; product verdicts remain in their retained runs')


def verify_export(directory):
    """Offline byte integrity check; never upgrades a stopped run to passed."""
    manifest=json.loads((directory/'manifest.json').read_text())
    if manifest.get('schema')!='readmit-lab-session-export/v1':raise ValueError('unknown session export')
    actual={path.relative_to(directory).as_posix() for path in directory.rglob('*') if path.is_file() and path!=directory/'manifest.json'}
    if actual!=set(manifest['files']):raise ValueError('session export inventory differs')
    for name,identity in manifest['files'].items():
        if Path(name).is_absolute() or '..' in Path(name).parts:raise ValueError('invalid exported member')
        target=directory/name
        if any(path.is_symlink() for path in [target,*target.parents]):raise ValueError('session export cannot contain links')
        raw=target.read_bytes()
        if len(raw)!=identity['bytes'] or hashlib.sha256(raw).hexdigest()!=identity['sha256']:raise ValueError('session export member changed: '+name)
    return manifest


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action');parser.add_argument('generation');parser.add_argument('--mode',default='positive');parser.add_argument('--previous',default='')
    parser.add_argument('--route',default='v2fhir',choices=['v2fhir','v2v2']);parser.add_argument('--defect',default='duplicate',choices=['duplicate','field'])
    args=parser.parse_args()
    worker(args.action,args.generation,args.mode,args.previous,args.route,args.defect)
