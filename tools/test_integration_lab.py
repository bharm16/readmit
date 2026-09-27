"""Fast offline checks; live target qualification is dispatch-only."""
import datetime
import importlib.util
import json
from pathlib import Path
import tempfile
import time
import unittest
from independent_lab import security


class IndependentLabTests(unittest.TestCase):
    def test_observer_scope_never_grants_write_or_delete(self):
        self.assertTrue(security.permitted('system/*.rs','GET','Appointment',True))
        for method in ['POST','PUT','DELETE']:
            self.assertFalse(security.permitted('system/*.rs',method,'Appointment'))
        self.assertFalse(security.permitted('system/*.cruds','POST','Subscription'))
        self.assertTrue(security.permitted('system/*.crus','PUT','Observation'))

    @unittest.skipUnless(importlib.util.find_spec('cryptography'), 'crypto fixture exercised by live lab container')
    def test_actual_rsa_signatures_audience_and_expiry(self):
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.asymmetric import rsa
        with tempfile.TemporaryDirectory() as directory:
            key = rsa.generate_private_key(public_exponent=65537,key_size=2048)
            path=Path(directory)/'key.pem'
            path.write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
            public=key.public_key().public_bytes(serialization.Encoding.PEM,serialization.PublicFormat.SubjectPublicKeyInfo)
            claims={'aud':'fixture','iat':1000,'exp':1100,'sub':'observer'}
            signed=security.sign(claims,path)
            self.assertEqual(security.verify(signed,public,'fixture',now=1001)['sub'],'observer')
            for audience,now in [('wrong',1001),('fixture',1100)]:
                with self.assertRaises(ValueError): security.verify(signed,public,audience,now=now)
            head,body,sig=signed.split('.')
            damaged=bytearray(security.unb64(sig));damaged[0]^=1
            with self.assertRaises(Exception):security.verify(head+'.'+body+'.'+security.b64(damaged),public,'fixture',now=1001)

    def test_compose_is_private_and_pinned(self):
        text=(Path(__file__).parent/'independent_lab/compose.yaml').read_text()
        self.assertIn('internal: true',text)
        for line in text.splitlines():
            if '::' in line:self.assertIn('127.0.0.1::',line)
            if 'image:' in line and 'readmit-lab-' not in line:self.assertIn('@sha256:',line)
        self.assertNotIn(':latest',text)

    def test_target_and_oracle_are_separate(self):
        root=Path(__file__).parent/'independent_lab'
        for name in ['runtime.py','channel.js']:
            source=(root/name).read_text()
            self.assertNotIn('expected.json',source)
            self.assertNotIn('internal/',source)
        oracle=json.loads((Path(__file__).parent.parent/'testdata/integration-lab/expected.json').read_text())
        self.assertGreater(oracle['late_observation_horizon_seconds'],1.2)




