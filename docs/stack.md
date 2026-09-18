# Implementation stack

The choices below were agreed on 2026-09-17 from a stack review. The hard-to-reverse decisions are recorded as ADRs and are not repeated here:

- [ADR-0001](adr/0001-go-single-binary-release-matrix.md): Go, single static binary, the five release targets, toolchain and platform policy.
- [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md): evidence bundles are versioned directories with raw payload files; no database.
- [ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md): specs, profiles, observations, and results are strict JSON evaluated by typed Go operators.
- [ADR-0005](adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md): the desktop application is a separate Wails module over a typed Go facade, never a wrapper around the executable.
- [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md): credentials stay in an OS or customer-managed store; readmit registers references to them and never stores, writes or renders a value. Amended 2026-09-18: the same rule covers the key material behind storage protection.
- [ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md): organization entitlements are signed, versioned documents verified locally against an explicitly selected trust store.
- [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md): the case index is a derived, disposable readmit-owned file rebuilt from canonical evidence, not a database.

Everything else on this page is an ordinary choice. Change it when there is a reason. No ADR is needed unless the change is hard to reverse.

## Module and layout

Two Go modules. `github.com/bharm16/readmit` holds the engine and produces the released `readmit` executable. `github.com/bharm16/readmit/desktop` holds the desktop shell and requires the first through a `replace` directive, so the webview dependency graph never enters the released module. Ordinary internal packages with explicit constructor parameters. No dependency-injection container, plugin loader, or service boundary: commands and the desktop facade call the same small domain packages directly.

## CLI

