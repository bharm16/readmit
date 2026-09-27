"""Create, qualify and remove only an owned independent synthetic lab."""
import argparse
import json
import hashlib
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
from independent_lab import evidence

HERE = Path(__file__).resolve().parent
LAB = HERE / "independent_lab"


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def owner(state):
    state = Path(state).resolve(strict=True)
    if state.is_symlink() or not state.is_dir():
        raise ValueError("lab state must be a real directory")
    info = json.loads((state / "owner.json").read_text())
    if info.get("schema") != "readmit-independent-lab/v1" or not re.fullmatch(r"readmit-lab-[0-9a-f]{16}", info.get("project", "")) or info.get("state") != str(state):
        raise ValueError("state does not identify an owned lab")
    return state, info


def compose(state, *args):
    state, info = owner(state)
    return run(["docker", "compose", "--project-name", info["project"], "--env-file", str(state / "lab.env"), "-f", str(LAB / "compose.yaml"), *args])


def initialize(path):
    path = Path(path).absolute()
    for parent in (path.parent.resolve(), *path.parent.resolve().parents):
        if (parent/'identity.sha256').exists() or (parent/'family.json').exists():
            raise ValueError('lab state must be outside retained evidence')
    path.mkdir(mode=0o700)
    project = "readmit-lab-" + secrets.token_hex(8)
    for name in ("channels", "evidence", "control"):
        (path / name).mkdir(mode=0o700)
    (path / "owner.json").write_text(json.dumps({"schema": "readmit-independent-lab/v1", "project": project, "state": str(path.resolve()), "config_root": str(LAB)}))
    env = path / "lab.env"
    env.write_text("LAB_STATE=" + str(path.resolve()) + "\nLAB_DB_PASSWORD=" + secrets.token_hex(32) + "\n")
    env.chmod(0o600)
    run(["docker", "build", "-f", str(LAB / "Dockerfile.fixture"), "-t", "readmit-lab-fixture:1", str(LAB)])
    run(["docker", "run", "--rm", "--network", "none", "-v", str(HERE) + ":/lab:ro", "-v", str(path) + ":/state", "readmit-lab-fixture:1", "python", "-c", "from independent_lab.security import provision; provision('/state/secrets')"])
    root = path / "secrets"
    subsets = {"engine": ["ca.pem", "engine.p12", "keystore-password", "engine-client-key.pem"], "fixture": ["ca.pem", "fixture.pem", "fixture-key.pem", "issuer-key.pem", "issuer-public.pem", "clients.json"], "clients": ["ca.pem", "setup-key.pem", "observer-key.pem", "tls-client.pem", "tls-client-key.pem", "engine-admin-password"]}
    for group, names in subsets.items():
        destination = root / group
        destination.mkdir(mode=0o700)
        for name in names:
            shutil.copyfile(root / name, destination / name)
            (destination / name).chmod(0o600)
    print("Initialized isolated lab " + project)


def verify_configuration():
    lock = json.loads((LAB / 'lock.json').read_text())
    if lock.get('schema') != 'readmit-independent-lab-lock/v1' or not lock.get('config_sha256'):
        raise ValueError('lab configuration lock is missing')
    for name, expected in lock['config_sha256'].items():
        if not name.startswith(('tools/independent_lab/', 'tools/integration_lab.py', 'testdata/integration-lab/')) or '..' in Path(name).parts:
            raise ValueError('invalid locked member')
        if hashlib.sha256((HERE.parent / name).read_bytes()).hexdigest() != expected:
            raise ValueError('lab configuration differs from reviewed lock: ' + name)
    return lock


def docker_ids(state):
    _, info = owner(state)
    result = subprocess.check_output(['docker','ps','-aq','--filter','label=com.docker.compose.project='+info['project']], text=True)
    return result.split()


