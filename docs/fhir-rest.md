# FHIR R4 HTTP execution and search evidence

`internal/fhirrest` executes a finite reviewed FHIR R4 **4.0.1** request through
the existing scoped HTTPS transport. `Prepare` is offline; `Execute` requires
current external authority, performs an explicit capability preflight, and writes
an immutable acquisition artifact. `Open` and `Inspect` never contact a target,
resolve credentials, retry an action or resume interrupted work. These engine
APIs are the shared adapter for later connected FHIR integration; they add no
desktop console or separate HTTP scripting workflow.

## Reviewed request and capability boundary

`readmit-fhir-http-plan/v1` binds the exact request URL, method, original body
bytes, finite condition/Prefer headers, authentication-provider identity, explicit
FHIR base, network policy, baseline CapabilityStatement, budgets, retry policy,
optional pinned prior representation and optional typed projection. Parameter
values and resource bytes are private evidence, not operational log fields.

`internal/fhirrequest` derives `readmit-fhir-request/v1` from those exact inputs.
The typed resource subset is Patient, Encounter, Appointment, Practitioner,
Location, ServiceRequest, Observation and DiagnosticReport, plus protocol
Bundle, CapabilityStatement and OperationOutcome. Other resource types are
refused before typed execution; unsupported returned semantics remain distinct
from malformed payloads. Supported interactions are capabilities, instance read and vread, type search by
GET or form POST `_search`, create, version-aware instance update, advertised
JSON Patch, conditional create/update, transaction and batch. An instance delete
requires the separately approved `setup-action` transport purpose; a Bundle that
contains a delete does too. A delete never substitutes for cancellation.

Requests use `application/fhir+json`; JSON Patch uses
`application/json-patch+json`, and POST search uses
`application/x-www-form-urlencoded`. Patch operations and JSON Pointer escapes
are validated locally, including whole-document pointers and refusal of a move
into its own descendant. No arbitrary `$operation`, GraphQL, FHIRPath Patch,
nested transaction execution, conditional delete or unadvertised patch format is
implied. Batch entries cannot depend on another resource being created in the
same batch. Repeated explicit write targets are refused. Instance updates and
patches require authored If-Match; conditional update remains the separately
advertised conditional interaction. Conditional reads within Bundle entries are
not supported; standalone If-None-Match reads use the pinned prior contract.

The preflight fetches `BASE/metadata` under its own exact child binding, verifies
R4 and JSON support, and checks the requested interactions, versioning,
conditional features, patch format and declared search parameters. Required
search-parameter type/definition or resource-versioning changes invalidate stale
admission. Unrelated metadata text is not a capability requirement. Actual
preflight bytes and its request/response receipt are retained; server claims are
not vendor qualification or access permission.

The default preflight derives server-authenticated TLS from the action. The
optional `capability_http` declaration permits an explicit metadata-purpose TLS
client-key reference, while `page_private_key` permits a search-purpose reference
for GET pages following POST search. Both are checked during local preparation.
Credential references keep their existing exact endpoint/operation scope; this
adapter never rewrites a reference's purpose to make it fit.

## Transport, authorization and effects

The additive `readmit-runtime-http-action/v2` contract binds a closed set of
headers: Accept, Content-Type, Prefer, If-Match, If-None-Match,
If-Modified-Since and If-None-Exist. Historical runtime v1 and HTTP v1 retain their
accepted fields and behavior. There is no user-defined header map, proxy fallback,
redirect following or second HTTP client. Every child request checks the live
root authority and exact derived binding before the shared destination layer
admits DNS, TLS, credentials or payload writes.

SMART v2 request authorization calls the same pure request interpreter. POST
search needs search permission; a conditional update needs update permission.
Batch and transaction permission is the union of the actual member interactions,
not a `Bundle` create permission. Read-only member Bundles do not acquire write
permission. Metadata and continuation pages are explicit typed requests. Existing
SMART runtime v1 behavior is unchanged. No-auth laboratory use is explicit;
credentials are never an implicit fallback.

Before each attempted request, the writer durably records its exact intent.
Every response has its own exact byte file, allowlisted metadata, transport
receipt and derived outcome. A failed transport after a possible write remains
`delivery-uncertain`. There is no automatic retry of create, update, patch, delete,
batch or transaction, including after auth failure. Repeating a write requires a
new authored/approved action.

The declared retry policy applies to the authored safe read or search and its
pages. It allows at most four attempts for a transport failure, 401 renewal when
the provider supports it, 429 or 503. Each attempt receives a new destination
admission and is retained. Retry-After must fit the explicit delay and overall
time budgets. The independent capability preflight is one request; failure
refuses the operation. A 202 is `pending`, with no implicit async polling or
success claim. An async completion policy is not qualified by this release.

## Response meaning

