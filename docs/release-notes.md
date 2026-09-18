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
  crash reporting, update check or analytics and names every file it keeps.
  It survives an interruption honestly: the workspace, case, region and run a
  viewer had open and every note they had typed and not stored are retained in a
  versioned `readmit-desktop-session/v1` document outside evidence and restored
  when the window opens, while a run that was in flight is reopened read-only
  and stays interrupted with its delivery uncertain. Recovery never resumes,
  restarts or resends: executing again needs a deliberate action and a new
  output folder.

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
  endpoint nonproduction is not proof the address is safe to send to.

- A recorded classification can only refuse. `production` refuses a replay
  during preparation, before a plan exists, so `replay` and `test` are both held
  to it. What a send may reach is decided separately, against the addresses the
  configuration resolves to at the point of sending: `replay --policy FILE
  --decision NEW_FILE` reads a `readmit-send-policy/v1` document of approved
  destinations and denies a class nobody recorded, a name resolving to several
  addresses, a name that does not resolve and an address no approved destination
  contains. Without a policy, a literal loopback address is the only destination
  that remains. The `readmit-send-decision/v1` document recording what was
  allowed or denied and why is retained before anything is opened; a decision
  can stop a send and cannot retract bytes already sent. `target check --policy`
  reports the same decision from the same rule and sends nothing. `listen` and
  `collect` refuse a nonloopback bind unless `--approved-bind` is passed.

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

- `observe` owns the source-neutral contracts a regression assertion about an
  external system rests on. A `readmit-observation-window/v1` document declares
  the source identity and scope in view, the watermark the window opens at, how
  state that existed before it opened is handled, and the completion rule:
  the observed state must hold still across a declared number of samples
  spanning a declared quiet period, inside a declared deadline, so polling never
  stops at the first convenient answer. A
  `readmit-observation-completion/v1` record retains what a collector reported
  and the verdict that follows, names the boundary, window and samples it
  evaluated, and is re-decided against its declared window rather than trusted.
  An observed empty state is evidence; a collector that never ran, stale data, a
  truncated capture, a lost connection, an ambiguous source status and an
  unsupported source kind are each execution errors, and none of them may be
  read as proof that something is absent. State already present when the window
  opened is never evidence that the run produced it. `readmit-observation/v1`,
  `readmit-result/v1`, `readmit-test/v1`-`v2` and `readmit-job/v1` are unchanged.

- `observe collect` is the first source-specific collector, filling in the slots
  those contracts already declare rather than adding a second set. A
  `readmit-observation-source/v1` document declares a bounded JSON, CSV, XML or
  text export on disk, or a bounded read of an approved HTTPS API, with the
  envelope its output is carried in and the locator of the record key. Exports
  are divided by the readers a mapping recipe already uses, so nothing is
  normalized and an HL7 payload inside a record keeps its bytes. Every
  observation states how old the material it read is, and a cached or aged
  answer is stale evidence rather than current evidence. Destinations go through
  the same send policy a replay is held to and the connection uses the address
  that policy checked; TLS 1.2 is the floor with verification always on and an
  explicit customer authority supported. A credential is a reference readmit
  never stores, writes or renders, read through the one mechanism it has for
  reading a value out of a store it does not own. Retries are bounded, recorded
  and applied only to a read that produced no answer at all. The original
  material each read observed is retained unchanged beside the record, and a
  disabled collector, a stale read, a truncated export, a lost connection, an
  unauthorized read and an ambiguous response each produce a named execution
  error rather than a passing absence assertion.

- `corpus generate` writes a reproducible performance corpus from four declared
  inputs and records them, with the corpus length and digest, in a
  `readmit-corpus/v1` manifest written after it. `corpus scan` reads one back
  under the same `readmit-import-plan/v1` an import declares, through a 64 KiB
  window in bounded parsing batches, holding one record and one batch whatever
  the stream's length; it reports the peak it actually held beside the bound it
  is held to, renders one bounded window of records, acknowledges an interrupt
  within one record without leaving an artifact behind, and names the case
  bundle bounds a stream is already past instead of widening them. A
  `readmit-benchmark/v1` report publishes the declared corpus, the declared
  bounds, what the run measured and the machine it measured on, with the
  performance envelope proposed for the product recorded explicitly as
  engineering targets rather than as measurements. The desktop message grid now
  virtualizes its rows, so what it draws is decided by the viewport rather than
  by the size of the window or the case. Every existing case, index, project,
  run, result and report contract is unchanged.