def record_runtime(state):
    state, info = owner(state)
    images = {}
    for identifier in docker_ids(state):
        fields = json.loads(subprocess.check_output(['docker','inspect','--format','{{json .Config.Labels}}',identifier]))
        service = fields.get('com.docker.compose.service')
        if service in ('engine','hapi','postgres','fixture'):
            images[service] = subprocess.check_output(['docker','inspect','--format','{{.Image}}',identifier],text=True).strip()
    if set(images) != {'engine','hapi','postgres','fixture'}:
        raise ValueError('lab target topology is incomplete')
    pg_container = info['project']+'-postgres-1'
    postgres = subprocess.check_output(['docker','exec',pg_container,'psql','-U','lab','-d','hapi','-Atc','show server_version'],text=True).strip()
    if postgres.split()[0]!='16.11':
        raise ValueError('PostgreSQL runtime does not match lock')
    java = subprocess.run(['docker','exec',info['project']+'-engine-1','java','-version'],check=True,capture_output=True,text=True).stderr
    if '17.0.17' not in java:
        raise ValueError('engine is not using pinned Java 17')
    internal=subprocess.check_output(['docker','network','inspect',info['project']+'_lab','--format','{{.Internal}}'],text=True).strip()
    if internal!='true':raise ValueError('lab network is not isolated')
    runtime = {'network_internal':True,'schema':'readmit-independent-lab-runtime/v1','images':images,'postgres_version':postgres,'java':java,'platform':subprocess.check_output(['docker','info','--format','{{.OSType}}/{{.Architecture}}'],text=True).strip(),'config_sha256':verify_configuration()['config_sha256']}
    (state/'evidence/runtime.json').write_text(json.dumps(runtime,indent=2))


def export_evidence(state):
    state, info = owner(state)
    files=evidence.read_files(state/'evidence')
    qualified=evidence.verified_export(files)
    needles = [p.read_bytes() for p in (state/'secrets').rglob('*') if p.is_file()]
    needles.append(next(line.split('=',1)[1].encode() for line in (state/'lab.env').read_text().splitlines() if line.startswith('LAB_DB_PASSWORD=')))
    for raw in files.values():
        if b'-----BEGIN PRIVATE KEY-----' in raw or re.search(rb'eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+',raw) or any(value and value in raw for value in needles):
            raise ValueError('credential-shaped material found; evidence was not exported')
    destination=state/'export'
    destination.mkdir(mode=0o700)
    manifest={'schema':'readmit-independent-lab-evidence/v1','qualified':qualified,'files':{}}
    for name,raw in sorted(files.items()):
        with (destination/name).open('xb') as output:output.write(raw)
        manifest['files'][name]={'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
    (destination/'manifest.json').write_text(json.dumps(manifest,indent=2))
    print('Exported credential-free synthetic reference evidence')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["init", "up", "qualify", "down", "export"])
    parser.add_argument("state", type=Path)
    args = parser.parse_args()
    if args.action in ("init","up","qualify"):verify_configuration()
    if args.action == "init":
        initialize(args.state)
    elif args.action == "up":
        compose(args.state, "up", "--build", "--force-recreate", "-d")
    elif args.action == "qualify":
        state,_=owner(args.state)
        if (state/"evidence/qualification.json").exists():raise ValueError("qualified evidence is immutable; initialize a new lab")
        record_runtime(args.state)
        compose(args.state, "run", "--rm", "qualifier", "python", "-m", "independent_lab.qualify", "prepare")
        compose(args.state, "exec", "-T", "engine", "bash", "-c", 'java -cp "/opt/lab:$(cat /opt/lab/classpath)" ChannelFactory --all /lab-channels')
        compose(args.state, "run", "--rm", "qualifier")
        state,info=owner(args.state)
        rows=int(subprocess.check_output(['docker','exec',info['project']+'-postgres-1','psql','-U','lab','-d','hapi','-Atc','select count(*) from hfj_resource'],text=True).strip())
        if rows<=0:raise ValueError('HAPI did not persist resources in PostgreSQL')
        bound=evidence.validate(evidence.read_files(state/'evidence'))
        (state/'evidence/controller-complete.json').write_text(json.dumps({'schema':'readmit-independent-lab-controller/v1','postgres_fhir_resource_rows':rows,'verified':True,'acquisition_sha256':bound}))
    elif args.action == "export":
        export_evidence(args.state)
    else:
        # Random project names and exact state ownership prevent global cleanup.
        state,info=owner(args.state)
        if info.get('config_root')!=str(LAB):raise ValueError('lab configuration owner differs')
        compose(args.state, "down", "--volumes", "--remove-orphans")
        if docker_ids(args.state):raise ValueError('owned lab containers remain')
        volumes=subprocess.check_output(['docker','volume','ls','-q','--filter','label=com.docker.compose.project='+info['project']],text=True).strip()
        if volumes:raise ValueError('owned lab volumes remain')
        networks=subprocess.check_output(['docker','network','ls','-q','--filter','label=com.docker.compose.project='+info['project']],text=True).strip()
        if networks:raise ValueError('owned lab networks remain')
        (state/'evidence/teardown.json').write_text(json.dumps({'schema':'readmit-independent-lab-teardown/v1','containers_absent':True,'volumes_absent':True,'networks_absent':True}))


if __name__ == "__main__":
    main()
