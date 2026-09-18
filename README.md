# readmit

Local-first HL7 v2 incident-reproduction and regression-testing CLI for healthcare integration engineers. `inspect` is available; later workflows are tracked in [GitHub issues](https://github.com/bharm16/readmit/issues).

```sh
readmit inspect message.hl7
readmit inspect captured.mllp --format mllp --roundtrip preserved.mllp
readmit inspect message.hl7 --show-values
```

`inspect` shows message counts, detected or declared formats, segment/field names,
byte offsets, and field states. Values are hidden by default. `--show-values` is
an explicit request to print message content, using quoted, escaped byte strings
so terminal controls and non-UTF-8 bytes are never interpreted as terminal text.
Repetitions are shown separately. Escape sequences and component separators are
preserved literally; this release does not decode HL7 values or character sets.

`--roundtrip NEW_FILE` reserializes the parsed evidence, including its framing,
byte for byte. The destination must not exist; existing files, source aliases,
and symlinks are refused. On Unix it is created with mode `0600`; Windows uses
the destination directory's inherited access controls. There is no editing API.
Future edits must create separate derived artifacts, never rewrite the source.

Command data goes to stdout; bounded diagnostics go to stderr with exit status 1.
Success uses exit status 0. Diagnostics do not echo filenames, values, or supplied
arguments. There is no stdin/pipe input in this release; inputs are regular files.

## Supported input

| `--format` | Accepted layout |
| --- | --- |
| `auto` (default) | A leading `0x0B` selects MLLP; otherwise one raw message beginning with `MSH` is required. |
| `raw` | Exactly one message beginning with MSH, with a terminator after every segment, including the last. |
| `mllp` | One or more contiguous `0x0B MESSAGE 0x1C 0x0D` frames, exactly one message per frame. |

`--terminator auto` accepts uniform CR, LF, or CRLF endings, detected separately
inside each message. `--terminator cr`, `lf`, or `crlf` declares the expected
ending and rejects mismatches. Endings are never normalized. Mixed endings,
concatenated raw messages, multiple messages within one frame, batch wrappers,
BOMs, extra blank lines, trailing garbage, and missing final terminators are
rejected. Repeated MSH headers produce an explicit boundary diagnostic; use one
raw file per message or put each message in its own MLLP frame. Declarations do
not authorize silently repairing malformed input.

MSH-1 and the declared MSH-2 encoding characters must be distinct printable ASCII
punctuation, excluding double quote (which denotes explicit null). Two- or
three-character MSH-2 declarations may omit the optional escape/subcomponent
delimiters; absent delimiters have no implicit default. Four-character declarations
and a fifth truncation character are also supported. Other delimiter sets are
rejected explicitly. Segment IDs are three uppercase letters/digits, beginning with a
letter. Non-UTF-8 field bytes are retained; unexpected control bytes and
unterminated escapes are rejected. Local escape contents are kept opaque, so
delimiters within a paired escape are not treated as field/repetition boundaries.

Empty, explicit-null (`""`), and omitted fields are distinct. For example,
`PID|1||ID||NAME|""|` has a null PID-6, an empty PID-7, and an omitted PID-8.
Known omitted fields appear in the tree. Unknown segments and fields retain
positional names such as `ZPD-3`; omitted unknown positions cannot be enumerated.

Field labels are supplied for MSH, MSA, ERR, PID, PV1, SCH, EVN, NTE, RGS, AIS,
AIG, AIL, AIP, and OBX when MSH-12 declares version `2.5.1`. Other versions use
positional labels. See [dictionary provenance](docs/dictionary-provenance.md).
This is **syntax inspection**, not semantic validation or a claim of HL7
conformance. No semantic SIU profile ships in this issue.

Inputs are limited to 16 MiB and 200,000 syntax nodes across all frames to bound
memory expansion. Oversized or malformed input fails before tree output or
round-trip creation. The entire source is retained in memory for byte-span views.

## Download and run

Download an archive and `checksums.txt` from [GitHub Releases](https://github.com/bharm16/readmit/releases).
The archive contains the executable, documentation, licenses, field-label source,
and synthetic fixtures. No Go installation, installer, container, or runtime
download is required.

| Archive target | Operating-system floor |
| --- | --- |
| `darwin_arm64`, `darwin_amd64` | macOS 13 or newer |
| `linux_arm64`, `linux_amd64` | Linux kernel 3.2 or newer |
| `windows_amd64` | Windows 10 or Windows Server 2016 or newer |

These floors follow [Go's supported platforms](https://go.dev/wiki/MinimumRequirements).
CI executes the actual archives on the five native runners in [the stack](docs/stack.md);
it does not independently certify every older OS release in that range.

For example, after downloading a macOS archive:

```sh
shasum -a 256 readmit_*_darwin_arm64.tar.gz
# Compare with the matching entry in checksums.txt, then:
tar -xzf readmit_0.1.0-alpha.2_darwin_arm64.tar.gz
./readmit inspect testdata/fixtures/adt-cr.hl7
```

On Linux use `sha256sum`. On Windows use
`Get-FileHash .\readmit_0.1.0-alpha.2_windows_amd64.zip -Algorithm SHA256`,
compare its hash with `checksums.txt`, extract with `Expand-Archive`, and run
`.\readmit.exe inspect .\testdata\fixtures\adt-cr.hl7`.

Prereleases are unsigned: no Apple Developer ID notarization or Windows code
signing. OS or endpoint policies may block execution. GitHub build provenance is
attached to the binaries; it is separate from OS code signing. With GitHub CLI,
verify an extracted executable using
`gh attestation verify ./readmit --repo bharm16/readmit` (or `readmit.exe`).

## Privacy and development

No telemetry, crash reporting, update checks, automatic uploads, or network
access exists in `inspect`. Product-wide, network access is limited to endpoints
the user explicitly configures; this command has no endpoint configuration.
No customer-derived data belongs in source control or CI fixtures. Logs and
reports do not print raw values without an explicit request. Round-trip copies
still contain the original evidence and inherit its handling requirements.

Build with the pinned Go 1.27.1 toolchain:

```sh
go build -trimpath -o bin/readmit ./cmd/readmit
go vet ./...
CGO_ENABLED=1 go test -race ./...
go test ./internal/hl7 -run '^$' -fuzz=FuzzParse -fuzztime=20s
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
govulncheck ./...
```

CI resolves the exact `toolchain` version from `go.mod` with
`python3 tools/toolchain.py`, passes it explicitly to setup-go, and verifies the
active compiler with `python3 tools/toolchain.py --check`. `GOTOOLCHAIN=local`
prevents automatic compiler switching. Release builds use `CGO_ENABLED=0`; race tests use cgo.
GoReleaser v2.18.2 creates the five archives and SHA-256 checksums. On a `v*` tag,
publication waits for quality checks and native tests of those same archives,
including exact archive and executable version matches against the tag;
the release is then downloaded and exercised on a fresh Linux runner with an
empty PATH. The runtime is never rebuilt between testing and publishing.
Before uploading the archives, `python3 tools/smoke.py --artifacts dist
--check-build-info` reads each packaged executable's build metadata with Go and
requires its compiler version to match the pin. Native smoke tests still run the
executable with an empty PATH. The release-tool regressions run with
`python3 -m unittest discover -s tools -p 'test_*.py' -v`.

Fixtures and their independently authored intent are described in
[testdata/README.md](testdata/README.md). Review standards and design sources:

- Stack and version pins: [docs/stack.md](docs/stack.md)
- Decisions: [docs/adr/](docs/adr/)
- Agent conventions: [docs/agents/](docs/agents/)
