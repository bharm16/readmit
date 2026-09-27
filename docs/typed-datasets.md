# Retained typed datasets

The connected engine can retain fields alongside business keys. Each
`readmit-dataset/v1` has an exact run, before/after phase, namespace and source
configuration identity; an explicit projection; acquisition facts and effective
projection limits; an immutable material reference; and ordered row occurrences.
A row ID identifies an occurrence inside that retained snapshot. It is never a
business key, a resource identifier or evidence of business ordering. Equal keys
remain separate rows. Keys, digests and projected values may identify people and
remain customer-local sensitive evidence.

`readmit-dataset-projection/v1` declares up to 32 distinct column selectors, their
types, business-key roles, requiredness, repetition handling and limits. JSON,
CSV, XML and text use `internal/importer`'s envelope dialects and bounded record
readers. HL7 uses the existing lossless selector and character-set reader. SQL
uses validated column identifiers and the existing fixed, parameterized SELECT
builder over an approved view. There is no SQL text, script or expression in a
projection. Source-specific adapters retain their own provenance.

States are `present`, `empty`, `null`, `absent`, `invalid` and `unreadable`.
JSON null and XML `xsi:nil` are source states; HL7's explicit null is interpreted
only by the HL7 reader. JSON arrays and repeated XML elements/HL7 fields retain
ordered items, including repeated equal values and nulls. A scalar selector that
finds repeated XML elements is invalid, never first-match. A scalar HL7 selection
into repeated fields or segments requires explicit occurrence/repetition indices.
A null JSON collection stays distinct from a collection containing a null member. XML namespaces are
refused until a namespace-aware locator is supported, rather than silently
matching local names. Missing required columns or invalid/unreadable values make
the dataset unusable. Optional absent values remain explicitly absent.

Decimal spelling and fractional precision are retained without float conversion.
Booleans, text, dates, local date-times, offset date-times and explicitly declared
code systems retain distinct types. Missing timezone information stays absent;
a date does not become midnight UTC. Timestamp precision stays explicit.
Database material is `readmit-dataset-database-read/v1`: column metadata and the
typed values returned by `database/sql`. It is not original database wire bytes.
Driver-reported precision/scale are recorded as returned, separately from the
literal's precision. Floating-point decimal results are unusable for exact
checks; an approved view must return an exact representation. Invalid string
bytes are retained as such. Oracle DATE retains its time of day; PostgreSQL and
SQL Server DATE retain a date. Unrecognized native time types are refused.

The dataset's `material.bin` holds exact file/HTTP bytes, a typed driver result,
or `readmit-dataset-capture-read/v1` containing verified capture occurrence bytes
and original occurrence IDs. Record offsets/sizes locate envelope records in the
original material. No response authorization header or resolved credential is
retained. `Open` validates the identity, re-derives the projection solely from
retained material, and compares the complete manifest. Editing a mapped value and
recomputing the outer digest cannot make it agree with unchanged source bytes.
The hash still proves integrity, not that the source told the truth.

## Acquisition and completeness

`observesource.CollectDataset` reserves an acquisition directory before effects,
uses existing source readers and credential/destination rules, and returns the
sealed snapshot. `readmit-dataset-acquisition/v1` retains the network decision
where applicable and binds the nested dataset. `OpenDataset` uses no original
path, URL, database connection or credential provider. Caller-supplied authority
must bind the selected source and projection to the approved action. HTTP and database acquisitions can consume the shared scoped action authority
through the v2 acquisition envelope. Their compiled plan, source, environment,
policy and purpose are checked before effects; historical permissions remain
unchanged.

A complete snapshot means one declared source response was fully acquired and
projected. It does not mean a downstream observation horizon ended. Missing,
failed, cancelled, truncated and invalid projections are unusable, including a
prefix that contains apparently sufficient records. A schema-valid complete
empty dataset is a real observation. HTTP JSON projections must explicitly name
the source's continuation member; a nonempty continuation prevents completeness.
Any HTTP Link header is conservatively refused as incomplete. Nothing follows a
page, polls for a desired count, or infers a source's pagination protocol.

Limits cover rows, source/material bytes and acquisition time. Envelope projections
retain the shared reader's 128-record ceiling; HL7/database projections allow up
to 10,000 rows. Independent expansion limits of 100,000 typed values and 32 MiB
of encoded projected rows are retained in the manifest. The final artifact is
bounded before a snapshot can be returned as usable. Read attempts, retries,
HTTP status/freshness and capture observation intervals are retained as typed
acquisition facts. Database drivers
can allocate a single returned value before the collector sees it; approved
views must bound field sizes, as for existing key-only reads. Declared projection
limits may not widen the source's configured read limits. Raw failed material is
kept when it fits the global retention bound, but cannot enter passing checks.

## Connected plans and assertions

`readmit-connected-test/v2` and `readmit-execution-plan/v2` add explicit typed-row
dataset namespaces and pinned projection references. Version 1 refuses these
members even if null; its operators and identities keep their meaning. V2 uses
`readmit-dataset-assertion-set/v1` as its operator version. Compilation checks
source, namespace, phase, projection identity and every requested column/type before
any acquisition. Every declared typed dataset must have a binding; no required
dataset can be silently omitted from evaluation. Existing `connected prepare`
can compile this contract offline.

The IG06 handoff is `connectedtest.CollectDataset`: the caller selects a declared
dataset ID and source, while the compiled plan supplies the projection and exact
binding. The caller receives a snapshot directly; no manual dataset-file step is
introduced. `EvaluateDatasets` consumes these snapshots in the shared assertion
package. `RetainDatasetResult` copies the plan and datasets into
`readmit-dataset-execution-result/v1`; `OpenDatasetResult` re-evaluates that retained
result without any live source. A passing snapshot report cannot turn an
uncertain, cancelled or incomplete execution into a pass.

Dataset assertion bindings pin namespace, phase, source and projection identity;
the execution supplies the exact run instance. Operators are finite typed Go
operations:

- `value-equals` compares the complete typed representation, including precision,
  timezone metadata and ordered repetitions.
- `decimal-equals` compares exact rational values while retaining original
  spelling and precision. `instant-equals` compares explicit instants; absent
  timezones, date-only values and precision finer than nanoseconds are undecided.
- `value-related` and `value-changed` compare exactly one selected row on each
  side. They compare representations, with explicit before/after bindings.
- `row-count` retains multiplicity; `unique-keys` checks the declared typed key
  representations without deleting duplicate rows.
- `each-equals` applies bounded `every`, `any` or `none` quantifiers. `every` over
  no selected rows is undecided, rather than a vacuous pass.
- `sequence-equals` requires explicitly declared source order. Database row order
  is recorded but remains unordered for this operator; no business order is
  inferred from query return order or sorted keys.

A single-row selection returning zero or multiple rows reports `no-row` or
`multiple-rows`. Conditions can skip a check, and skips/undecided stay distinct
from passes. A missing, unusable or wrongly bound dataset is an evaluation error.
Old occurrence-ID and key-only assertion contracts retain their own meaning.

## Remaining integration

The engine acquisition/evaluation/retention handoff is exercised by an end-to-end
contract test. The full IG06 runtime still owns send/collect scheduling, bounded
horizons, setup/cleanup and cancellation across protocols. Native Observation and
Tests flows remain with their desktop owners. This delivery adds no frontend,
facade, page redesign or separate manual dataset workflow. Database tests exercise
the actual PostgreSQL driver against an independent TLS protocol fixture; they
are not qualification of every server/version/driver cell.
