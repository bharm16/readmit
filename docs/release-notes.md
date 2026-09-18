Unsigned preview of local HL7 incident reproduction and regression workflows.

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

- `collect` receives HL7 of any message type over bounded MLLP under a strict
  JSON acknowledgement policy, labels every retained source explicitly, and
  seals a `readmit-case/v4` collection record that never reports application
  processing from an accept acknowledgement.
- A native desktop shell opens a workspace folder, lists what it declares, and
  verifies a case through the same reader the command line uses. It ships the
  frozen synthetic sample workspace and reopens recent folders. It is a separate
  build and is not included in these archives.

- `collect` handles original and enhanced acknowledgement workflows: stage
  specific correlation with separate commit and application codes, declared
  MSH-15/MSH-16 conditions, application acknowledgements delivered to a
  separately configured endpoint under `--application-ack-timeout`, and named
  errors for acknowledgement modes it does not support. A commit acceptance is
  never recorded as an application result, and an undelivered or timed-out
  stage is retained as unanswered rather than as an application failure.

- `project` creates and manages an interface investigation: a versioned
  `readmit-project/v1` document holding declared interface versions, registered
  cases, and the title, tags, ownership, status and linked incidents of each
  one, kept beside evidence and never inside it. Provenance is read from the
  verified bundle rather than declared, and the case identity the project
  records is the same value the command line, the desktop shell and an exported
  packet name.

- `project` separates immutable evidence from editable working copies: a
  versioned `readmit-revisions/v1` document beside the evidence holds the notes
  and drafts a person maintains and the lineage of every revision. `project
  revise` registers derived evidence with the verified identity of its parent
  and the operation manifest the derived bundle declares; `project note` creates
  or replaces one note. `readmit-project/v1` is unchanged. A transformation is
  registered with its lineage or not at all: evidence that is not one is refused
  as a revision, `project add` refuses evidence that is, and one name and one
  piece of evidence are held by exactly one of the two documents. The desktop
  shell can edit a note without any edit reaching an import, a finalized run or
  any other retained artifact.

- `report` runs the named synthetic regression and seals its reproducer,
  verified failure/pass results, diagnosis, diff, profiles, and rerun procedure
  in one packet. Offline verification and separate rerun workspaces are included.

- Released behavior is now also checked against an independently implemented HL7
  endpoint, a hand-authored corpus that no readmit command produced, and
  mutation tests that require those checks to fail when behavior changes. No
  supported integration-engine export corpus is covered yet.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

Inspection and capture remain local and byte-preserving. `listen` is a bounded
local MLLP test fixture for the documented SIU profile, not a production receiver
or general HL7 conformance validator. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
