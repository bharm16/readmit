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
- `protect` encrypts evidence with a key readmit never holds: a versioned
  `readmit-protection/v1` control registers the program that reads the key back
  from an OS or customer-managed store, the declared at-rest control on the
  volume, the recorded rotation, the lifecycle state and the retention period.
  `protect pack` writes a versioned `readmit-transfer/v1` package with
  AES-256-GCM content and an encrypted `readmit-transfer-index/v1` index, so the
  packed names and sizes are protected with the content and the plaintext
  descriptor names only the control that opens it. The evidence `protect pack`
  reads is unchanged and it writes no plaintext temporary copy of its own, which
  is a statement about that command and about no other; a wrong key, a
  rotated-away key, a truncated package, an altered one and a destination inside
  retained evidence are each refused by name. A declared storage control is
  recorded as declared and never verified, nothing is encrypted implicitly,
  retirement is not revocation, and `protect discard` refuses a package inside
  its declared retention period before stating that unlinking is not erasure on
  a solid-state device, a copy-on-write filesystem, a snapshot, a backup, a
  replica or a recipient's machine.
- `backup` copies a whole project directory into a verified `readmit-backup/v1`
  directory, reads one back whole, and restores it somewhere else with every
  registered identity intact, because a bundle identity covers relative paths
  and contents only. Incomplete evidence is displayed rather than replaced: a
  registered case that is missing, unreadable or no longer the evidence the
  project recorded is recorded and restored exactly that way, and both commands
  exit non-zero. The completion marker is written last, so an interrupted backup
  is refused rather than restored; an altered manifest, an altered or missing
  stored file, an unrecorded one, an absolute or traversing recorded path, and a
  destination inside the project or the backup are each refused by name. A
  derived index is never copied: the backup records the declarations it was
  built under and the restore builds it again from the restored canonical case,
  so a damaged index is a rebuild and no retained value is held twice.

- `import --recipe` maps CSV, JSON, XML and timestamped text envelopes through a
  reusable `readmit-mapping-recipe/v1` document that supplies the payload,
  observed time, source, direction and channel through typed operators alone.
  Previews and receipts record the recipe verbatim with the SHA-256 identity of
  its declarations. Observed times must carry their own UTC offset and
  directions are translated through the recipe's declared table, so nothing is
  inferred from a name, a timestamp or a message. CSV and XML are read from the
  member's own bytes rather than through readers that normalize line endings. A
  record whose declared values do not resolve is retained whole with a named
  reason and no provenance; a member whose structure contradicts the recipe is
  refused.

- `target` records, validates and diagnoses one named nonproduction
  environment. `readmit-target/v3` adds the environment name, its recorded
  classification, an explicit TLS server name and a client certificate whose
  private key stays a credential reference; `readmit-target/v1` and `/v2` are
  read exactly as before and neither is migrated. `target check` opens one
  connection, completes TLS and reports the negotiated session, the verified
  server name and the subject, issuer and expiry of every presented
  certificate, with expired certificates, untrusted authorities, hostname
  mismatches, refused connections, timeouts and rejected client certificates
  each named separately, and a certificate verification refused is reported so
  the failure can be diagnosed. `replay` and `test` verify against the same
  declared server name; this release's transport presents no client
  certificate, so a configuration declaring one is refused for replay rather
  than sent without it. A check never sends an HL7 payload, and it says so: reaching
  an endpoint is transport evidence, not evidence of application processing, and
  a certificate expiry is reported rather than acted on. The recorded
  classification is displayed by every command that shows a target, including
  every replay, and is never treated as permission: a person labelling an
  endpoint nonproduction is not proof the address is safe to send to, and
  blocking on a recorded class is not in this release.

- The desktop shell finds evidence through a filterable message grid over the
  rebuildable case index. It renders one bounded window of occurrences at a time
  and asks for the next, so a large case is never drawn at once, and every
  window re-verifies the case and re-checks the index against it, so a view is
  never served from an index the evidence no longer supports or one whose
  declared retention has ended. Saved filters are a
  versioned `readmit-filters/v1` document of this viewer's own local state,
  narrowing by occurrence type, source, observed time, decoded field state, ACK
  outcome and field values; the selected filter is kept when navigating from one
  case to another and when the window is opened again. Every grid states how
  many records the filter excluded, and separately how many the index could not
  settle and how many the case itself could not decode, so a filtered view never
  reads as though the case held nothing else. A saved filter holds what a person
  typed to filter by and the window's privacy status names it; no message byte,
  field value or original source path crosses the boundary into the interface.
  `readmit-index/v1` and every case, project, run, result and report contract are
  unchanged.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

Inspection and capture remain local and byte-preserving. `listen` is a bounded
local MLLP test fixture for the documented SIU profile, not a production receiver
or general HL7 conformance validator. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