- `target reset` returns one named nonproduction environment to its declared
  starting state through reviewed reset actions, and never through code a
  document supplied. A `readmit-reset-plan/v1` document an operator selects
  explicitly declares actions from a closed set of typed Go operators, each
  carrying the one authority it requires: a step a person performs and confirms
  by name, a read of exactly the one receiver observation file named inside the
  plan's own directory, and one connection that sends no HL7 payload. The
  contract holds no command, script, interpreter or argument; a test spec cannot
  name a plan or an action; and reset prose stays operator-readable text that
  nothing executes. A reset refuses every environment nobody recorded as
  nonproduction, holds its one connection to the same approved-destination
  decision a send is held to, and retains a `readmit-reset-outcome/v1` document.
  A reset that failed, or that could not be confirmed, exits 2 as an execution
  error rather than an assertion failure or a pass. `readmit-target/v1`-`/v3`,
  `readmit-test/v1`, `readmit-observation/v1`, `readmit-send-policy/v1` and
  `readmit-send-decision/v1` are unchanged.

- `collect` serves several peers at once (`--max-connections`, up to 64), each
  becoming its own case source and its own recorded session, with receipts
  sealed in evidence order. A peer beyond the limit waits for a free slot rather
  than being accepted and dropped. `--max-sessions` and `--max-capture-bytes`
  declare a connection budget and a capture quota; reaching either, or
  `--max-messages`, stops the capture in a controlled way, and nothing is read
  once the quota cannot hold it. The listen socket accepts TLS and mutual TLS
  (`--tls-certificate`, `--tls-key-reference`, `--secrets`, `--client-ca`);
  declaring a client authority requires and verifies a client certificate it
  issued, the private key stays a credential reference readmit never stores, and
  one implementation now applies the TLS 1.2 floor, the always-on verification
  and the explicit-authority rule for every path that negotiates TLS.
  `--journal` retains the new `readmit-capture-journal/v1` record — every
  inbound frame's bytes synced before it is answered, and each acknowledgement's
  intent synced before it is written — and `collect status JOURNAL` recovers an
  interrupted capture read-only. Recovery separates acknowledgements that were
  sent, that were attempted and did not complete, and whose outcome was never
  recorded at all; it never resends, never resumes, and never reports an
  incomplete capture as a finished one. The capture's own exit status is
  unchanged by the journal. A
  `readmit-receiver-policy/v3` fault plan still requires one connection at a
  time, and the separate application acknowledgement endpoint remains plain
  transport. `readmit-collection/v1`-`/v3`, `readmit-receiver-policy/v1`-`/v3`,
  `readmit-case/v4` and `readmit-job/v1` are unchanged.

- `source collect` brings evidence in from an approved customer-controlled
  source: a `readmit-source/v1` document declares an export directory this
  machine can open, or a remote export reached by running the customer's own
  read-only transfer program, and each entry is streamed under the same
  `readmit-import-plan/v1` an import declares, so a collection holds one record
  at a time and stages original bytes that `import` then reads. The declared
  quota is applied to the source's own listing before anything is read and
  refuses rather than truncates; identical bytes under a second name are
  recorded as a duplicate of the first, by an identity over names and contents
  only; every attempt starts an entry again from nothing and only a transport
  failure is retried, so a retry never turns an uncertain read into a confident
  one. `source diagnose` reports what access was actually available by
  attempting it and collects nothing. A source that could not be listed, an
  entry no attempt read whole, a quota refusal, a cancellation, a destination no
  explicitly selected policy approved, and an application interface — which this
  release declares and does not collect from, with the read-only contract one
  would have to satisfy documented instead — are each execution errors rather
  than evidence that nothing was there. A remote source's credential is a
  `source-endpoint` reference registered in `readmit-secrets/v1`, presented to
  the transfer program on standard input and never as an argument; readmit
  implements no SSH client and cannot establish what answered. Every existing
  case, index, project, run, result, report and observation contract is
  unchanged.

- The shared profile-pack contract, `readmit-profile-pack/v1`, is defined and
  read by `internal/profilepack` with independently authored positive and
  negative fixtures. A pack declares its identity and version, its source,
  extraction, license and rights-review provenance, and its coverage per HL7
  version (2.3.1 through 2.8.2) and message family (ADT, SIU, ORM, ORU) with
  separate parse, labels, structural and workflow support. A combination the
  pack does not declare is unknown, untested and unsupported do not pass, a
  field name is answered only under a combination whose labels are supported,
  and no v1 pack may claim structural or workflow support. No pack is bundled,
  no command reads one, the bundled `readmit-field-labels/v1` dictionary is
  unchanged, and correlation, the profile and reproducer editors, typed
  assertions and the seven-version library remain separate deliveries.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

Inspection and capture remain local and byte-preserving. `listen` is a bounded
local MLLP test fixture for the documented SIU profile, not a production receiver
or general HL7 conformance validator. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
