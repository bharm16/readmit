# Implementation stack

The choices below were agreed on 2026-09-17 from a stack review. The hard-to-reverse decisions are recorded as ADRs and are not repeated here:

- [ADR-0001](adr/0001-go-single-binary-release-matrix.md): Go, single static binary, the five release targets, toolchain and platform policy.
- [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md): evidence bundles are versioned directories with raw payload files; no database.
- [ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md): specs, profiles, observations, and results are strict JSON evaluated by typed Go operators.
- [ADR-0005](adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md): the desktop application is a separate Wails module over a typed Go facade, never a wrapper around the executable.
- [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md): credentials stay in an OS or customer-managed store; readmit registers references to them and never stores, writes or renders a value. Amended 2026-09-18: the same rule covers the key material behind storage protection.
- [ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md): organization entitlements are signed, versioned documents verified locally against an explicitly selected trust store.
- [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md): the case index is a derived, disposable readmit-owned file rebuilt from canonical evidence, not a database.
- [ADR-0009](adr/0009-profile-packs-are-offline-metadata-with-explicit-support.md): profile packs normalize pinned metadata offline and declare parsing, labels, structure and workflow support separately.
- [ADR-0010](adr/0010-vendor-billing-issues-offline-entitlements-without-evidence.md): Paddle billing stays in a separate vendor service that issues Readmit's offline entitlements without receiving customer evidence.

The [September 18 product decisions](product-decisions.md) also settle the
remaining connector, database, packaging, trial and commercial directions.
That page distinguishes selected future work from implemented capabilities;
the [release checklist](release-acceptance.md) retains their acceptance gates.

Everything else on this page is an ordinary choice. Change it when there is a reason. No ADR is needed unless the change is hard to reverse.

## Module and layout

Two Go modules. `github.com/bharm16/readmit` holds the engine and produces the released `readmit` executable. `github.com/bharm16/readmit/desktop` holds the desktop shell and requires the first through a `replace` directive, so the webview dependency graph never enters the released module. Ordinary internal packages with explicit constructor parameters. No dependency-injection container, plugin loader, or service boundary: commands and the desktop facade call the same small domain packages directly.

## CLI

