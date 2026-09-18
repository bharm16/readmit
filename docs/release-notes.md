Unsigned preview of local HL7 inspection and case evidence capture.

- Byte-preserving raw and MLLP syntax inspection with explicit format reporting.
- v2.5.1 field labels, positional fallback, distinct empty/null/omitted states.
- Values hidden by default; `--show-values` explicitly displays escaped bytes.
- `--roundtrip NEW_FILE` writes an exact copy without overwriting evidence.
- `capture FILE... --output NEW_DIRECTORY` preserves occurrences, malformed
  bytes, source provenance, distinct times, and explicit ACK correlation gaps.
- `timeline BUNDLE` verifies and reopens `readmit-case/v1` evidence directories.
- Copy-stable content identity and deterministic generated provenance support.
- `listen` demonstrates fixed and defective SIU rescheduling with identical AA
  ACKs, an atomically exported ledger, and recorded case evidence.

- `diagnose` produces evidence-linked JSON/Markdown findings for the named SIU
  fixture profile, with explicit unsupported cases and capture-window limits.

- `synth` creates a reproducible synthetic SIU family with separate regression,
  cancellation, and known-invalid bundles from four explicit generator inputs.
- Five native-tested targets, SHA-256 checksums, and GitHub binary provenance.
- `diff` compares fields with explicit source mappings or declared alignment keys,
  reports ambiguous/inserted/missing occurrences, and lists scoped ignore rules.
- `test` evaluates unchanged declarative assertions against wire ACKs and
  session-bound ledger observations, with distinct pass/failure/error codes
  and a verifiable result directory.
- `replay` previews by default, sends only to explicitly configured test targets,
  preserves selected source values unless a named transformation is requested,
  and records exact transport evidence and uncertain outcomes in a new run.
- `redact` creates separate derived evidence with explicit policies and a
  fail-closed export review. Approved fixture exports regenerate their results
  and diagnosis, verify failure preservation, and exclude private mappings.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

Inspection and capture remain local and byte-preserving. `listen` is a bounded
local MLLP test fixture for the documented SIU profile, not a production receiver
or general HL7 conformance validator. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
