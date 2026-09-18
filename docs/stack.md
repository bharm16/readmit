# Implementation stack

The choices below were agreed on 2026-09-17 from a stack review. The hard-to-reverse decisions are recorded as ADRs and are not repeated here:

- [ADR-0001](adr/0001-go-single-binary-release-matrix.md): Go, single static binary, the five release targets, toolchain and platform policy.
- [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md): evidence bundles are versioned directories with raw payload files; no database.
- [ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md): specs, profiles, observations, and results are strict JSON evaluated by typed Go operators.
- [ADR-0005](adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md): the desktop application is a separate Wails module over a typed Go facade, never a wrapper around the executable.
- [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md): credentials stay in an OS or customer-managed store; readmit registers references to them and never stores, writes or renders a value.
- [ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md): organization entitlements are signed, versioned documents verified locally against an explicitly selected trust store.
- [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md): the case index is a derived, disposable readmit-owned file rebuilt from canonical evidence, not a database.

Everything else on this page is an ordinary choice. Change it when there is a reason. No ADR is needed unless the change is hard to reverse.

## Module and layout

Two Go modules. `github.com/bharm16/readmit` holds the engine and produces the released `readmit` executable. `github.com/bharm16/readmit/desktop` holds the desktop shell and requires the first through a `replace` directive, so the webview dependency graph never enters the released module. Ordinary internal packages with explicit constructor parameters. No dependency-injection container, plugin loader, or service boundary: commands and the desktop facade call the same small domain packages directly.

## CLI

- [Cobra](https://github.com/spf13/cobra) for the command tree: `inspect`, `capture`, `index`, `project`, `license`, `secret`, `listen`, `replay`, `test`, `diff`, `diagnose`, `synth`, `redact`, `report`. It is the only direct third-party dependency of the released executable. No Viper, no interactive TUI.
- Command data goes to stdout, diagnostics to stderr. Machine-readable modes never interleave progress output with JSON.
- Target configuration is read from explicitly selected files, not hidden global config or environment-variable precedence.
- Credentials are referenced, never held. A `readmit-secrets/v1` document registers the store kind an operator declared, the single purpose and endpoint address a credential may be bound to, the absolute path of the program that reads it back, and the recorded rotation. No command accepts a credential value, and a resolved value masks itself under every formatting verb and refuses to be serialized. See [credential references](secret.md).
- Terminal and Markdown rendering use `fmt`, `text/tabwriter`, and `text/template`.

## Desktop application

- [Wails 2](https://wails.io) renders a React and TypeScript interface in the platform webview. It is the only direct third-party dependency of the `desktop` module. Vite builds the interface, which is embedded in the executable; nothing is fetched at run time.
- `internal/desktop` is the typed Go facade. Desktop operations return typed results with one explicit state each. The interface never parses command output and never reimplements HL7 or case bundle semantics. See [the desktop contract](desktop.md).
- The desktop build needs cgo and a platform webview and has its own workflow. It never applies `CGO_ENABLED=0`, never changes the command-line build, and is not in the release archives.
- Local shell state is one bounded, versioned list of recently opened folders. No telemetry, crash reporting, update checks, or evidence in browser storage.

## HL7 core

- A readmit-owned, byte-preserving parser. Decoded values are a view over the original bytes and byte spans, never a replacement for them. Display normalization never becomes serialization normalization.
- Semantic support starts with one named profile: HL7 v2.5.1 SIU fixture profile, version 1. That is a readmit-supported profile, not a claim of v2.5.1 conformance.
- Existing Go HL7 libraries (for example `kardianos/hl7`) are not used as the message model. Their lossless and malformed-input behaviour has not been verified against readmit's requirements.
- Dictionary provenance and redistribution rights must be confirmed before bundling externally sourced definitions.

## Networking

- Standard library `net`, `bufio`, `io`, `context`, and `crypto/tls`, with a readmit-owned MLLP framing implementation.
- TLS 1.2 minimum, TLS 1.3 permitted, certificate verification always on, explicit customer CA configuration supported.
- Foreground execution with contexts for cancellation. No queue, no distributed jobs, and no HTTP API for local commands to read the receiver's ledger. The receiver exports observation files.

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

No database, ORM, web application server, container runtime, hosted backend, external rules engine, message broker, Redis, search server, LLM API, payment integration, licence or activation server, or application authentication system. The case index of [ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md) is not one of them: it is a derived, disposable file the engine writes and reads the way it writes and reads every other artifact, rebuilt from the canonical case directory and never the only home of anything. Entitlement verification is a local check of a signed file and introduces none of them. Access control is the operating-system account and filesystem permissions. readmit has no credential store of its own: it registers references to credentials kept in an operating system credential store or a customer-managed secret provider, reads one by running the program the operator declared, and never writes to a store, so its own privilege is read access to the credentials a person registered. The desktop application is a local webview over the same engine, not a hosted web application: it serves nothing over a network and has no accounts.
