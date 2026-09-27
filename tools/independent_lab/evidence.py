"""Bound and verify the actual acquisitions before publishing qualification."""
import base64
import hashlib
import json
from pathlib import Path
import re
import stat
import xml.etree.ElementTree as ET

SCHEMA = 'readmit-independent-lab-qualification/v1'
ROUTES = {'v2-to-v2','v2-to-fhir','fhir-native'}
SECURITY = {'unauthenticated_denied','observer_write_denied','observer_delete_denied','wrong_audience_denied','expired_assertion_denied','bad_signature_denied','scope_escalation_denied','assertion_replay_denied','untrusted_ca_denied','missing_client_certificate_denied','trusted_mtls_client_accepted'}
FIXTURES = ['siu-s12.hl7','siu-s13.hl7','adt-a01.hl7','siu-s12.hl7','siu-s13.hl7','orm-o01.hl7','oru-r01.hl7']
CONTROLS = ['LAB-S12','LAB-S13','LAB-ADT','LAB-S12','LAB-S13','LAB-ORM','LAB-ORU']


def decode(raw):
    def unique(pairs):
        result={}
        for key,value in pairs:
            if key in result:raise ValueError('duplicate evidence member')
            result[key]=value
        return result
    return json.loads(raw,object_pairs_hook=unique)


def read_files(directory):
    selected=[]
    total=0
    allowed={'oracle.json','controller-complete.json','qualification.json','qualification.incomplete.json','capability-statement.json','runtime.json','teardown.json'}
    for path in sorted(Path(directory).iterdir()):
        info=path.lstat()
        if not stat.S_ISREG(info.st_mode):raise ValueError('evidence members must be regular files')
        if path.name not in allowed and not path.name.endswith(('-observations.json','-export.xml')):continue
        total+=info.st_size
        if info.st_size>16*1024*1024 or total>128*1024*1024 or len(selected)>=100:
            raise ValueError('evidence package exceeds its bounds')
        selected.append((path,info))
    files={}
    for path,info in selected:
        # Refuse substitutions since the bounded inventory, including links.
        import os
        descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
        with os.fdopen(descriptor,'rb') as stream:
            opened=os.fstat(stream.fileno())
            if not stat.S_ISREG(opened.st_mode) or (opened.st_dev,opened.st_ino)!=(info.st_dev,info.st_ino):raise ValueError('evidence changed during reading')
            raw=stream.read(info.st_size+1)
        if len(raw)!=info.st_size:raise ValueError('evidence size changed during reading')
        files[path.name]=raw
    return files


def hashes(files):
    return {name:hashlib.sha256(raw).hexdigest() for name,raw in sorted(files.items()) if name not in {'controller-complete.json','teardown.json','qualification.incomplete.json'}}


