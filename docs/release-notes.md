Initial unsigned preview of `readmit inspect` for issue #1.

- Byte-preserving raw and MLLP syntax inspection with explicit format reporting.
- v2.5.1 field labels, positional fallback, distinct empty/null/omitted states.
- Values hidden by default; `--show-values` explicitly displays escaped bytes.
- `--roundtrip NEW_FILE` writes an exact copy without overwriting evidence.
- Five native-tested targets, SHA-256 checksums, and GitHub binary provenance.

Download the archive for your OS and architecture and compare its SHA-256 with
`checksums.txt` before extraction. Run `readmit inspect testdata/fixtures/adt-cr.hl7`
from the extracted directory (`.\readmit.exe` on Windows). No Go installation is
needed. See the bundled README for accepted formats, limits, and platform floors.

This is syntax inspection only. It does not validate HL7 semantics, edit data,
send messages, or access the network. All bundled fixtures are synthetic.

Prerelease binaries have no Apple notarization or Windows code signing.
