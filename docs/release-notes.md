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
  The window is navigated from the keyboard alone: five regions in a fixed focus
  order, a command palette, a search over what the open workspace and its project
  declare, keyboard-resizable evidence and inspector panes, light and dark, and
  text from 100% to 200%. Every status carries its own word and its own shape, so
  none is told apart by colour, and the window states that there is no telemetry,
  crash reporting, update check or analytics and names the one file it keeps.

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

- `secret` references credentials without holding them: a versioned
  `readmit-secrets/v1` document registers the store an operator declared, the
  one purpose and endpoint address a credential may be bound to, the absolute
  path of the program that reads it back from that store, and the recorded
  rotation. No command accepts a credential value, nothing renders one, and a
  resolved value refuses to be serialized, so a target configuration, a project,
  a manifest, a report and local browser state carry a reference or nothing.
  `readmit-target/v2` adds that reference; `readmit-target/v1` and
  `readmit-run/v1` are unchanged. `secret scan` resolves the registered
  credentials and checks named files and directories, including the reference
  document itself, for those exact bytes, their JSON escapes and their base64
  encodings, reporting locations and never a value. It is one check on known
  values, not an assessment that a file is safe to share.

- `report` runs the named synthetic regression and seals its reproducer,
  verified failure/pass results, diagnosis, diff, profiles, and rerun procedure
  in one packet. Offline verification and separate rerun workspaces are included.

- Released behavior is now also checked against an independently implemented HL7
  endpoint, a hand-authored corpus that no readmit command produced, and
  mutation tests that require those checks to fail when behavior changes. No
  supported integration-engine export corpus is covered yet.

- `license` verifies a signed organization entitlement offline: a versioned
  `readmit-entitlement/v1` document, an Ed25519 signature checked against an
  explicitly selected trust store, seat and runner scope with signed device
  bindings, import and byte-identical export, renewal by issue sequence, signing
  key rotation and revocation, device transfer by release and reissue, and a
  configurable grace window. No network request, activation service or update
  check is involved, no price, plan or grace duration is chosen by the engine,
  and expiry withdraws granted capabilities only: existing evidence stays
  readable and exportable, and a revocation issued after signing is documented
  as something local verification cannot observe.

- `import` brings multi-file, folder and ZIP evidence into a `readmit-case/v1`
  case under an explicit declaration of framing, batch boundary, terminator,
  encoding, direction and members, stated as flags with no defaults or saved as
  a reusable `readmit-import-plan/v1` document. `--preview` reports every member
  and record it would extract without writing anything, and an import records
  what it did write in a `readmit-import-receipt/v1` receipt beside the case.
  Ambiguous splitting, contradicted framing or encoding, and unsafe archive
  entries are refused by name; malformed records are retained whole and
  quarantined with their reason.

- `index` builds a derived, disposable `readmit-index/v1` file over one case so
  declared fields can be searched without reading every message again. What is
  retained, in what form and until when are three declarations with no defaults;
  `states` retains nothing read out of a message and `digests` retains no value
  bytes, which is not de-identification. A damaged, truncated, expired or stale
  index is refused and rebuilt from the canonical case directory, never repaired
  and never served; an index cannot be written inside evidence, and the case
  stays readable and unchanged throughout.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

Inspection and capture remain local and byte-preserving. `listen` is a bounded
local MLLP test fixture for the documented SIU profile, not a production receiver
or general HL7 conformance validator. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
