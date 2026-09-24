"""Prove the independent verification suite fails when readmit's behavior changes.

Each entry below alters one documented behavior in a throwaway copy of the Go
sources, rebuilds readmit from that copy, and requires the named check in
`verify.py` to fail. A mutation the suite does not notice is a finding about the
suite, not a detail to paper over, so this script reports it as SURVIVED and
exits nonzero.

The repository tree is never modified: every build happens in a temporary
directory, and the corpus and the verifier are always the unmutated originals.
"""

import argparse
from collections import namedtuple
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parent.parent
VERIFY = Path(__file__).resolve().parent / "verify.py"
COPIED = ("cmd", "internal")
COPIED_FILES = ("go.mod", "go.sum")

Mutation = namedtuple("Mutation", "name path old new check rationale")

MUTATIONS = (
    Mutation(
        name="explicit-null-read-as-empty",
        path="internal/hl7/parse.go",
        old='\tif bytes.Equal(source[span.Start:span.End], []byte(`""`)) {\n\t\treturn Null\n\t}\n',
        new="",
        check="corpus-inspect",
        rationale="an explicit HL7 null would become an ordinary present value",
    ),
    Mutation(
        name="repetitions-hidden-from-display",
        path="internal/operation/inspect.go",
        old="if len(field.Repetitions) < 2 {",
        new="if len(field.Repetitions) < 3 {",
        check="corpus-inspect",
        rationale="a two-repetition field would be displayed as a single value",
    ),
    Mutation(
        name="ambiguous-acknowledgement-claimed-matched",
        path="internal/bundle/capture.go",
        old="\t\tdefault:\n\t\t\tlink.Kind = AmbiguousACK\n\t\t}",
        new="\t\tdefault:\n\t\t\tlink.Kind = Matched\n\t\t}",
        check="corpus-capture",
        rationale="an acknowledgement with several candidates would claim one correlation",
    ),
    Mutation(
        name="frame-ends-with-a-newline",
        path="internal/mllp/framing.go",
        old="return append(frame, 0x1c, '\\r')",
        new="return append(frame, 0x1c, '\\n')",
        check="endpoint-accept",
        rationale="outbound MLLP blocks would end with the wrong terminator byte",
    ),
    Mutation(
        name="any-acknowledgement-code-accepted",
        path="internal/replay/transport.go",
        old='\tcase "AE":\n\t\treturn ApplicationError',
        new='\tcase "AE":\n\t\treturn Accepted',
        check="endpoint-negative",
        rationale="an application error would be recorded as an acceptance",
    ),
    Mutation(
        name="timeout-recorded-as-rejection",
        path="internal/replay/transport.go",
        old='\tcase "timeout":\n\t\treturn Timeout',
        new='\tcase "timeout":\n\t\treturn Rejected',
        check="endpoint-negative",
        rationale="an unknown delivery result would be recorded as a negative application result",
    ),
    Mutation(
        name="interrupt-no-longer-cancels",
        path="internal/cli/operation.go",
        old="signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)",
        new="signal.NotifyContext(cmd.Context(), os.Kill, syscall.SIGTERM)",
        check="endpoint-cancel",
        rationale="an interrupt would not cancel an outstanding replay",
    ),
    Mutation(
        name="defective-mode-updates-in-place",
        path="internal/receiver/message.go",
        old="if r.config.Mode == observation.Fixed {",
        new="if r.config.Mode != observation.Fixed {",
        check="listener-ledger",
        rationale="the fixture's scheduling defect would move to the other mode",
    ),
    Mutation(
        name="acknowledgement-echoes-a-constant",
        path="internal/receiver/message.go",
        old='msa := []string{"MSA", code, request.controlID}',
        new='msa := []string{"MSA", code, "READMIT-FIXTURE"}',
        check="listener-ledger",
        rationale="an acknowledgement would no longer echo the control ID it answers",
    ),
    Mutation(
        name="unsupported-trigger-accepted",
        path="internal/receiver/message.go",
        old='if typeName != profile.MessageType || r.action == "" || version != profile.HL7Version {',
        new="if typeName != profile.MessageType || version != profile.HL7Version {",
        check="listener-negative",
        rationale="a trigger outside the fixture profile would reach the ledger",
    ),
    Mutation(
        name="original-mode-recorded-as-enhanced",
        path="internal/receiver/collector.go",
        old="return c.singleAnswer(composer, controlID, collection.OriginalMode, code, reason, ordinal)",
        new="return c.singleAnswer(composer, controlID, collection.EnhancedMode, code, reason, ordinal)",
        check="collector-original",
        rationale="a sender that asked for one acknowledgement would be recorded as asking for two stages",
    ),
    Mutation(
        name="acknowledgement-stages-share-a-control-id",
        path="internal/receiver/collector.go",
        old='"READMITACC"',
        new='"READMITAPP"',
        check="collector-enhanced",
        rationale="the commit and application acknowledgements would be indistinguishable to their receiver",
    ),
    Mutation(
        name="application-stage-answers-on-the-receiving-connection",
        path="internal/receiver/collector.go",
        old="if result.application.Destination == collection.SameConnection {",
        new="if result.application.Destination != collection.NoDestination {",
        check="collector-separate-endpoint",
        rationale="a separately configured application endpoint would be ignored and the answer sent back on one socket",
    ),
    Mutation(
        name="undelivered-application-stage-still-claimed",
        path="internal/receiver/collector.go",
        old="\t\t// Nothing reached the peer, so nothing is retained and no stage is\n"
            "\t\t// claimed. An absent application acknowledgement is not a rejection.\n"
            "\t\tdowngrade(&entry.Application, reasonUndelivered)\n"
            "\t\treturn nil\n",
        new="\t\treturn nil\n",
        check="collector-application-timeout",
        rationale="an application acknowledgement that never left the process would be recorded as delivered",
    ),
    Mutation(
        name="undeclared-acknowledgement-condition-accepted",
        path="internal/collection/policy.go",
        old="return value == Always || value == Never || value == OnError || value == OnSuccess",
        new="return true",
        check="collector-unsupported-mode",
        rationale="an acknowledgement mode this receiver does not implement would be treated as declared",
    ),
)