class QualificationOracleTests(unittest.TestCase):
    def positive(self):
        import base64
        from independent_lab import qualify
        expected=json.loads((Path(__file__).parent.parent/'testdata/integration-lab/expected.json').read_text())
        receiver={'generation':'lab-unit','messages':[]}
        for trigger,stamp in [('S12','20300102093000+0000'),('S13','20300102100000+0000')]:
            raw=('MSH|^~\\&|OIE-LAB|LAB|||20300101000000||SIU^'+trigger+'|LAB-'+trigger+'|P|2.5.1\rPID|1||lab-patient-01^LAB\rSCH|lab-appointment-01^LAB||||||||||^^^'+stamp+'\r').encode()
            receiver['messages'].append({'payload_base64':base64.b64encode(raw).decode(),'received_at_ns':1000000000})
        def bundle(values):return {'entry':[{'resource':v} for v in values]}
        appointments=[]
        for id,patient in [('lab-appointment-01','lab-patient-01'),('lab-native-appointment-01','lab-native-patient-01')]:
            appointments.append({'id':id,'identifier':[{'system':'urn:readmit:lab:appointment','value':id}],'start':'2030-01-02T10:00:00Z','status':'booked','participant':[{'actor':{'reference':'Patient/'+patient}}]})
        observed={'Appointment':bundle(appointments),'Patient':bundle([{'id':'lab-patient-01','name':[{'family':'Lark'}]}]),'Encounter':bundle([{'id':'lab-encounter-01','status':'in-progress','subject':{'reference':'Patient/lab-patient-01'}}]),'ServiceRequest':bundle([{'id':'lab-order-01','subject':{'reference':'Patient/lab-patient-01'}}]),'Observation':bundle([{'id':'lab-observation-'+str(i+1),'valueString':v,'code':{'text':['Fictional glucose','Fictional sodium'][i]},'status':'final','subject':{'reference':'Patient/lab-patient-01'},'basedOn':[{'reference':'ServiceRequest/lab-order-01'}]} for i,v in enumerate(['100','140'])]),'DiagnosticReport':bundle([{'status':'final','subject':{'reference':'Patient/lab-patient-01'},'basedOn':[{'reference':'ServiceRequest/lab-order-01'}],'result':[{'reference':'Observation/lab-observation-1'},{'reference':'Observation/lab-observation-2'}]}])}
        self.assertTrue(all(qualify.check(receiver,observed,expected).values()))
        return receiver,observed,expected

    def test_each_order_result_link_is_required(self):
        import copy
        from independent_lab import qualify
        receiver,observed,expected=self.positive()
        variants=[]
        changed=copy.deepcopy(observed);changed['ServiceRequest']['entry']=[];variants.append(changed)
        changed=copy.deepcopy(observed);changed['ServiceRequest']['entry'][0]['resource']['subject']['reference']='Patient/wrong';variants.append(changed)
        changed=copy.deepcopy(observed);changed['Observation']['entry'][0]['resource']['subject']['reference']='Patient/wrong';variants.append(changed)
        changed=copy.deepcopy(observed);changed['DiagnosticReport']['entry'][0]['resource']['result'][0]['reference']='Observation/wrong';variants.append(changed)
        changed=copy.deepcopy(observed);changed['Observation']['entry'][0]['resource']['id']='wrong-result';variants.append(changed)
        changed=copy.deepcopy(observed);changed['Observation']['entry'][0]['resource']['valueString']='140';variants.append(changed)
        for variant in variants:self.assertFalse(qualify.check(receiver,variant,expected)['v2-to-fhir'])

    def test_immediate_fhir_duplicates_are_not_late_qualification(self):
        import copy
        import datetime
        from independent_lab import qualify
        receiver,observed,_=self.positive()
        initial={'receiver':copy.deepcopy(receiver),'v2-to-fhir':{'entry':[copy.deepcopy(observed['Appointment']['entry'][0])]},'fhir-native':{'entry':[copy.deepcopy(observed['Appointment']['entry'][1])]}}
        time_ns=int(datetime.datetime(2030,1,1,tzinfo=datetime.timezone.utc).timestamp()*1000000000)
        transport={'acknowledgements':[{'ack_received_at_ns':time_ns} for _ in range(7)],'native_responses':[{'received_at_ns':time_ns} for _ in range(2)]}
        receiver['messages'].append(copy.deepcopy(receiver['messages'][-1]));receiver['messages'][-1]['received_at_ns']=time_ns+1500000000
        extras=[]
        for item,start in [(observed['Appointment']['entry'][0],'2030-01-02T10:00:00Z'),(observed['Appointment']['entry'][1],'2030-01-02T09:30:00Z'),(observed['Appointment']['entry'][1],'2030-01-02T10:00:00Z')]:
            extra=copy.deepcopy(item);extra['resource']['id']='duplicate-'+str(len(extras));extra['resource']['start']=start;extra['resource']['meta']={'lastUpdated':'2030-01-01T00:00:00.100Z'};extras.append(extra)
        observed['Appointment']['entry'].extend(extras)
        results=qualify.defect_matches(receiver,observed,'late-duplicate',initial,transport)
        self.assertTrue(results['v2-to-v2']);self.assertFalse(results['v2-to-fhir']);self.assertFalse(results['fhir-native'])
        for entry in extras:entry['resource']['meta']['lastUpdated']='2030-01-01T00:00:01.500Z'
        self.assertTrue(all(qualify.defect_matches(receiver,observed,'late-duplicate',initial,transport).values()))


class EvidenceExportTests(unittest.TestCase):
    def test_completion_markers_do_not_qualify_missing_acquisitions(self):
        from independent_lab import evidence
        for files in [{},{'controller-complete.json':b'{}','teardown.json':b'{}'}]:
            if files:
                with self.assertRaises(ValueError):evidence.verified_export(files)
            else:self.assertFalse(evidence.verified_export(files))

    def test_inventory_refuses_links_and_overlarge_file_before_reading(self):
        from independent_lab import evidence
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'qualification.json'
            with path.open('wb') as stream:stream.truncate(17*1024*1024)
            with self.assertRaises(ValueError):evidence.read_files(directory)
            path.unlink();path.symlink_to('/etc/hosts')
            with self.assertRaises(ValueError):evidence.read_files(directory)

class AcquiredEvidenceIntegrityTests(unittest.TestCase):
    def test_acquisition_removal_and_mutation_cannot_remain_qualified(self):
        import hashlib
        import os
        from independent_lab import evidence
        retained=Path(__file__).parent.parent/'testdata/integration-lab/qualification/recreation-20260927'
        directory=Path(os.environ.get('READMIT_LAB_EVIDENCE',retained))
        files=evidence.read_files(directory)
        self.assertTrue(evidence.verified_export(files))
        manifest=json.loads((directory/'manifest.json').read_text())
        self.assertEqual(manifest['schema'],'readmit-independent-lab-evidence/v1')
        self.assertIs(manifest['qualified'],True)
        self.assertEqual(manifest['files'],{name:{'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)} for name,raw in files.items()})
        for name in ['qualification.json','duplicate-positive-observations.json','duplicate-positive-v2v2-export.xml','duplicate-positive-v2fhir-export.xml']:
            damaged=dict(files);del damaged[name]
            with self.assertRaises((ValueError,KeyError)):evidence.verified_export(damaged)
        damaged=dict(files);name='duplicate-positive-v2v2-export.xml';damaged[name]=damaged[name].replace(b'<description>',b'<description>Changed ',1)
        with self.assertRaises(ValueError):evidence.verified_export(damaged)


if __name__=='__main__':unittest.main()
