# Run bundle format: readmit-run/v1

A run is a new customer-local directory following ADR-0002. It contains no
database. Finalized evidence is immutable; readers reject incomplete, unknown
version, unknown-member, incorrectly hashed, or internally inconsistent artifacts.

```text
NEW_RUN/
  manifest.json
  events.jsonl
  identity.sha256
  payloads/
    o000001-source.bin
    o000001-intended.bin
    o000001-sent.bin
    o000001-received.bin
    ...
```

Payloads never pass through JSON text. Empty sent/received files are retained for
unattempted or refused deliveries. Source bytes include their original framing;
intended/sent/received files contain application-level MLLP bytes. For TLS these
are the plaintext bytes written through, or read from, the verified TLS connection,
not handshake packets or encrypted TCP records.

`manifest.json` includes:

- `schema: "readmit-run/v1"`, `state: "complete"`, observed `started_at` and
  `completed_at`, and the selected `message_count`.
- `contains_source_values: true` and `export_policy: "customer-local-only"`, always,
  including when no transformation is selected. Original run manifests and
  source-to-surrogate material must never be copied into a share-oriented export.
  A derived export is separately assembled from approved derived artifacts.
- `source_bundle_identity`, the verified source case's content identity.
- `target`: address, transport, test-endpoint/approval declarations, connect and
  message timeout strings, maximum ACK bytes, and optional `ca_sha256` for an
  explicitly supplied CA snapshot. No CA source path is retained.
- Ordered `mappings`, each with `source_occurrence`, `outbound_occurrence`, and
  `source_sha256`. Outbound IDs are `o000001`, `o000002`, etc. Source IDs retain the
  exact case event IDs, such as `s0001-e000003` after an interleaved captured ACK.
- The ordered `transformations` list and `changes`. Every change names its
  transformation, source and outbound occurrence, canonical shared selector,
  `old_state`, `new_state`, `old_base64`, and `new_base64`. Base64 preserves literal
  source bytes, including non-UTF-8 bytes; field states remain explicit.

Every line of `events.jsonl` is one selected message, including those not attempted
after a failure. An event has source/outbound occurrence IDs, `source`, `intended`,
`sent`, and `received` payload descriptors (`path`, `size`, `sha256`), decoded
`control_id_base64`, outcome, delivery certainty, `elapsed_ns`, ACK correlation,
and either a `transport_error` object or null. A transport error stores only a
fixed `phase` and `class`, never an arbitrary operating-system error string or
source value. ACK metadata includes `correlation` (`none`, `invalid`, `mismatched`,
or `matched`), the supported AA/AE/AR `code` when available, and decoded
`control_id_base64`. Raw MSA/ERR values remain available in received bytes for
shared-selector consumers.

`elapsed_ns` uses Go's monotonic elapsed clock. The first attempted event includes
connection/TLS setup; every attempted event includes message transport. It excludes
writing the completed event to disk. Unattempted events have zero elapsed time.
ACK matching refers only to the current outstanding message, so duplicate source
control IDs cannot collapse occurrence mappings.

The writer preflights storage bounds and reserves a new directory with an
in-progress manifest before dialing. Source and intended bytes are written before
delivery; each completed attempt then persists actual sent/received bytes and an
event line. Normal transport failures finalize the run, including the remaining
unattempted events. The complete manifest is installed and `identity.sha256` is
written last. Missing completion evidence, abrupt termination, and write failures
leave an incomplete artifact, which is retained for inspection and refused by the
verified reader. No existing output directory is overwritten. New directories and
files request permissions 0700 and 0600 respectively.

The identity is SHA-256 of `readmit-run/v1\n` followed by every relative path and
file content, excluding only `identity.sha256`, in bytewise path order. Each path
and each content is prefixed by its eight-byte unsigned big-endian byte length.
The marker contains the lowercase digest and LF. Filesystem timestamps, absolute
paths, and directory names do not affect identity. This detects content changes;
it is not an authenticity signature.

## Go consumer boundary

All replay APIs are in `internal/replay`:

```go
target, err := replay.ReadTarget(configPath)
plan, err := replay.Prepare(sourceCasePath, target, replay.Options{
    Occurrences: []string{"s0001-e000001", "s0001-e000003"},
    Transformations: []replay.Transformation{{Name: "rebase-control-ids"}},
})
run, err := replay.Execute(ctx, plan, newRunDirectory)
verified, err := replay.Open(newRunDirectory)
bytes, err := verified.Raw(verified.Events[0].Sent)
```

Handle each error before continuing. `Prepare` is local-only and seals verified
source location/identity, CA bytes, transformations, and payloads. `Plan.Count`,
`SourceIdentity`, `Target`, `Mappings`, and `Outbound` expose its preview; returned
payloads and mappings are private copies. `Execute` is the only network boundary.
Transport failures are finalized event outcomes and return a run without a Go
error; configuration or storage failure returns an error. `Run.Successful()` is
true only when every event has a correlated AA.

`Open` verifies strict JSON, completion, directory identity, every payload hash,
source occurrence ordering, source-to-outbound mapping, transformation records by
reapplying the named operators to retained source bytes, intended bytes, actual
sent prefixes, ACK metadata, and outcome consistency. `Raw` returns a private copy
only for a matching descriptor. Files must be regular, with no symlinks or
unexpected directories/files; sizes and counts are bounded before loading.

Diff alignment uses `(source_bundle_identity, source_occurrence)`, verified against
the compared case's identity and that occurrence's hash. It never aligns regenerated
control IDs as if they were source IDs. Partial `sent` bytes must not be presented
as a complete delivered message; `intended` preserves the planned complete message
for inspection. A test runner independently binds its initial/final observation
session and ordered receipt list. Replay neither collects observations nor assumes
its outbound IDs equal the receiver's event IDs.
