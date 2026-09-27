"""Exercise actual target boundaries using independently authored fixture facts."""
import base64
import copy
import datetime
import hashlib
import http.cookiejar
import json
from pathlib import Path
import secrets
import socket
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from . import security

SECRET = Path('/run/lab')
FIXTURES = Path('/fixtures')
EVIDENCE = Path('/evidence')
CHANNELS = Path('/lab-channels')
CONTROL = Path('/control')
IDS = {'v2v2': '10000000-0000-0000-0000-000000000001', 'v2fhir': '10000000-0000-0000-0000-000000000002'}
DEFECTS = ['duplicate', 'field', 'link', 'drop', 'reorder', 'late-duplicate']
MODES = ['positive', 'defective', 'corrected', 'reintroduced']


def context(mutual=False, trust=True):
    ctx = ssl.create_default_context(cafile=str(SECRET / 'ca.pem') if trust else None)
    if mutual:
        ctx.load_cert_chain(SECRET / 'tls-client.pem', SECRET / 'tls-client-key.pem')
    return ctx


def request(url, method='GET', body=None, headers=None, client=None):
    req = urllib.request.Request(url, data=body, method=method, headers=headers or {})
    try:
        opener = client.open if client else lambda r, timeout: urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=context())).open(r,timeout=timeout)
        with opener(req, timeout=30) as response:
            return response.status, response.read(8 * 1024 * 1024)
    except urllib.error.HTTPError as error:
        return error.code, error.read(65536)


def access(client='setup', **overrides):
    scope = 'system/*.cruds' if client == 'setup' else 'system/*.rs'
    body = urllib.parse.urlencode({'grant_type': 'client_credentials', 'scope': scope, 'client_assertion_type': 'urn:ietf:params:oauth:client-assertion-type:jwt-bearer', 'client_assertion': security.assertion(client, SECRET, **overrides)}).encode()
    status, raw = request('https://fixture:9443/token', 'POST', body, {'Content-Type': 'application/x-www-form-urlencoded'})
    if status != 200:
        raise RuntimeError('SMART token request refused with status ' + str(status))
    return json.loads(raw)['access_token']


def api(path, method='GET', obj=None, client='setup', retain=None):
    status, raw = request('https://fixture:9443' + path, method, json.dumps(obj).encode() if obj is not None else None, {'Authorization': 'Bearer ' + access(client), 'Content-Type': 'application/fhir+json'})
    if status not in (200, 201, 204):
        raise RuntimeError('fixture request refused: ' + path.split('?')[0] + ' status=' + str(status) + ' body=' + raw[:1000].decode(errors='replace'))
    if retain is not None:
        retain.append({'path':path,'method':method,'status':status,'body_sha256':hashlib.sha256(raw).hexdigest(),'body_base64':base64.b64encode(raw).decode(),'received_at_ns':time.time_ns(),'request_body_base64':base64.b64encode(json.dumps(obj).encode()).decode() if obj is not None else ''})
    return json.loads(raw) if raw else None