- [Cobra](https://github.com/spf13/cobra) for the command tree: `inspect`, `capture`, `index`, `corpus`, `project`, `backup`, `license`, `secret`, `protect`, `target`, `listen`, `replay`, `test`, `observe`, `diff`, `diagnose`, `synth`, `redact`, `report`. It is the only direct third-party dependency of the released executable. No Viper, no interactive TUI.
- Command data goes to stdout, diagnostics to stderr. Machine-readable modes never interleave progress output with JSON.
- Target configuration is read from explicitly selected files, not hidden global config or environment-variable precedence.
- Credentials are referenced, never held. A `readmit-secrets/v1` document registers the store kind an operator declared, the single purpose and endpoint address a credential may be bound to, the absolute path of the program that reads it back, and the recorded rotation. No command accepts a credential value, and a resolved value masks itself under every formatting verb and refuses to be serialized. See [credential references](secret.md).
- Encryption keys are referenced the same way. A `readmit-protection/v1` control registers the declared at-rest storage control, the program that reads the key back, the recorded rotation, the lifecycle state and the retention period. See [evidence protection](protect.md).
- Terminal and Markdown rendering use `fmt`, `text/tabwriter`, and `text/template`.

## Desktop application

- [Wails 2](https://wails.io) renders a React and TypeScript interface in the platform webview. It is the only direct third-party dependency of the `desktop` module. Vite builds the interface, which is embedded in the executable; nothing is fetched at run time.
- `internal/desktop` is the typed Go facade. Desktop operations return typed results with one explicit state each. The interface never parses command output and never reimplements HL7 or case bundle semantics. See [the desktop contract](desktop.md).
- The desktop build needs cgo and a platform webview and has its own workflow. It never applies `CGO_ENABLED=0`, never changes the command-line build, and is not in the release archives.
- Local shell state is two bounded, versioned documents: the list of recently opened folders, and the filters a viewer saved with the one selected now (`readmit-filters/v1`). A saved filter holds what a person typed to filter by, which for a field value is the same patient data that field holds, so it is owner-readable, named in the window's privacy status, and never written into evidence. No telemetry, crash reporting, update checks, or evidence in browser storage.

## HL7 core

- A readmit-owned, byte-preserving parser. Decoded values are a view over the original bytes and byte spans, never a replacement for them. Display normalization never becomes serialization normalization.
- Semantic support starts with one named profile: HL7 v2.5.1 SIU fixture profile, version 1. That is a readmit-supported profile, not a claim of v2.5.1 conformance.
- Existing Go HL7 libraries (for example `kardianos/hl7`) are not used as the message model. Their lossless and malformed-input behaviour has not been verified against readmit's requirements.
- Dictionary provenance and redistribution rights must be confirmed before bundling externally sourced definitions.

## Networking

- Standard library `net`, `bufio`, `io`, `context`, and `crypto/tls`, with a readmit-owned MLLP framing implementation.
- TLS 1.2 minimum, TLS 1.3 permitted, certificate verification always on, explicit customer CA configuration supported, and an explicit TLS server name and client certificate where an environment needs them. A client certificate's private key is a credential reference, never a file readmit keeps.
- A named environment is one `readmit-target/v3` configuration an operator records, validates and diagnoses with `readmit target`. A connectivity diagnostic proves reachability and TLS and never sends an HL7 payload. The classification it records is displayed everywhere the target is shown and is never treated as permission. See [named test environments](target.md).
- A send is decided against a `readmit-send-policy/v1` document the operator selects explicitly, and the decision is retained as a `readmit-send-decision/v1` document. `internal/sendpolicy` owns the one rule: a production class refuses every replay, a name is resolved at the point of the send rather than when a configuration is recorded, and an unrecorded class, an unresolvable name, a name resolving to several addresses and an address outside every approved destination are each denied. A preview and a connectivity diagnosis report that same decision without requesting a send, so a check cannot predict an answer the send path would not give. The same package refuses a nonloopback bind for `listen` and `collect` unless the operator passes `--approved-bind`. See [safe replay](replay.md).
- Foreground execution with contexts for cancellation. No queue, no distributed jobs, and no HTTP API for local commands to read the receiver's ledger. The receiver exports observation files.

## External observations

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
- `readmit-observation/v1` is unchanged. It remains the fixture receiver's
  ledger snapshot for one source; the window contracts are separate documents
  beside it, not a revision of it.

## JSON

- `encoding/json/v2` and `encoding/json/jsontext`, generally available since Go 1.27. `RejectUnknownMembers(true)` for typed configuration; `Deterministic(true)` where reproducible output is required.
- Embedded profiles via `go:embed`.

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
- Verification is a pure function of the document bytes and a trust store the
  operator selected with `--trust`. No network call, no activation service, no
  phone-home and no update check. No trust store is embedded: the vendor's
  signing identity is decided outside the engine, and no command signs an
  entitlement.
- Prices, plan names, trial length, grace duration, seat and runner counts and
  capability names are members of the document, never constants in engine code.
- No read, verification or export path consults an entitlement. See
  [offline organization entitlements](license.md).

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
  document. Past a bound the build is refused, never truncated.
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
- No telemetry, crash reporting, or update checks.

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
- PR checks: tests, vet, govulncheck, independent endpoint/corpus and mutation checks, and native executable smoke tests. The desktop shell is built and checked in a separate workflow, because it needs cgo and a platform webview that the release jobs deliberately do not.
- Release jobs test the exact artifacts being published, not rebuilt equivalents.
- Release credentials and signing never run in untrusted pull-request workflows.

## Release and distribution

- GoReleaser OSS builds the archives: `.tar.gz` for macOS and Linux, `.zip` for Windows, plus SHA-256 checksums, published as GitHub Releases.
- `actions/attest` v4 for build provenance on the binaries. Attest only build outputs, never customer evidence.
- No installers, package-manager distribution, or auto-update in v1.
- Signing is deferred past v1. Apple Developer ID notarization (GoReleaser's macOS notarization support) and Azure Artifact Signing for Windows need accounts and identity validation. Prereleases are unsigned and documented as such. Signing does not guarantee that SmartScreen or an endpoint policy accepts a new binary without warnings.

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
| actions/attest | v4, by commit SHA |

Commit `go.mod` and `go.sum`. Neither directive alone locks the compiler.
CI passes the resolved `toolchain` pin explicitly to setup-go, checks the active
compiler, and reads the compiler version from every packaged executable before
upload. `GOTOOLCHAIN=local` prevents automatic switching; it does not enforce the
pin by itself. Native smoke tests run without Go or other tools on PATH.

## Deliberately absent

No database, ORM, web application server, container runtime, hosted backend, external rules engine, message broker, Redis, search server, LLM API, payment integration, licence or activation server, or application authentication system. The case index of [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md) is not one of them: it is a derived, disposable file the engine writes and reads the way it writes and reads every other artifact, rebuilt from the canonical case directory and never the only home of anything. Entitlement verification is a local check of a signed file and introduces none of them. Access control is the operating-system account and filesystem permissions, plus the encrypted transfer packages an operator asks for. readmit has no credential or key store of its own: it registers references to credentials and encryption keys kept in an operating system credential store or a customer-managed secret provider, reads one by running the program the operator declared, and never writes to a store, so its own privilege is read access to the values a person registered. Nothing is encrypted implicitly, no key is escrowed or recoverable, and no deletion readmit performs is presented as erasure. The desktop application is a local webview over the same engine, not a hosted web application: it serves nothing over a network and has no accounts.
