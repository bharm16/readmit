---
status: accepted
date: 2026-09-17
---

# Go single binary with a declared five-target release matrix

readmit must ship as one self-contained executable that an interop engineer can drop onto a Windows, Linux, or macOS machine inside a hospital network with no toolchain, no installer, and no network access beyond the MLLP endpoints they configure. We write it in Go with a small dependency footprint and cgo disabled wherever practical, and we release exactly five targets: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64. CI smoke-tests the actual artifact on each target OS, because a successful cross-compile is not evidence that the binary runs where a customer will run it.

## Considered options

- **Rust.** Also produces static single binaries, with stronger guarantees. Rejected for v1 because iteration speed matters more than memory safety or throughput here; the hard problems are HL7 edge cases and tooling ergonomics.
- **TypeScript compiled with Bun.** Viable, and it would have met the executable requirement, since Bun supports standalone cross-compiled executables. Rejected because the embedded runtime makes each artifact tens of megabytes rather than a few, the receiver and replayer are long-lived socket programs where Go's standard library is the more proven fit, and nothing in v1 needs the JavaScript ecosystem. Reconsider if a web UI ever needs to share code with the CLI.
- **Python with a bundler.** Ruled out by the single-executable requirement: bundled Python artifacts are large, fragile across OS versions, and often trip antivirus false positives on customer machines.

## Consequences

- No cgo means pure-Go TLS and no native SQLite or similar. Any future storage or crypto need must have a pure-Go path or be dropped.
- Windows arm64 and 32-bit targets are deliberately not built. Add a target only when a customer needs it, and add it to the CI smoke test at the same time.
- Every later ticket inherits the release pipeline from issue #1. Adding a dependency means checking that it is pure Go and cross-compiles for all five targets.
- Reopening this decision means a rewrite. The triggers that would justify it are a required native dependency with no pure-Go equivalent, or a GUI or web front end that must share code with the CLI.
