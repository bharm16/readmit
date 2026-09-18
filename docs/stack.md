# Implementation stack

The choices below were agreed on 2026-09-17 from a stack review. The hard-to-reverse decisions are recorded as ADRs and are not repeated here:

- [ADR-0001](adr/0001-go-single-binary-release-matrix.md): Go, single static binary, the five release targets, toolchain and platform policy.
- [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md): evidence bundles are versioned directories with raw payload files; no database.
- [ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md): specs, profiles, observations, and results are strict JSON evaluated by typed Go operators.

Everything else on this page is an ordinary choice. Change it when there is a reason. No ADR is needed unless the change is hard to reverse.

## Module and layout

One Go module, `github.com/bharm16/readmit`, producing one `readmit` executable. Ordinary internal packages with explicit constructor parameters. No dependency-injection container, plugin loader, or service boundary: commands call the same small domain packages directly.

## CLI

- [Cobra](https://github.com/spf13/cobra) for the command tree: `inspect`, `capture`, `listen`, `replay`, `test`, `diff`, `diagnose`, `synth`, `redact`, `report`. It is the only direct third-party application dependency. No Viper, no interactive TUI.
- Command data goes to stdout, diagnostics to stderr. Machine-readable modes never interleave progress output with JSON.
- Target configuration is read from explicitly selected files, not hidden global config or environment-variable precedence.
- Terminal and Markdown rendering use `fmt`, `text/tabwriter`, and `text/template`.

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

## Logging

- `log/slog` with an allowlist of fields: run ID, operation, duration, counts, error class, completion state.
- Never raw message content, source filenames, or identifiers by default. Evidence lives in bundles; logs do not duplicate it.
- No telemetry, crash reporting, or update checks.

## Testing and quality

- `testing`, native fuzzing (framing, parsing, selectors, bundle readers), the race detector, and `os/exec` tests against the built binary.
- Independently authored golden fixtures for parsing and for expected workflow results.
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
- PR checks: tests, vet, govulncheck, and native executable smoke tests.
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
| Go toolchain | go1.27.1 (`toolchain` directive in go.mod, `GOTOOLCHAIN=local` in CI, setup-go reads go.mod) |
| Cobra | v1.10.2 |
| govulncheck | v1.8.0 |
| GoReleaser OSS | v2.18.2 |
| actions/attest | v4, by commit SHA |

Commit `go.mod` and `go.sum`. The `go` directive is not an exact compiler lock on its own; the `toolchain` directive together with `GOTOOLCHAIN=local` is.

## Deliberately absent

No database, ORM, web framework, frontend, container runtime, hosted backend, external rules engine, message broker, Redis, LLM API, payment integration, or application authentication system. Access control is the operating-system account, filesystem permissions, and explicitly configured network credentials.
