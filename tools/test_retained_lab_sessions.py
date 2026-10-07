"""Reopen retained real product/lab acquisitions offline, without starting Docker."""
import hashlib
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from independent_lab import evidence, session


class RetainedProductSessionTests(unittest.TestCase):
    def test_real_product_cycle_has_original_stimuli_aa_receipts_and_independent_state(self):
        archive = Path(__file__).resolve().parent.parent/'testdata/integration-lab/qualification/product-sessions-20261006.tar.gz'
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary).resolve()
            with tarfile.open(archive,'r:gz') as package:
                members=package.getmembers()
                self.assertLess(sum(item.size for item in members),1024*1024*1024)
                self.assertLess(len(members),10000)
                regular=set()
                for item in members:
                    self.assertFalse(Path(item.name).is_absolute())
                    self.assertNotIn('..',Path(item.name).parts)
                    self.assertTrue(item.isfile() or item.isdir() or item.islnk())
                    if item.islnk():
                        self.assertIn(item.linkname,regular)
                    if item.isfile():regular.add(item.name)
                package.extractall(root,filter='data')
            manifest=session.verify_export(root/'session')
            self.assertEqual(manifest['state'],'stopped')
            teardown=json.loads((root/'session/teardown.json').read_text())
            for key in ('containers_absent','volumes_absent','networks_absent'):self.assertTrue(teardown[key])
            receipts=[];builds=[];oracles=[]
            for generation in sorted((root/'session').glob('lab-*')):
                product=generation/'product'
                if not product.exists():continue
                receipt=json.loads((product/'qualification.json').read_text())
                receipts.append(receipt)
                builds.append(json.loads((product/'build.json').read_text())['sha256'])
                product_manifest=json.loads((product/'manifest.json').read_text())
                for name,digest in product_manifest['files'].items():
                    self.assertEqual(hashlib.sha256((product/name).read_bytes()).hexdigest(),digest)
                witness=json.loads((product/'witness-after.json').read_text())
                appointments=[entry['resource'] for entry in witness.get('entry',[])]
                defective=receipt['mode'] in ('defective','reintroduced')
                self.assertEqual(len(appointments),2 if defective else 1)
                self.assertEqual(len({item['id'] for item in appointments}),len(appointments))
                self.assertEqual({item['start'] for item in appointments},{'2030-01-02T09:30:00Z','2030-01-02T10:00:00Z'} if defective else {'2030-01-02T10:00:00Z'})
                for item in appointments:
                    self.assertEqual(item['resourceType'],'Appointment')
                    self.assertEqual(item['status'],'booked')
                    self.assertIn({'system':'urn:readmit:lab:appointment','value':'lab-appointment-01'},item['identifier'])
                    self.assertIn({'system':'urn:readmit:independent-lab','code':generation.name},item['meta']['tag'])
                authored={}
                for phase in ('book','move'):
                    typed=json.loads((product/'inputs'/(phase+'-checks.json')).read_text())['assertions']
                    wire=json.loads((product/'inputs'/(phase+'-ack.json')).read_text())['assertions']
                    for check in wire:check['subject']['field'].pop('message')
                    authored[phase]={'typed':typed,'wire':wire}
                oracles.append(json.dumps(authored,sort_keys=True))
                result=json.loads((product/'result/manifest.json').read_text())
                self.assertEqual(result['state'],'complete')
                self.assertEqual(result['verdict'],'fail' if defective else 'pass')
                for phase,filename in [('book','siu-s12.hl7'),('move','siu-s13.hl7')]:
                    payloads=product/'result/phases'/phase/'transport/run/payloads'
                    sent=(payloads/'o000001-sent.bin').read_bytes()
                    self.assertEqual(sent,b'\x0b'+(generation/'stimuli'/filename).read_bytes()+b'\x1c\r')
                    ack=(payloads/'o000001-received.bin').read_bytes()
                    control=sent[1:].split(b'\r')[0].split(b'|')[9]
                    self.assertIn(b'\rMSA|AA|'+control,ack)
                if (generation/'tunnel-stop.json').exists():
                    self.assertTrue(json.loads((generation/'tunnel-stop.json').read_text())['listeners_closed'])
            self.assertEqual({item['mode'] for item in receipts},{'defective','corrected','reintroduced'})
            self.assertEqual(len(receipts),3)
            self.assertEqual(len(set(builds)),1)
            self.assertEqual(len(set(oracles)),1)
            self.assertEqual(len({json.dumps(item['assertions'],sort_keys=True) for item in receipts}),1)
            self.assertEqual(len({item['assertion_oracle_sha256'] for item in receipts}),1)
            self.assertTrue(evidence.verified_export(evidence.read_files(root/'reference')))


if __name__=='__main__':unittest.main()