- [Cobra](https://github.com/spf13/cobra) for the command tree: `inspect`, `capture`, `index`, `corpus`, `project`, `backup`, `upgrade`, `license`, `secret`, `protect`, `target`, `source`, `listen`, `replay`, `test`, `observe`, `explain`, `diff`, `diagnose`, `correlate`, `synth`, `scenario`, `redact`, `report`. It is the only direct third-party dependency of the released executable. No Viper, no interactive TUI.
- Command data goes to stdout, diagnostics to stderr. Machine-readable modes never interleave progress output with JSON.
- Target configuration is read from explicitly selected files, not hidden global config or environment-variable precedence.
- Credentials are referenced, never held. A `readmit-secrets/v1` document registers the store kind an operator declared, the single purpose and endpoint address a credential may be bound to, the absolute path of the program that reads it back, and the recorded rotation. The purposes are `mllp-endpoint` and `source-endpoint`; a reference registered for one is refused wherever the other is needed. No command accepts a credential value, and a resolved value masks itself under every formatting verb and refuses to be serialized. See [credential references](secret.md).
- Encryption keys are referenced the same way. A `readmit-protection/v1` control registers the declared at-rest storage control, the program that reads the key back, the recorded rotation, the lifecycle state and the retention period. See [evidence protection](protect.md).
- Terminal and Markdown rendering use `fmt`, `text/tabwriter`, and `text/template`.

## Desktop application

- [Wails 2](https://wails.io) renders a React and TypeScript interface in the platform webview. It is the only direct third-party dependency of the `desktop` module. Vite builds the interface, which is embedded in the executable; nothing is fetched at run time.
- `internal/desktop` is the typed Go facade. Desktop operations return typed results with one explicit state each. The interface never parses command output and never reimplements HL7 or case bundle semantics. See [the desktop contract](desktop.md).
- The desktop build needs cgo and a platform webview and has its own workflow. It never applies `CGO_ENABLED=0`, never changes the command-line build, and is not in the release archives.
- The window's one write of evidence into a workspace is a reproducer: the occurrences a
  person selected, the setup dependencies their declared relations retain, and
  the field edits applied to them, written as a new derived `readmit-case/v3`
  bundle under the `readmit-reproducer/v1` derivation with a
  `readmit-reproducer/v1` transformation manifest beside it. The plan is a
  `readmit-reproducer-plan/v1` list of typed operators `internal/reproducer`
  interprets; it is held in the window while it is edited and stored nowhere
  else. Original evidence is immutable, so this is a new artifact beside it and
  never an in-place rewrite. See [reproducers](reproducer.md).
- The window's one write of a document into a workspace is a regression test.
  `internal/testauthor` answers a `readmit-test-draft/v1` draft one typed stage
  at a time over a verified case and generates the `readmit-test/v1` spec the
  command line runs, handing the bytes back only after `internal/testrunner`'s
  own reader has accepted them. The boundary fixes the initial state and the
  evidence fixes the send order, targets are read through the same
  `internal/replay` reader a spec resolves one with, and a production
  classification is refused where it is chosen. The draft is held in the window
  while it is answered and stored nowhere else. `readmit-test/v1` gains no
  member. See [authoring a regression test](test-authoring.md).
- Local shell state is three bounded, versioned documents: the list of recently opened folders, the filters a viewer saved with the one selected now (`readmit-filters/v1`), and the working session that viewer has not stored (`readmit-desktop-session/v1`) — where they were, and the notes they had typed and not stored. A saved filter holds what a person typed to filter by and a retained draft holds a note they were writing, which is the same patient data the evidence beside it holds, so all three are owner-readable, named in the window's privacy status, and never written into evidence. Restoring a session reads; it never resumes or resends network work. No telemetry, crash reporting, update checks, or evidence in browser storage.

## HL7 core

- A readmit-owned, byte-preserving parser. Decoded values are a view over the original bytes and byte spans, never a replacement for them. Display normalization never becomes serialization normalization.
- Semantic support starts with one named profile: HL7 v2.5.1 SIU fixture profile, version 1. That is a readmit-supported profile, not a claim of v2.5.1 conformance.
- Existing Go HL7 libraries (for example `kardianos/hl7`) are not used as the message model. Their lossless and malformed-input behaviour has not been verified against readmit's requirements.
- Dictionary provenance and redistribution rights must be confirmed before bundling externally sourced definitions.
- Selected expansion: pinned nHapi metadata for 2.3.1 through 2.7.1 and HL7apy
  1.3.5 metadata for 2.8.2, normalized at build time with no added customer
  runtime. Exact versions, source commits and separate support levels are in
  [D1](product-decisions.md#d1--profile-metadata-and-supported-meaning); #45 owns
  library delivery after the separate shared pack contract.
- That contract is `readmit-profile-pack/v1`, read by `internal/profilepack`: pack
  identity and version, source/extraction/license/rights-review provenance,
  coverage per HL7 version and ADT/SIU/ORM/ORU family with distinct parse,
  labels, structural and workflow levels, and an outcome API in which an
  undeclared combination is unknown and only a declared `supported` passes. No
  v1 pack may claim structural or workflow support, because the contract carries
  no such content. No pack is bundled; the bundled v2.5.1 dictionary is
  unchanged. See [profile packs](profile-packs.md).
- The set of packs one release carries is a library, read by
  `internal/profilelibrary`. It is not a new document: a library is one
  directory of pack documents, and opening it refuses two packs that declare
  the same HL7 version and family, two packs sharing an id, a member larger
  than a pack may be, and anything in the directory that is not a regular pack
  document, including a symbolic link. One pack answers a combination or nobody does;
  there is no precedence, no nearest version and no merge. The published matrix
  states all 28 combinations of the closed sets, covered or not, and a library
  is bundleable only when every pack it holds records an approved rights
  review. No library is bundled and no command reads one. See
  [the profile library](profile-library.md).
- A site's own interface contract is the separate `readmit-local-profile/v1`
  document read by `internal/localprofile`: one pinned pack and combination,
  site-defined Z-segments and constrained standard fields, and per field a
  usage code, a typed conditional requirement over one named position, a
  cardinality, a data type, a local code table, an assigning authority and a
  date rule. A typed editor is the only way it changes, and resolving it
  against its pinned pack marks every rule `profile`, `overridden`, `local` or
  `undeclared`. Because a v1 pack carries labels and nothing else, every
  constraint resolves local and the resolution says so. No message is evaluated
  against a profile and no profile is bundled. See
  [local profiles](local-profiles.md).
- Changing a local profile is versioned by `internal/profileversion`.
  `readmit-profile-version/v1` seals one profile at one version over the length
  and SHA-256 of its canonical document, so reformatting changes nothing and a
  changed rule changes the version; one version standing for two documents is
  refused rather than compared. A comparison names every part that differs —
  base, segment, field, terminology, authority, date — as added, removed or
  changed. `readmit-profile-references/v1` is the separate index of what saved
  tests pin, because `readmit-test/v1` gains no member; a pin is an exact id and
  version together with the checksum of the document that version stood for. An
  assessment lists the saved tests and cases written against the version that
  changed, states no verdict, and moves no pin. An upgrade names one test, the
  version it is on and the version it moves to, and refuses a profile rewritten
  under a version a saved test already pins. See
  [profile versions](profile-versions.md).
- An interface workflow designed as a sequence is one `readmit-scenario/v1`
  document read by `internal/scenario`: the fixture lifecycle profile it binds
  to, the identities it keeps linked across its steps with the state each one
  begins in, each step's offset from a declared base time, and the outcome its
  author declared for that step. Two profiles are implemented,
  `readmit-adt-lifecycle-v1` and `readmit-siu-lifecycle-v1`; each declares the
  subject kinds and trigger events a scenario bound to it may use, and nothing
  is borrowed across them. These are readmit fixture lifecycle profiles, not
  HL7 conformance and not the `readmit-siu-v1` message profile `synth`
  generates bytes from; a profile pack still declares workflow support
  separately from parsing and labels, and no `v1` pack may declare it
  supported. `scenario preview` walks the sequence through the profile's typed
  transition operators and refuses the whole document when a step's declared
  outcome and the lifecycle disagree in either direction, so a negative case
  nobody confirmed is never read as one that was. A refused step changes no
  state, which is how a cancellation or merge case sits inside one workflow.
  Nothing generates HL7 bytes, reads evidence or reaches a network, and
  `readmit-test/v1` gains no member. See
  [designing a workflow as a sequence](scenario-design.md).

## Networking

- Standard library `net`, `bufio`, `io`, `context`, and `crypto/tls`, with a readmit-owned MLLP framing implementation.
- TLS 1.2 minimum, TLS 1.3 permitted, certificate verification always on, explicit customer CA configuration supported, and an explicit TLS server name and client certificate where an environment needs them. A client certificate's private key is a credential reference, never a file readmit keeps. `internal/transportsecurity` is the single implementation of that rule for every path that negotiates TLS, inbound or outbound, so a diagnosis, a send and a capture cannot honour three versions of the same settings. A `collect` listener is the inbound side of it: its certificate is a configured file, its private key is a credential reference, and declaring the authority that issues client certificates is what mutual TLS means there — a client certificate it issued is required and verified, never merely requested.
- A named environment is one `readmit-target/v3` configuration an operator records, validates and diagnoses with `readmit target`. A connectivity diagnostic proves reachability and TLS and never sends an HL7 payload. The classification it records is displayed everywhere the target is shown and is never treated as permission. See [named test environments](target.md).
- A send is decided against a `readmit-send-policy/v1` document the operator selects explicitly, and the decision is retained as a `readmit-send-decision/v1` document. `internal/sendpolicy` owns the one rule: a production class refuses every replay, a name is resolved at the point of the send rather than when a configuration is recorded, and an unrecorded class, an unresolvable name, a name resolving to several addresses and an address outside every approved destination are each denied. A preview and a connectivity diagnosis report that same decision without requesting a send, so a check cannot predict an answer the send path would not give. The same package refuses a nonloopback bind for `listen` and `collect` unless the operator passes `--approved-bind`. See [safe replay](replay.md).
- A fixture reset is `readmit target reset` over a `readmit-reset-plan/v1` document the operator selects explicitly. `internal/fixturereset` owns a closed, reviewed set of typed Go reset operators, each declaring the one authority it requires; the contract carries no command, script, interpreter or argument, and a test spec names no reset action. A reset runs only against a recorded nonproduction environment, asks the same send decision for the one connection it may open, and retains a `readmit-reset-outcome/v1` document whose states are the execution vocabulary `internal/durablerun` owns. See [named test environments](target.md).
- `net/http` from the standard library is the client an external observation reads an approved API with. It is a client only: readmit serves nothing over HTTP. It negotiates TLS through the one rule `internal/transportsecurity` owns, and adds `GET` only, no redirect followed, no proxy taken from the environment, and a connection to the exact address the send policy checked.
- Foreground execution with contexts for cancellation. No background service, no distributed jobs, and no HTTP API for local commands to read the receiver's ledger. The receiver exports observation files.
- `readmit run queue` is a local scheduler inside one foreground command, not a daemon and not a job store: `internal/runqueue` reads a `readmit-run-queue/v1` document the operator selects explicitly, executes its jobs through the same `internal/durablerun` path a single `run start` takes, and reports a `readmit-run-queue-report/v1` document when it stops. Every scheduling decision is made on one goroutine and each run reports back over one channel. A job declares `shared` or `isolated` state; only an explicit isolated declaration lets two jobs reach one environment at once, and a `shared` job holds the named environment and endpoint its target records for the whole run. `after` makes one job a dependency of another and a job it comes after that did not pass stops it. Admission is a read of the `lease.json` beside the other runs in the same directory, never a filesystem lock: one runs directory is one queue's, and nothing is kept between queues. See [durable local runs](durable-runs.md).
- `collect` serves a bounded number of connections at once, each its own case source; `listen` stays one connection at a time because its ledger is one serialized appointment state. A peer beyond the declared limit is not accepted until a slot frees, and nothing is read once a declared bound could not retain it: backpressure is applied by not consuming a message, never by dropping one. A frame counts against a declared message limit when a connection is admitted to read it, so peers reading at once cannot together overshoot it. A declared connection, message or byte budget is a controlled stop; a structural limit of the case contract is an error.
- A capture may retain a `readmit-capture-journal/v1` journal beside the case it is collecting: inbound frame bytes synced before the frame is answered, each acknowledgement's intent synced before it is written, and a terminal record last. It is the same durability shape [durable runs](durable-runs.md) use and it shares their state vocabulary. Recovery is a read: it reports uncertain deliveries and never resends, resumes or converts an incomplete capture into a finished one.

## External observations

- Selected database collectors (#75): `database/sql` with `pgx/v5/stdlib`,
  `go-mssqldb` and `go-ora/v2`. These drivers are not yet installed. Their
  version/authentication tests and five-target static builds precede dependency
  pins and support claims. Defaults are 30 seconds, 10,000 rows and 10 MiB per
  query, explicitly adjustable under environment policy. Database grants over
  approved views enforce SELECT-only access; reset credentials are separate.
  See [D3](product-decisions.md#d3--database-observations) for the finite test
  matrix and separately authorized Oracle 19c gate. This permits the scoped
  pure-Go dependencies; today's Cobra-only module is not a permanent ban on
  the selected collectors, and canonical evidence still is not a database.
- What makes an observation trustworthy is source-neutral and lives in
  `internal/observewindow`, not in any collector. A
  `readmit-observation-window/v1` document declares the source identity and
  scope in view, the watermark the window opens at, how pre-existing state is
  handled and the completion rule; a `readmit-observation-completion/v1` record
  retains what a collector reported and the verdict that follows from it. One
  rule has one implementation, so a collector cannot widen it by construction.
  See [trustworthy observation windows](observe.md).
- A window over an eventually consistent source completes when the observed
  state has held still across a declared number of samples spanning a declared
  quiet period, inside a declared deadline. Polling never stops at the first
  convenient answer, and reaching the deadline without that having happened is
  an error rather than a verdict.
- Failed collection never becomes a passing absence assertion. An observed empty
  state is evidence; a collector that never ran, stale data, a truncated
  capture, a lost connection, an ambiguous source status and an unsupported
  source kind are each execution errors that observed nothing. State that
  existed before the window opened is never evidence that the run produced it,
  and a window declared over an unknown prior state can never attribute one.
- A `readmit-observation-source/v2` document says how one source is reached and
  how its output is read, and `readmit-observation-source/v1` is still read
  unchanged. `internal/observesource` is the source-specific collector: a
  bounded JSON, CSV, XML or text export on disk, a bounded read of an approved
  HTTPS API, and a downstream HL7 capture read back from the case a receiver
  sealed. It fills in the slots the window contracts already declare — sample
  status, evidence identity, correlation — rather than adding a parallel set, so
  the database collector still to come reports the same way.
- A downstream capture is observed under one shared field selector naming the
  position its record key sits at, which is the same position vocabulary a
  correlation rule declares and carries the same disclaimer: reading a position
  asserts nothing about its HL7 meaning. Nothing is asked of the system under
  test — no acknowledgement receipt and no ledger export — and the capture is
  opened through the one verifying case reader and never written to.
- An export is divided by `internal/importer`'s own envelope readers under the
  bounds a [mapping recipe](mapping.md) is already held to. There is no second
  parser: the CSV and XML traps that would rewrite an HL7 payload's bytes are
  avoided once, in one place.
- Every observation states how old the material it read is, from an export's
  modification time or a response's `Age` and `Date`. A cached or aged answer is
  stale evidence, and a response stating neither is ambiguous rather than
  current. Retries are bounded, recorded, and applied only to a read that
  produced no answer at all, so a retry can never turn an uncertain read into a
  confident one.
- An observation credential is a reference, never a value, read through the one
  mechanism `secret` already owns. `readmit-secrets/v1` gains no member and
  changes no byte: a credential bound to an observation endpoint is a separate
  contract, exactly as a storage-protection key is, rather than a widened
  `purpose` on an existing version. See [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md).
- `readmit-observation/v1` is unchanged. It remains the fixture receiver's
  ledger snapshot for one source; the window contracts are separate documents
  beside it, not a revision of it.

## JSON

- `encoding/json/v2` and `encoding/json/jsontext`, generally available since Go 1.27. `RejectUnknownMembers(true)` for typed configuration; `Deterministic(true)` where reproducible output is required.
- Embedded profiles via `go:embed`.
- Expectations are `readmit-assertion-set/v1`, read by `internal/assertion`:
  sixteen typed field, temporal, collection and relationship operators over the
  shared `internal/hl7` selector, with a finite equality condition and three
  per-record quantifiers instead of an expression language. One table binds an
  operator to the subject it reads and the expectation it takes, so an
  incompatible pairing is refused when the document is read. Regular
  expressions are bounded and compiled by the reader; decimals are compared as
  exact rationals through `math/big` rather than as floats; a timestamp with no
  offset names no instant. Undecided is a third outcome, and a collection
  question asked of an observation that did not complete is an execution error.
  `readmit-test/v1` is unchanged. `readmit explain` reads a set and re-decides
  it against a run bundle through `internal/runexplain`, which writes no
  artifact and defines no result document. See
  [typed assertions](assertions.md) and [explaining a run](explain.md).

## Randomness

- Synthetic generation uses the PCG generator from `math/rand/v2` with an explicit seed, base time, generator version, and profile version. It is kept separate from any security-sensitive randomness.

## Entitlements

- `crypto/ed25519` from the standard library signs and verifies entitlement
  documents. It needs no dependency and no parameter choice, so the released
  executable keeps building for its five `CGO_ENABLED=0` targets. It is kept
  separate from synthetic generation's randomness, which is not security
  sensitive.
- `readmit-entitlement/v1`, `readmit-entitlement-trust/v1` and
  `readmit-entitlement-store/v1` are ordinary strict-JSON contracts read the way
  every other artifact is read.
- `readmit-entitlement/v2` names authors and runner authorities instead of
  counting devices: at most two assigned devices per named author, and runner
  capacity in execution instances active at once. `readmit-entitlement-store/v2`
  records the author and device an installation activated as, and
  `readmit-runner-admission/v1` is the customer-controlled authority's local
  admission record. v1 is unchanged and never doubled to stand in for v2. The
  D6 clock guard is #116's separate versioned state, not a member of any of
  these. See [named authors and active runners](license-v2.md).
- Verification is a pure function of the document bytes and a trust store the
  operator selected with `--trust`. No network call, no activation service, no
  phone-home and no update check. No trust store is embedded: the vendor's
  signing identity is decided outside the engine, and no command signs an
  entitlement.
- Prices, plan names, trial length, grace duration, seat and runner counts and
  capability names are members of the document, never constants in engine code.
- No read, verification or export path consults an entitlement. See
  [offline organization entitlements](license.md).
- `readmit-billing-event/v1` and `readmit-billing-account/v1` are the vendor's
  own contracts: a signed payment event and the account ledger it moves, read by
  `internal/billing` with the same strict decoder, the same trust store contract
  and the same Ed25519 algorithm. They are the issuing side only. No payments
  dependency, merchant account, HTTP client, webhook endpoint or provider SDK is
  introduced, and no command reads either one. See
  [purchasing through a separate portal](billing.md).

## Case index

- A search index over one case bundle is a single versioned strict-JSON
  `readmit-index/v1` file written and read by `internal/index`, with no storage
  engine underneath it. It is derived and disposable: a pure function of the
  canonical case directory and the operator's retention declarations, deletable
  at any instant, and reproduced exactly by building it again. See
  [searching a case](index.md).
- Every read of an index opens the case it names through the shared bundle
  reader and refuses the pair the moment they disagree, so a stale index cannot
  serve answers about evidence that is no longer there.
- What is retained, in what form, and until when are three declarations with no
  defaults. A retained decoded field is patient data and is treated as such; a
  digest of a short value is not de-identification.
- Bounded at 16 declared fields, 128 retained bytes per value and 16 MiB per
  document. Past the field or document bound the build is refused; a longer
  value keeps its first 128 bytes and is marked `truncated`.
- The desktop message grid is a filtered, windowed view over one such index and
  is not a second search path: every question it asks about a value or a decoded
  state goes through `index.Document.Search`, and every window re-verifies the
  case and re-checks the index against it. See [the desktop shell](desktop.md).

## Performance corpus and streaming scans

- A declared performance corpus is generated, never committed. `internal/corpus`
  writes one from the same four inputs `synth` declares — a seed, a base time, a
  generator version and a profile version — and records them in a versioned
  strict-JSON `readmit-corpus/v1` manifest beside it with the length and digest
  of what it wrote. The same declarations reproduce the same bytes.
- Reading one back is `importer.Scan`: one 64 KiB read window, at most one
  16 MiB record, and one parsing batch of at most 256 records or 8 MiB, decoded
  and released before the next batch. What a scan holds is a property of those
  bounds, not of the stream, and it is reported as a measured peak beside the
  bound it is held to. A stream past 8 GiB, and a record that reaches no
  declared boundary within 16 MiB, are refused rather than buffered.
- A scan runs under the same `readmit-import-plan/v1` an import runs under and
  makes the same four refusals, so it reports what an import of the same bytes
  would find. It writes no evidence, so it never widens a case bundle bound; it
  names the ones a stream is already past.
- A run publishes a `readmit-benchmark/v1` document: the declared corpus, the
  declared bounds, what it measured, the machine it measured on, and the
  performance envelope proposed in #25 recorded explicitly as engineering
  targets rather than measurements. Nothing compares the two or reports a
  verdict. See [the performance corpus](corpus.md).

## Evidence source collection

- A `readmit-source/v1` document declares one approved customer-controlled
  source: a `directory` this machine can already open, a `transfer` reached by
  running the operator's own read-only transfer program, or an `api`, which is
  declarable and not collected. Every kind declares a scope, a quota with no
  defaults and a retry declaration; the members of the other kinds are refused
  rather than ignored. See [collecting from an approved source](source.md).
- `internal/evidencesource` writes no second ingestion path. Each entry is
  streamed through `importer.Scan` under the same `readmit-import-plan/v1` an
  import declares, so a collection holds one record and one parsing batch
  whatever the entry's length, refuses what an import of the same bytes refuses,
  and stages original bytes that `import` then reads as an ordinary folder.
- A collection that could not run never becomes evidence that nothing was there.
  Statuses are `internal/observewindow`'s source-neutral vocabulary rather than a
  second one: a source that could not be listed, an entry no attempt read whole,
  a quota refusal, a cancellation and an unsupported kind are each execution
  errors. Only `complete` describes the source.
- Retries are bounded, recorded and safe for reads. Every attempt starts an entry
  again from nothing, only a transport failure is retried, and a refusal the
  bytes themselves caused is never retried. Duplicate detection is the ADR-0002
  identity over names and contents only: no timestamp and no absolute path.
- A `transfer` or `api` source is a destination, decided by `internal/sendpolicy`
  before it is reached, and its credential is a `source-endpoint` reference
  presented to the transfer program on standard input and nowhere else.
- **readmit implements no SSH or SFTP client.** Pure-Go SSH and SFTP libraries
  exist and cross-compile for the five release targets, but adding them changes
  the release story of [ADR-0001](adr/0001-go-single-binary-release-matrix.md) —
  Cobra is the only direct third-party dependency of the released executable — so
  a remote export is collected by running the customer's own approved client.
  readmit runs the program it was given and cannot establish what answered.

## Project backup and restore

- A backup of a project directory is a plain directory holding the project's
  files under one subdirectory, a versioned strict-JSON `readmit-backup/v1`
  manifest naming each of them with its length and digest, and a completion
  marker written last, so a backup interrupted at any point is refused rather
  than restored. See [backing up a workspace](backup.md).
- What a backup records about registered evidence is what the shared bundle
  reader reported: `verified`, `changed`, `unreadable` or `missing`. Evidence is
  never reconstructed, substituted or silently omitted, and a backup or restore
  that is not whole exits non-zero.
- A derived index is recorded as the declarations it was built under and never
  copied, so a restore rebuilds it from the restored canonical case and the
  values one retained are never held in a second place.
- Bounded at 65,536 files, 64 MiB for one file, 1 GiB for one backup and 512
  recorded indexes. Past a bound the backup is refused, never truncated.

## Evidence protection

- `crypto/aes` with `crypto/cipher`'s GCM, `crypto/hkdf` and `crypto/rand` from
  the standard library encrypt transfer packages. No dependency and no parameter
  negotiation, so the released executable keeps building for its five
  `CGO_ENABLED=0` targets. An unread cipher or derivation is refused; there is
  no algorithm agility to talk down.
- `readmit-protection/v1`, `readmit-transfer/v1` and
  `readmit-transfer-index/v1` are ordinary strict-JSON contracts read the way
  every other artifact is read.
- The key is read by running the operator's declared program through the one
  mechanism `secret` already owns, and exists only inside the command that read
  it. readmit stores no key, writes to no store, and has no escrow, recovery key
  or password-derived key.
- The at-rest control on a volume is a declaration recorded as made. readmit
  does not interrogate FileVault, BitLocker, LUKS or a backup system, and never
  reports a declaration as a verified property.
- A package's index is encrypted with its content: names and sizes are sensitive
  data, not harmless metadata. The plaintext descriptor names the control and
  nothing about the evidence.
- Original evidence is immutable, so a package is a new artifact beside it and
  never an in-place rewrite. Removing a package unlinks it and is documented as
  unlinking, never as erasure. See [evidence protection](protect.md).

## Logging

- `log/slog` with an allowlist of fields: run ID, operation, duration, counts, error class, completion state.
- Never raw message content, source filenames, or identifiers by default. Evidence lives in bundles; logs do not duplicate it.
- No telemetry, crash reporting, or automatic update check. `upgrade check`
  is an explicit one an operator runs over a candidate directory somebody
  already staged: it contacts no service and opens no network connection.

## Testing and quality

- `testing`, native fuzzing (framing, parsing, selectors, bundle readers), the race detector, and `os/exec` tests against the built binary.
- Independently authored golden fixtures for parsing and for expected workflow results.
- A separate verification layer that does not share the Go packages' assumptions: a stdlib-only Python HL7 endpoint (`tools/independent.py`), a hand-authored corpus under `testdata/verification`, and mutation tests (`tools/mutate.py`) that require those checks to fail when behavior changes. See [independent verification](independent-verification.md).
- `gofmt`, `go vet`, and `govulncheck`. No large lint configuration to start. No Ginkgo or Gomega.
- Race-detector jobs run with cgo enabled; release builds use `CGO_ENABLED=0`. The setting is per job, never global.

## CI

GitHub Actions, with the declared release matrix mapped to native runners:

| Target | Runner |
| --- | --- |
| linux/amd64 | `ubuntu-24.04` |
| linux/arm64 | `ubuntu-24.04-arm` |
| darwin/amd64 | `macos-15-intel` |
| darwin/arm64 | `macos-15` |
| windows/amd64 | `windows-2025` |

- Third-party actions are pinned by commit SHA.
- Go tests, tooling/independent verification, vulnerability scanning, and three fuzz shards run concurrently. Fuzz targets are discovered from Go's test inventory, including targets added by other worktrees. The stable `quality` check requires every lane to pass.
- Each Go job owns a compiler/platform/dependency-scoped cache that advances with the commit. Desktop cache identity includes both module checksum files. Superseded PR runs are cancelled; main and release-tag runs are independent.
- `make test` keeps the small observation boundary under race detection and runs the exact production-size boundary separately without instrumentation. See [validation](agents/testing.md) for the local loop.
- PR checks: tests, vet, govulncheck, independent endpoint/corpus and mutation checks, and native executable smoke tests. The desktop shell is built and checked in a separate workflow, because it needs cgo and a platform webview that the release jobs deliberately do not. That workflow also builds each native desktop package on the runner it targets and installs, checks and removes it there through the platform's own installer; `desktop` is that workflow's stable aggregate over the shell build and the five package jobs. No runner has a display, so no package's window is opened.
- Release jobs test the exact artifacts being published, not rebuilt equivalents.
- Release credentials and signing never run in untrusted pull-request workflows.

## Release and distribution

- GoReleaser OSS builds the archives: `.tar.gz` for macOS and Linux, `.zip` for Windows, plus SHA-256 checksums, published as GitHub Releases.
- `actions/attest` v4 for build provenance on the binaries. Attest only build outputs, never customer evidence.
- Current CLI releases are standalone archives. The desktop packages are built
  and installation-tested on every push as unsigned development previews and are
  published nowhere. There is no automatic update check.
- Selected desktop delivery (#104): Windows MSI with WebView2 handling;
  Developer ID-signed/notarized/stapled macOS DMG plus signed managed PKG;
  Ubuntu `.deb` with WebKitGTK dependencies. The finite OS/architecture targets
  are in [D5](product-decisions.md#d5--desktop-distribution-and-signing).
  The package formats, prerequisites and target matrix are declared once in
  `desktop/packaging/packages.json` (`readmit-desktop-packaging/v1`); each build
  writes a `readmit-desktop-package/v1` manifest with every package's SHA-256
  and `"signed_for_distribution": false`, which is distinct from the ad-hoc
  signature Apple silicon requires the linker to apply to any executable. See [the desktop packages](desktop.md#native-packages).
  The desktop package and the command-line build of one commit carry the same
  engine stamp, and CI compares what the installed application and the archived
  executable report.
- Package work can start before #88 engine-parity completion and signer
  onboarding. Production release still requires both, native installation
  tests, Apple Developer ID/notarytool and Azure Artifact Signing Public Trust.
  Existing unsigned prereleases remain labelled previews; this decision does
  not retroactively sign them. Signing cannot guarantee endpoint-policy or
  SmartScreen acceptance.

## Version pins

Pin the current patch release and bump through reviewed pull requests, never during a build.

| Component | Pinned at 2026-09-17 |
| --- | --- |
| Go toolchain | go1.27.1 (`tools/toolchain.py` resolves the `toolchain` directive for setup-go; `GOTOOLCHAIN=local` in CI) |
| Cobra | v1.10.2 |
| Wails | v2.16.0 (desktop module only) |
| React and React DOM | 19.3.0 (with `@types/react` and `@types/react-dom` 19.3.0) |
| Vite | 8.3.0 (with `@vitejs/plugin-react` 6.1.1) |
| TypeScript | 5.9.3 |
| Node | 24 in CI; every resolved frontend version is locked in `desktop/frontend/package-lock.json` |
| govulncheck | v1.8.0 |
| GoReleaser OSS | v2.18.2 |
| WiX | 6.0.1 (Windows installer database only; not in the application) |
| actions/attest | v4, by commit SHA |

Commit `go.mod` and `go.sum`. Neither directive alone locks the compiler.
CI passes the resolved `toolchain` pin explicitly to setup-go, checks the active
compiler, and reads the compiler version from every packaged executable before
upload. `GOTOOLCHAIN=local` prevents automatic switching; it does not enforce the
pin by itself. Native smoke tests run without Go or other tools on PATH.

## Absent from the current build

The inventory below describes the implemented engine, not a veto on adopted
work. [D2](product-decisions.md#d2--integration-engine-exports) selects the
Mirth 4.5.2/OIE 4.6.0 file adapters;
[D3](product-decisions.md#d3--database-observations) selects external database
reads; [D6–D8](product-decisions.md#d6--evaluation-and-clock-policy) select trial,
vendor billing and administration policies. A separate vendor service is not
an evidence-engine dependency. None is implemented merely by being selected.

No database, ORM, web application server, container runtime, hosted backend, external rules engine, message broker, Redis, search server, LLM API, payment integration, licence or activation server, or application authentication system. The case index of [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md) is not one of them: it is a derived, disposable file the engine writes and reads the way it writes and reads every other artifact, rebuilt from the canonical case directory and never the only home of anything. Entitlement verification is a local check of a signed file and introduces none of them. Access control is the operating-system account and filesystem permissions, plus the encrypted transfer packages an operator asks for. readmit has no credential or key store of its own: it registers references to credentials and encryption keys kept in an operating system credential store or a customer-managed secret provider, reads one by running the program the operator declared, and never writes to a store, so its own privilege is read access to the values a person registered. Nothing is encrypted implicitly, no key is escrowed or recoverable, and no deletion readmit performs is presented as erasure. The desktop application is a local webview over the same engine, not a hosted web application: it serves nothing over a network and has no accounts.