def validate(files):
    # The independent oracle is shared by the collector and its verifying reader;
    # neither the native target nor OIE imports this module or its oracle.
    from . import qualify
    required={'qualification.json','runtime.json','capability-statement.json','oracle.json'}
    if not required<=files.keys():raise ValueError('qualification evidence is incomplete')
    receipt=decode(files['qualification.json'])
    runtime=decode(files['runtime.json'])
    capability=decode(files['capability-statement.json'])
    if receipt.get('schema')!=SCHEMA or receipt.get('oie_version')!='4.6.0' or receipt.get('fhir_version')!='4.0.1' or receipt.get('hapi_software',{}).get('version')!='8.6.0':raise ValueError('qualification version mismatch')
    if capability.get('fhirVersion')!='4.0.1' or capability.get('software',{}).get('version')!='8.6.0':raise ValueError('capability version mismatch')
    if runtime.get('schema')!='readmit-independent-lab-runtime/v1' or runtime.get('postgres_version','').split()[0]!='16.11' or '17.0.17' not in runtime.get('java','') or runtime.get('network_internal') is not True:raise ValueError('runtime qualification is incomplete')
    if set(runtime.get('images',{}))!={'engine','hapi','postgres','fixture'} or not runtime.get('config_sha256'):raise ValueError('runtime image/config pins are absent')
    if set(receipt.get('security',{}))!=SECURITY or any(value is not True for value in receipt['security'].values()):raise ValueError('security qualification is incomplete')
    if receipt.get('final_reset',{}).get('verified_empty') is not True:raise ValueError('final reset is unverified')
    expected={defect+'-'+mode for defect in qualify.DEFECTS for mode in qualify.MODES}
    cases=receipt.get('cases',[])
    if len(cases)!=24 or {case.get('name') for case in cases}!=expected:raise ValueError('qualification case matrix is incomplete')
    generations=set()
    oracle=decode(files['oracle.json'])
    if hashlib.sha256(files['oracle.json']).hexdigest()!=runtime['config_sha256'].get('testdata/integration-lab/expected.json'):raise ValueError('oracle differs from qualified configuration')
    for case in cases:
        name=case['name'];generation=case['generation']
        if not re.fullmatch(r'lab-[a-z0-9-]{1,48}',generation) or generation in generations:raise ValueError('qualification reused a generation')
        if generations and case['reset'].get('generation')!=cases[len(generations)-1]['generation']:raise ValueError('reset belongs to a different generation')
        generations.add(generation)
        if case['mode'] not in qualify.MODES or case['defect'] not in qualify.DEFECTS or name!=case['defect']+'-'+case['mode']:raise ValueError('case revision mismatch')
        needed={name+'-observations.json',name+'-v2v2-export.xml',name+'-v2fhir-export.xml'}
        required.update(needed)
        if not needed<=files.keys():raise ValueError('missing actual target acquisition')
        acquisition=decode(files[name+'-observations.json'])
        if acquisition.get('schema')!='readmit-independent-lab-acquisition/v1' or acquisition.get('receiver',{}).get('generation')!=generation:raise ValueError('acquisition generation mismatch')
        checks=qualify.check(acquisition['receiver'],acquisition['fhir'],oracle)
        should_pass=case['mode'] in {'positive','corrected'}
        if set(case.get('checks',{}))!=ROUTES or checks!=case['checks'] or any(value is not should_pass for value in checks.values()):raise ValueError('retained oracle result does not reproduce')
        signatures=qualify.defect_matches(acquisition['receiver'],acquisition['fhir'],case['defect'],acquisition['initial'],case) if not should_pass else {}
        if signatures!=case.get('defect_signatures') or any(value is not True for value in signatures.values()):raise ValueError('named defect evidence does not reproduce')
        if case.get('reset',{}).get('verified_empty') is not True or case.get('transport_positive') is not True:raise ValueError('case setup or transport is incomplete')
        if len(case.get('acknowledgements',[]))!=7 or len(case.get('native_responses',[]))!=2:raise ValueError('transport receipts are incomplete')
        for ack,filename,control in zip(case['acknowledgements'],FIXTURES,CONTROLS):
            if ack.get('fixture')!=filename:raise ValueError('stimulus fixture ordering mismatch')
            raw=base64.b64decode(ack['ack_base64'],validate=True)
            msa=[line.split(b'|') for line in raw.split(b'\r') if line.startswith(b'MSA|')]
            source=base64.b64decode(ack['input_base64'],validate=True)
            if ('ZLG|'+generation+'\r').encode() not in source:raise ValueError('stimulus generation mismatch')
            if not raw.startswith(b'\x0b') or not raw.endswith(b'\x1c\r') or len(msa)!=1 or msa[0][1:3]!=[b'AA',control.encode()] or ack['input_sha256']!=hashlib.sha256(source).hexdigest():raise ValueError('acknowledgement does not bind exact stimulus')
        for response in case['native_responses']:
            raw=base64.b64decode(response['body_base64'],validate=True)
            if response['status'] not in (200,201) or response['body_sha256']!=hashlib.sha256(raw).hexdigest():raise ValueError('native response was not retained intact')
        for route,identifier in qualify.IDS.items():
            channel=ET.fromstring(files[name+'-'+route+'-export.xml'])
            if channel.tag!='channel' or channel.attrib.get('version')!='4.6.0' or channel.findtext('id')!=identifier:raise ValueError('wrong engine channel export')
    unexpected={name for name in files if name.endswith(('-observations.json','-export.xml'))}-required
    if unexpected:raise ValueError('unrelated acquisitions in qualification')
    if receipt['final_reset'].get('generation')!=cases[-1]['generation']:raise ValueError('final reset belongs to a different generation')
    return hashes({name:files[name] for name in required})


def verified_export(files):
    if 'controller-complete.json' not in files:return False
    bound=validate(files)
    complete=decode(files['controller-complete.json'])
    if complete.get('schema')!='readmit-independent-lab-controller/v1' or complete.get('verified') is not True or type(complete.get('postgres_fhir_resource_rows')) is not int or complete['postgres_fhir_resource_rows']<=0 or complete.get('acquisition_sha256')!=bound:raise ValueError('completed acquisitions were changed or not bound')
    if 'teardown.json' not in files:return False
    teardown=decode(files['teardown.json'])
    if teardown.get('schema')!='readmit-independent-lab-teardown/v1' or any(teardown.get(key) is not True for key in ('containers_absent','volumes_absent','networks_absent')):raise ValueError('teardown is unverified')
    return True
