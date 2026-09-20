"""Explicit native-test activation, never a shipped product trust authority.

The test-only public signed vector is valid through 2100 so archive smoke can
run with no Go or crypto library installed. Its randomly generated private key
was discarded; only the public key and signature are retained. Production does
not embed, discover, or trust this fixture.
"""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile


def activate(binary, work, environment):
    root = Path(tempfile.mkdtemp(prefix="test-operation-", dir=work)).resolve()
    fixtures = Path(__file__).resolve().parent / "testdata" / "operation"
    for name in ("entitlement.json", "trust.json"):
        shutil.copyfile(fixtures / name, root / name)
    policy = root / "operation-policy.json"
    policy.write_text(json.dumps({
        "schema": "readmit-operation-policy/v1",
        "entitlement": str(root / "entitlement.json"),
        "trust": str(root / "trust.json"), "state": str(root / "clock.json"),
        "author": "test-author", "device": "test-device",
        "authority": "test-runner", "admissions": str(root / "admissions.json"),
    }))
    result = subprocess.run([str(binary), "--operation-policy", str(policy),
                             "license", "operation", "activate"],
                            cwd=work, env=environment, capture_output=True, timeout=15)
    if result.returncode:
        raise RuntimeError("explicit test operation activation failed: " + repr(result.stderr))
    return ["--operation-policy", str(policy)]