Outcomes depend on the requested interaction. A valid 204 delete differs from an
invalid empty 204 read. Successful minimal-body writes, resources and
OperationOutcome bodies remain distinct. ETag, resource version and scoped
Location identities must agree where supplied, including with an authored
instance ID when a write returns only a minimal body or OperationOutcome. Known 401/403/404/409/412/429/5xx
responses are distinguished from transport uncertainty; a non-FHIR error body is
retained without being presented as a resource or an empty dataset.

A 304 is usable only for a read with the exact pinned URL, prior-byte digest and
matching If-None-Match/ETag. Its typed view derives from the retained prior bytes,
not a new fetch. Conditional responses record the server-reported zero/one/multiple
match implication. Batch response entries are evaluated in request order; HTTP
200 over mixed entry outcomes becomes partial failure. A transaction response
must match every member. Neither a successful transaction response nor the lab's
rollback proof establishes downstream business processing completion.

## Complete collection and consistency

Search follows the retained Bundle next-link graph within the declared base and
origin. Type-relative paging URLs and opaque base paging URLs are supported;
each continuation binds its resource scope and the preceding response digest.
Cycles, changed resource types, wrong origins, malformed links and budget
exhaustion stop collection with explicit incomplete evidence. Reference and
Location values do not trigger hidden follow-up requests.

The initial request's decoded parameter multiplicity is compared with each
returned self link. Missing or changed filters, unsupported entry classification,
`_summary`, `_elements`, SUBSETTED resources and changed versions prevent a
complete projection. A missing Bundle.total is permitted; total never determines
when collection ends. Match, include and outcome entries have separate counts. Search entries must
carry usable resource bodies; an explicit outcome entry must carry an
OperationOutcome. Missing bodies are protocol-invalid. An omitted optional search
mode remains retained with unknown coverage, without guessing match/include.

`matches` counts distinct logical identities; `match_occurrences` counts received
matching entries. Repeated logical resources remain in raw pages and explicit
overlap records, and are projected once rather than counted as a second entity.
Distinct resources sharing a business Identifier remain distinct. Version/body
changes across occurrences are recorded and make coverage unknown. All original
page bytes remain available, including the versions omitted from a unique-row
projection.

A fully collected search does **not** claim a transactional snapshot. Consistency
is `not-declared` unless observed changes establish `changed-between-pages`.
The retained `execution_state` records acquisition termination independently
from the derived final state. Collection coverage and projection status are
recomputed independently: a missing required projected field can yield `unknown`
with complete collection coverage, and reopening reproduces that same result.
Cancellation stops acquisition and retries. Finalization still derives already
retained pages under their fixed byte/node/value limits, independently of the
canceled network context; offline reopening therefore reproduces partial
projections as well as incomplete coverage.
Projection resource type must match the authored search/read scope before any
request is sent. Collection coverage, projection status, version evidence and
consistency are separate from later observation-window or clinical assertion policy.

Budgets are explicit: up to 64 pages, 16,384 processed entries, 128 MiB response
bytes and five minutes, with a separately bounded per-response transport limit.
Individual FHIR JSON bodies retain the existing 16 MiB/100,000-node/depth-64
interpreter bounds. A final received page crossing a row or byte budget is retained and marked
incomplete. Reaching a bound is never passing absence.
The containing artifact has its own 256 MiB/800-file bound.

## Retention, privacy and verification

`readmit-fhir-http-result/v1` retains the plan/policy, ordered intents and attempts,
original response bytes, headers, outcomes, coverage, overlaps and projections.
The offline reader reads one bounded filesystem snapshot, validates the seal,
reconstructs exact child bindings and page links, and re-derives outcomes and
projections using `internal/fhirr4`. It rejects orphan journal records, changed
source digests, contradictory summaries and response claims without valid
transport receipts. `OpenEvidence` exposes detached request/response bytes from that same verified
snapshot, so downstream consumers need no second filesystem read. Moving an unchanged artifact does not change its meaning.
Interrupted inspection reports uncertainty and never executes an intent.

Authorization/cookie headers are absent from retained metadata. Known credential
echoes are withheld and recorded as `response-withheld`, not written as raw
resource evidence. The redacted `readmit-fhir-http-summary/v1` contains states and
counts; default formatting of plans/results/attempts does not include private
URLs, parameters or bodies. Full JSON result documents are private evidence and
must not be used as operational logs or ordinary UI status projections.

Independent local TLS fixtures implement a separate resource/version store and
exercise create/read/update/patch, conditional matching, version conflict,
transaction success/rollback, batch partial failure, lost committed responses,
three-page searches, overlaps, response classifications and retry behavior.
Offline relocation and resealed-tampering tests exercise the actual reader;
SMART tests exercise the actual protected runtime. These fixtures qualify the
implemented engine boundary, not a deployed EHR vendor, customer permissions,
clinical expectations or universal server consistency.

Protocol sources: [FHIR R4 HTTP](https://hl7.org/fhir/R4/http.html),
[FHIR R4 search](https://hl7.org/fhir/R4/search.html), and
[SMART 2.2 scopes](https://hl7.org/fhir/smart-app-launch/STU2.2/scopes-and-launch-context.html).