# Checks no mutation can exercise, recorded so the gap is a decision rather than
# an oversight.
UNMUTATED_CHECKS = {
    "engine-export-corpus": "reports the absence of an approved engine-export corpus (issue #35); "
                            "there is no readmit behavior to alter",
}


class MutationError(Exception):
    """The catalogue no longer describes the source it claims to mutate."""


def apply_mutation(mutation, destination):
    """Copy the buildable Go tree into destination and alter exactly one behavior."""
    destination.mkdir(parents=True, exist_ok=True)
    for name in COPIED_FILES:
        shutil.copy2(ROOT / name, destination / name)
    for name in COPIED:
        shutil.copytree(ROOT / name, destination / name)
    target = destination / mutation.path
    source = target.read_text(encoding="utf-8")
    occurrences = source.count(mutation.old)
    if occurrences != 1:
        raise MutationError(
            f"{mutation.name}: the mutated text occurs {occurrences} times in {mutation.path}"
        )
    target.write_text(source.replace(mutation.old, mutation.new), encoding="utf-8")
    return destination


def build(directory):
    binary = directory / ("readmit.exe" if os.name == "nt" else "readmit")
    # Built as `make verify` builds the binary it checks. -trimpath also keeps
    # the throwaway copy's directory out of the build cache key, so unchanged
    # packages are reused instead of compiled and cached again per mutation.
    completed = subprocess.run(
        ["go", "build", "-trimpath", "-o", str(binary), "./cmd/readmit"],
        cwd=directory, env=dict(os.environ, GOTOOLCHAIN="local"),
        capture_output=True, check=False, timeout=900,
    )
    if completed.returncode != 0:
        raise MutationError(f"the mutated tree does not build: {completed.stderr.decode()[:600]}")
    return binary


def detected(mutation, binary):
    completed = subprocess.run(
        [sys.executable, str(VERIFY), "--binary", str(binary), "--only", mutation.check],
        cwd=ROOT, capture_output=True, check=False, timeout=900,
    )
    report = completed.stdout.decode().strip()
    # A nonzero status alone would let an unrelated flake pose as detection.
    return completed.returncode != 0 and f"FAIL {mutation.check}" in report, report


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--only", action="append", choices=[m.name for m in MUTATIONS],
                        help="run one named mutation (repeatable)")
    parser.add_argument("--list", action="store_true", help="print the mutation names and exit")
    arguments = parser.parse_args()
    if arguments.list:
        print("\n".join(f"{m.name} -> {m.check}" for m in MUTATIONS))
        return 0
    selected = [m for m in MUTATIONS if not arguments.only or m.name in arguments.only]
    survived = 0
    with tempfile.TemporaryDirectory(prefix="readmit-mutate-") as directory:
        for mutation in selected:
            started = time.monotonic()
            try:
                binary = build(apply_mutation(mutation, Path(directory) / mutation.name))
                caught, report = detected(mutation, binary)
            except (MutationError, OSError, subprocess.SubprocessError) as failure:
                caught, report = False, f"{type(failure).__name__}: {failure}"
            elapsed = time.monotonic() - started
            if not caught:
                survived += 1
            status = "DETECTED" if caught else "SURVIVED"
            print(f"{status} {mutation.name} via {mutation.check} ({elapsed:.1f}s)", flush=True)
            print(f"         {mutation.rationale}; {report.splitlines()[-1] if report else 'no output'}",
                  flush=True)
    for name, reason in sorted(UNMUTATED_CHECKS.items()):
        print(f"RECORDED {name} is not mutation-covered: {reason}", flush=True)
    return 1 if survived else 0


if __name__ == "__main__":
    sys.exit(main())