def engine_client():
    cookies = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context()), urllib.request.HTTPCookieProcessor(cookies))
    password = (SECRET / 'engine-admin-password').read_text()
    def login(value):
        return request('https://engine:8443/api/users/_login', 'POST', urllib.parse.urlencode({'username': 'admin', 'password': value}).encode(), {'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/x-www-form-urlencoded', 'Accept': 'application/xml'}, client)
    status, raw = login(password)
    if status != 200 or b'SUCCESS' not in raw:
        if (CONTROL / 'admin-rotated').exists():
            raise RuntimeError('generated engine administrator credential failed')
        status, raw = login('admin')
        if status != 200 or b'SUCCESS' not in raw:
            raise RuntimeError('isolated engine bootstrap login failed')
        status, _ = request('https://engine:8443/api/users/1/password', 'PUT', password.encode(), {'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'text/plain'}, client)
        if status != 200:
            raise RuntimeError('engine password rotation failed')
        (CONTROL / 'admin-rotated').write_text('rotated')
    return client


def engine(client, path, method='GET', body=None, accept='application/xml'):
    status, raw = request('https://engine:8443/api' + path, method, body, {'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/xml', 'Accept': accept}, client)
    if status not in (200, 201, 204):
        raise RuntimeError('engine operation failed: ' + path + ' status=' + str(status) + ' body=' + raw[:2000].decode(errors='replace'))
    return raw


def undeploy(client):
    statuses = ET.fromstring(engine(client, '/channels/statuses'))
    deployed = {node.text for node in statuses.findall('.//channelId')}
    for identifier in IDS.values():
        if identifier in deployed:
            engine(client, '/channels/' + identifier + '/_undeploy?returnErrors=true', 'POST')


def prepare():
    source = (Path(__file__).parent / 'channel.js').read_text()
    cases = []
    for defect in DEFECTS:
        for mode in MODES:
            name = defect + '-' + mode
            generation = 'lab-' + secrets.token_hex(10)
            case = {'name': name, 'mode': mode, 'defect': defect, 'generation': generation}
            cases.append(case)
            for route in IDS:
                cfg = dict(case, route=route)
                (CHANNELS / (route + '-' + name + '.js')).write_text(source.replace('__LAB_CONFIGURATION__', json.dumps(cfg, sort_keys=True)))
    (CONTROL / 'cases.json').write_text(json.dumps(cases))
    print('Prepared 24 isolated route revisions')


def send(port, filename, generation):
    wire = (FIXTURES / filename).read_bytes().replace(b'__GENERATION__', generation.encode())
    with socket.create_connection(('engine', port), timeout=10) as connection:
        connection.settimeout(30)
        connection.sendall(b'\x0b' + wire + b'\x1c\r')
        ack = b''
        while not ack.endswith(b'\x1c\r'):
            part = connection.recv(65536)
            if not part or len(ack) > 65536:
                raise RuntimeError('incomplete engine acknowledgement')
            ack += part
    if b'MSA|AA|' not in ack:
        raise RuntimeError('engine did not positively acknowledge synthetic stimulus: ' + ack.decode(errors='replace')[:500])
    return {'fixture':filename,'ack_received_at_ns':time.time_ns(),'input_base64':base64.b64encode(wire).decode(),'input_sha256': hashlib.sha256(wire).hexdigest(), 'ack_base64': base64.b64encode(ack).decode()}


def snapshot(generation):
    results = {}
    tag = urllib.parse.quote('urn:readmit:independent-lab|' + generation, safe='')
    for resource in sorted(security.RESOURCES):
        data = api('/fhir/' + resource + '?_count=1000&_tag=' + tag, client='observer')
        if any(link.get('relation') == 'next' for link in data.get('link', [])):
            raise RuntimeError('qualification observation was paginated')
        results[resource] = data
    results['AppointmentHistory']={}
    for entry in results['Appointment'].get('entry',[]):
        identifier=entry['resource']['id']
        history=api('/fhir/Appointment/'+identifier+'/_history?_count=1000',client='observer')
        if any(link.get('relation')=='next' for link in history.get('link',[])):
            raise RuntimeError('history observation was paginated')
        results['AppointmentHistory'][identifier]=history
    return results


def resources(observed, kind):
    return [entry['resource'] for entry in observed[kind].get('entry', [])]


def check(receiver, observed, expected=None):
    expected = json.loads((FIXTURES / 'expected.json').read_text()) if expected is None else expected
    payloads = [base64.b64decode(row['payload_base64']).decode() for row in receiver['messages']]
    controls = [raw.split('\r')[0].split('|')[9] for raw in payloads]
    v2 = controls == expected['v2_control_order']
    if v2:
        v2 = all(raw.split('\r')[0].split('|')[2] == expected['v2_sender'] for raw in payloads)
        final = payloads[-1].split('\r')
        sch = next(line for line in final if line.startswith('SCH|')).split('|')
        pid = next(line for line in final if line.startswith('PID|')).split('|')
        v2 = v2 and sch[11] == '^^^20300102100000+0000' and pid[3] == 'lab-patient-01^LAB'
    def appointment(key, patient):
        found = [r for r in resources(observed, 'Appointment') if any(i.get('system') == 'urn:readmit:lab:appointment' and i.get('value') == key for i in r.get('identifier', []))]
        return len(found) == 1 and found[0].get('start') == expected['appointment_start'] and found[0].get('status') == expected['appointment_status'] and found[0]['participant'][0]['actor']['reference'] == patient
    fhir = appointment('lab-appointment-01', expected['v2_patient_reference'])
    patients = [p for p in resources(observed, 'Patient') if p['id'] == 'lab-patient-01']
    encounters = [p for p in resources(observed, 'Encounter') if p['id'] == 'lab-encounter-01']
    observations = resources(observed, 'Observation')
    reports = resources(observed, 'DiagnosticReport')
    fhir = fhir and len(patients) == 1 and patients[0]['name'][0]['family'] == expected['patient_family'] and len(encounters) == 1 and encounters[0]['status'] == expected['encounter_status'] and encounters[0].get('subject',{}).get('reference')==expected['v2_patient_reference']
    fhir = fhir and sorted(o.get('valueString') for o in observations) == expected['observation_values'] and len(reports) == 1
    fhir = fhir and all(o.get('basedOn',[{}])[0].get('reference') == expected['order_reference'] and o.get('subject',{}).get('reference')==expected['v2_patient_reference'] for o in observations + reports)
    orders=[r for r in resources(observed,'ServiceRequest') if r.get('id')=='lab-order-01']
    fhir=fhir and len(orders)==1 and orders[0].get('subject',{}).get('reference')==expected['v2_patient_reference']
    expected_results={'Observation/'+id for id in expected['observation_records']}
    fhir=fhir and {o.get('id') for o in observations}==set(expected['observation_records'])
    fhir=fhir and all(o.get('valueString')==expected['observation_records'].get(o.get('id'),{}).get('value') and o.get('code',{}).get('text')==expected['observation_records'].get(o.get('id'),{}).get('code') and o.get('status')=='final' for o in observations)
    fhir=fhir and len(reports)==1 and reports[0].get('status')=='final'
    fhir=fhir and len(reports)==1 and {r.get('reference') for r in reports[0].get('result',[])}==expected_results
    return {'v2-to-v2': bool(v2), 'v2-to-fhir': bool(fhir), 'fhir-native': appointment('lab-native-appointment-01', expected['native_patient_reference'])}


def defect_matches(receiver, observed, defect, initial=None, transport=None):
    """Require the named defect, not merely any failed expectation."""
    payloads=[base64.b64decode(row['payload_base64']).decode() for row in receiver['messages']]
    controls=[raw.split('\r')[0].split('|')[9] for raw in payloads]
    v2=False
    if defect in ('duplicate','late-duplicate'):
        v2=controls==['LAB-S12','LAB-S13','LAB-S13'] and payloads[-1]==payloads[-2]
        if defect=='late-duplicate' and v2:
            v2=bool(initial and transport) and len(initial['receiver']['messages'])==2 and receiver['messages'][-1]['received_at_ns']-transport['acknowledgements'][1]['ack_received_at_ns']>=1000000000
    elif defect=='drop':v2=controls==['LAB-S12']
    elif defect=='reorder':v2=controls==['LAB-S13','LAB-S12']
    elif controls==['LAB-S12','LAB-S13']:
        final=payloads[-1].split('\r')
        if defect=='field':v2=next(line for line in final if line.startswith('SCH|')).split('|')[11]=='^^^20300102094500+0000'
        if defect=='link':v2=next(line for line in final if line.startswith('PID|')).split('|')[3]=='lab-wrong^LAB'
    def appointment(key,native=False):
        found=[r for r in resources(observed,'Appointment') if any(i.get('system')=='urn:readmit:lab:appointment' and i.get('value')==key for i in r.get('identifier',[]))]
        starts=sorted(r.get('start','') for r in found)
        if defect=='duplicate':return starts==['2030-01-02T09:30:00Z','2030-01-02T10:00:00Z']
        if defect=='late-duplicate':
            if not initial or not transport:return False
            key_name='fhir-native' if native else 'v2-to-fhir'
            first=[entry['resource'] for entry in initial[key_name].get('entry',[])]
            if len(first)!=1 or first[0].get('start')!='2030-01-02T10:00:00Z' or first[0].get('status')!='booked':return False
            expected_starts=['2030-01-02T09:30:00Z','2030-01-02T10:00:00Z','2030-01-02T10:00:00Z'] if native else ['2030-01-02T10:00:00Z','2030-01-02T10:00:00Z']
            if starts!=expected_starts:return False
            for extra in found:
                if extra['id']==first[0]['id']:continue
                stamp=datetime.datetime.fromisoformat(extra['meta']['lastUpdated'].replace('Z','+00:00'))
                if stamp.tzinfo is None:return False
                received=transport['native_responses'][0 if extra['start']=='2030-01-02T09:30:00Z' else 1]['received_at_ns'] if native else transport['acknowledgements'][4]['ack_received_at_ns']
                if int(stamp.timestamp()*1000000000)-received<1000000000:return False
            return True
        if len(found)!=1:return False
        current=found[0]
        if defect=='field':return current.get('start')=='2030-01-02T09:45:00Z' and current.get('status')=='noshow'
        if defect=='link':return current['participant'][0]['actor']['reference']=='Patient/lab-wrong'
        history=observed['AppointmentHistory'][current['id']]
        generation=receiver['generation']
        versions=[entry['resource'] for entry in history.get('entry',[]) if entry.get('resource') and any(t.get('system')=='urn:readmit:independent-lab' and t.get('code')==generation for t in entry['resource'].get('meta',{}).get('tag',[]))]
        versions.sort(key=lambda item:int(item['meta']['versionId']))
        sequence=[item.get('start') for item in versions]
        if defect=='drop':return sequence==['2030-01-02T09:30:00Z']
        if defect=='reorder':return sequence==['2030-01-02T10:00:00Z','2030-01-02T09:30:00Z']
        return False
    fhir=appointment('lab-appointment-01')
    if defect=='link':
        linked=resources(observed,'Observation')+resources(observed,'DiagnosticReport')
        fhir=fhir and len(linked)==3 and all(item['basedOn'][0]['reference']=='ServiceRequest/lab-wrong-order' for item in linked)
    return {'v2-to-v2':bool(v2),'v2-to-fhir':bool(fhir),'fhir-native':appointment('lab-native-appointment-01',True)}


def security_checks():
    findings = {}
    generation='lab-auth-'+secrets.token_hex(8)
    api('/admin/revision','POST',{'name':'auth-controls','mode':'positive','defect':'none','generation':generation})
    patient={'resourceType':'Patient','id':'lab-auth-control','meta':{'tag':[{'system':'urn:readmit:independent-lab','code':generation}]}}
    api('/fhir/Patient/lab-auth-control','PUT',patient)
    patient['id']='lab-denied'
    valid_body=json.dumps(patient).encode()
    status, _ = request('https://fixture:9443/fhir/Patient')
    findings['unauthenticated_denied'] = status == 401
    token = access('observer')
    status, denial = request('https://fixture:9443/fhir/Patient/lab-denied', 'PUT', valid_body, {'Authorization':'Bearer '+token, 'Content-Type':'application/fhir+json'})
    findings['observer_write_denied'] = status == 403 and json.loads(denial).get('error')=='insufficient scope'
    status, denial = request('https://fixture:9443/fhir/Patient/lab-denied', 'DELETE', headers={'Authorization':'Bearer '+token})
    findings['observer_delete_denied'] = status == 403 and json.loads(denial).get('error')=='insufficient scope'
    def exchange(assertion, scope='system/*.rs'):
        form = urllib.parse.urlencode({'grant_type':'client_credentials','scope':scope,'client_assertion_type':'urn:ietf:params:oauth:client-assertion-type:jwt-bearer','client_assertion':assertion}).encode()
        return request('https://fixture:9443/token','POST',form,{'Content-Type':'application/x-www-form-urlencoded'})[0]
    findings['wrong_audience_denied'] = exchange(security.assertion('observer',SECRET,aud='https://wrong.invalid/token')) == 401
    findings['expired_assertion_denied'] = exchange(security.assertion('observer',SECRET,exp=int(time.time())-1)) == 401
    jwt = security.assertion('observer',SECRET)
    parts = jwt.split('.')
    signature = bytearray(security.unb64(parts[2])); signature[0] ^= 1
    findings['bad_signature_denied'] = exchange(parts[0]+'.'+parts[1]+'.'+security.b64(signature)) == 401
    findings['scope_escalation_denied'] = exchange(security.assertion('observer',SECRET), 'system/*.cruds') == 403
    jwt=security.assertion('observer',SECRET)
    findings['assertion_replay_denied'] = exchange(jwt)==200 and exchange(jwt)==401
    try:
        urllib.request.urlopen('https://fixture:9443/health',context=context(trust=False),timeout=5)
        findings['untrusted_ca_denied']=False
    except (ssl.SSLError,urllib.error.URLError) as error:
        findings['untrusted_ca_denied']=isinstance(error,ssl.SSLError) or isinstance(getattr(error,'reason',None),ssl.SSLError)
    try:
        urllib.request.urlopen('https://fixture:9444/health',context=context(),timeout=5)
        findings['missing_client_certificate_denied']=False
    except (ssl.SSLError,urllib.error.URLError) as error:
        findings['missing_client_certificate_denied']=isinstance(error,ssl.SSLError) or isinstance(getattr(error,'reason',None),ssl.SSLError)
    with urllib.request.urlopen('https://fixture:9444/health',context=context(mutual=True),timeout=5) as response:
        findings['trusted_mtls_client_accepted']=response.status==200
    if not all(findings.values()):
        raise RuntimeError('auth/TLS fixture qualification failed: '+json.dumps(findings))
    api('/admin/reset','POST',{})
    return findings


def qualify():
    deadline = time.monotonic() + 180
    while True:
        try:
            api('/fhir/metadata', client='observer')
            client = engine_client()
            break
        except Exception:
            if time.monotonic() > deadline:
                raise RuntimeError('lab readiness deadline exceeded')
            time.sleep(2)
    metadata = api('/fhir/metadata', client='observer')
    status,health=request('https://fixture:9443/health')
    health=json.loads(health)
    lock=json.loads((Path(__file__).parent/'lock.json').read_text())
    if status!=200 or any(lock['config_sha256'].get(name)!=digest for name,digest in health.get('code_sha256',{}).items()) or len(health.get('code_sha256',{}))!=2:
        raise RuntimeError('running fixture code does not match reviewed lock')
    version = engine(client, '/server/version', accept='text/plain').decode().strip()
    jvm = engine(client, '/server/jvm', accept='text/plain').decode().strip()
    if version != '4.6.0' or metadata.get('fhirVersion') != '4.0.1' or metadata.get('software', {}).get('version') != '8.6.0':
        raise RuntimeError('running target version does not match lock')
    undeploy(client)
    bootstrap_reset=api('/admin/reset','POST',{})
    receipt = {'schema': 'readmit-independent-lab-qualification/v1', 'oie_version': version, 'jvm': jvm, 'hapi_software': metadata['software'], 'fhir_version': metadata['fhirVersion'], 'cases': [], 'security': security_checks(), 'bootstrap_reset':bootstrap_reset,'fixture_code_sha256':health['code_sha256']}
    (EVIDENCE / 'oracle.json').write_bytes((FIXTURES/'expected.json').read_bytes())
    (EVIDENCE / 'capability-statement.json').write_text(json.dumps(metadata, indent=2))
    cases = json.loads((CONTROL / 'cases.json').read_text())
    for case in cases:
        undeploy(client)
        reset = api('/admin/reset', 'POST', {})
        api('/admin/revision', 'POST', case)
        for obj in json.loads((FIXTURES / 'baseline.json').read_text()):
            obj['meta'] = {'tag': [{'system': 'urn:readmit:independent-lab', 'code': case['generation']}]}
            api('/fhir/' + obj['resourceType'] + '/' + obj['id'], 'PUT', obj)
        for route, identifier in IDS.items():
            path = CHANNELS / (route + '-' + case['name'] + '.xml')
            engine(client, '/channels/' + identifier + '?override=true', 'PUT', path.read_bytes(), accept='text/plain')
            engine(client, '/channels/' + identifier + '/_deploy?returnErrors=true', 'POST')
            deadline = time.monotonic() + 30
            while True:
                status = engine(client, '/channels/' + identifier + '/status')
                if b'<state>STARTED</state>' in status:
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError('channel did not reach STARTED')
                time.sleep(0.5)
            exported = engine(client, '/channels/' + identifier)
            (EVIDENCE / (case['name'] + '-' + route + '-export.xml')).write_bytes(exported)
        acknowledgements = [send(6661, name, case['generation']) for name in ['siu-s12.hl7', 'siu-s13.hl7']]
        initial={'receiver':api('/receiver',client='observer')}
        acknowledgements += [send(6662, name, case['generation']) for name in ['adt-a01.hl7', 'siu-s12.hl7', 'siu-s13.hl7']]
        query='?_count=1000&_tag='+urllib.parse.quote('urn:readmit:independent-lab|'+case['generation'],safe='')+'&identifier='+urllib.parse.quote('urn:readmit:lab:appointment|lab-appointment-01',safe='')
        initial['v2-to-fhir']=api('/fhir/Appointment'+query,client='observer')
        acknowledgements += [send(6662, name, case['generation']) for name in ['orm-o01.hl7', 'oru-r01.hl7']]
        obj = json.loads((FIXTURES / 'native-appointment.json').read_text())
        obj['meta'] = {'tag': [{'system': 'urn:readmit:independent-lab', 'code': case['generation']}]}
        native_responses=[]
        api('/native/Appointment/' + obj['id'], 'PUT', obj,retain=native_responses)
        obj['start'], obj['end'] = '2030-01-02T10:00:00Z', '2030-01-02T10:30:00Z'
        api('/native/Appointment/' + obj['id'], 'PUT', obj,retain=native_responses)
        query='?_count=1000&_tag='+urllib.parse.quote('urn:readmit:independent-lab|'+case['generation'],safe='')+'&identifier='+urllib.parse.quote('urn:readmit:lab:appointment|lab-native-appointment-01',safe='')
        initial['fhir-native']=api('/fhir/Appointment'+query,client='observer')
        # Always observe the whole declared horizon, including delayed duplicates.
        time.sleep(json.loads((FIXTURES / 'expected.json').read_text())['late_observation_horizon_seconds'])
        receiver = api('/receiver', client='observer')
        observed = snapshot(case['generation'])
        verdicts = check(receiver, observed)
        expected_pass = case['mode'] in {'positive', 'corrected'}
        signatures=defect_matches(receiver,observed,case['defect'],initial,{'acknowledgements':acknowledgements,'native_responses':native_responses}) if not expected_pass else {}
        result = dict(case, defect_signatures=signatures, transport_positive=True, checks=verdicts, reset=reset, acknowledgements=acknowledgements,native_responses=native_responses)
        (EVIDENCE / (case['name'] + '-observations.json')).write_text(json.dumps({'schema':'readmit-independent-lab-acquisition/v1','receiver': receiver, 'fhir': observed,'initial':initial}, indent=2))
        receipt['cases'].append(result)
        (EVIDENCE / 'qualification.incomplete.json').write_text(json.dumps(receipt, indent=2))
        if any(value != expected_pass for value in verdicts.values()) or any(not value for value in signatures.values()):
            raise RuntimeError('independent oracle disagrees for ' + case['name'] + ': ' + json.dumps(verdicts))
        print(case['name'] + ': intended downstream state verified', flush=True)
    undeploy(client)
    receipt['final_reset'] = api('/admin/reset', 'POST', {})
    (EVIDENCE / 'qualification.json').write_text(json.dumps(receipt, indent=2))
    print('Qualified all three boundaries in 24 isolated revisions')


if __name__ == '__main__':
    prepare() if len(sys.argv) > 1 and sys.argv[1] == 'prepare' else qualify()
