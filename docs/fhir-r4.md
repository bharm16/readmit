# Offline FHIR R4 JSON evidence

`internal/fhirr4` interprets explicitly selected **FHIR R4 4.0.1 JSON**. It
retains the original bytes and exposes typed resources, selectors, references,
dataset projections and CapabilityStatement claims. It is the Go interpreter
for later UI bindings; no JavaScript parser, FHIR console, network discovery or
parallel v2 message model is introduced.

This is an evidence/projection implementation. A parsed resource or a
schema/profile-conformant payload does not establish workflow success.
Whole-profile/implementation-guide validation is the optional
pinned validator worker in [FHIR profile validation](fhir-validation.md);
protocol acquisition, search completeness and interactions remain IG11. This
package has no HTTP client, resolver, credential provider or terminology fetch.

## Public boundaries

- `Decode` reads bounded original bytes with an explicit version, base and media
  type. `Raw`, `ResourceBytes`, `Resources` and `Findings` expose owned copies or
  immutable views. Malformed JSON is refused by the decoder.
- `Select` accepts only typed field, choice, index and all-items steps. It
  reports present, absent, multiple, ambiguous, invalid or unsupported results.
  It does not evaluate FHIRPath, JavaScript, SQL or expressions inside extensions.
- `Resolve`, `ResolveCanonical` and `Join` search retained resource identities
  in the document's declared scope. They never fetch an external reference.
- `Capabilities` parses finite CapabilityStatement claims: the declared
  interactions, searches, versioning and conditionals, nothing evaluated or
  defaulted. Claims are data, never permission or workflow success.
- `Project` derives a typed `readmit-fhir-dataset/v1` dataset from the decoded
  document under an explicit `readmit-fhir-projection/v1` projection, every
  call from the same retained bytes. Sealed retention of HTTP exchanges is the
  REST layer's `readmit-fhir-http-result/v1` in [FHIR HTTP](fhir-rest.md).

The qualified resource projection vocabulary covers Patient, Encounter,
Appointment, Practitioner, Location, ServiceRequest, Observation and
DiagnosticReport. It includes their identifiers, names/administrative fields,
status/time fields, participants, subject/encounter/order/result relationships,
repeated components and choice alternatives. Bundle, CapabilityStatement and
OperationOutcome are protocol artifacts. Other resources remain byte-exact and
inspectable; semantic selectors for them report unsupported. A dictionary of
field types is not a claim of complete profile validation.

| Owned qualification | Public test |
| --- | --- |
| All eight resource types and independently specified values | `TestFHIRGoldenFiniteProjectionsCoverEachNamedResource` |
| Appointment multiplicity, cross-base IDs, participant/order/result joins | `TestFHIRGoldenResourcesKeepIdentitiesReferencesAndRepeatedPrimitiveAlignment` |
| Primitive companions, aligned arrays, null/empty/choice refusals | `TestFHIRPrimitiveShapesAndChoiceErrorsStayInvalidNotHL7Null` |
| Exact large decimals, partial dates, long fractions and leap-second syntax | `TestFHIRRetainsOriginalBytesExactNumbersAndPrimitiveMetadata`, `TestFHIRBoundsAndTemporalPrecisionAreExplicit` |
| History-version, fullUrl, URN, contained and logical-reference scope | `TestFHIRReferenceContract*` |
| Capability presence, wrong version, malformed declarations and conditional behavior | `TestFHIRCapabilityClaimsStayFiniteAndVersionPinned`, `TestFHIRCapabilityShapeAndConditionalClaimsAreNotDefaulted` |
| Typed projection over the golden collection | `TestFHIRTypedProjectionOverGoldenCollection` |

The wholly fictional fixtures under `testdata/fhir-r4` are authored separately
from the decoder. They do not claim EHR compatibility or clinical correctness.

## JSON and primitive meaning

The decoder uses the standard library's strict JSON token stream, including
unique-member and UTF-8 checks, and keeps numeric token text instead of converting
it to binary floating point. `9007199254740993.1200`, exponent spelling and
trailing zeros remain unchanged. Original string escaping, member order,
whitespace and unknown extension bytes remain in the raw material.

FHIR `date` retains year, month or day precision without a timezone. `dateTime`
permits date-only/partial-date forms; when time is supplied, seconds and an
explicit zone are required. `instant` always requires both. Fractional digits,
`Z` versus numeric offsets, and leap-second lexical forms are preserved; the
reader does not truncate to nanoseconds or invent a UTC instant from a date.

Primitive `_element` companions retain their original id/extension content.
Repeated primitive values and companions are aligned by position, including
legal null padding and extension-only slots. A missing value is absent with its
metadata preserved. JSON null outside that representation, empty strings,
empty objects/arrays, wrong primitive kinds and multiple `[x]` alternatives
produce invalid findings. None is translated into HL7 explicit null.

Ordinary unknown extensions remain opaque data. Their expressions are never
executed. Uninterpreted modifier extensions or implicit rules prevent a
semantic projection from being treated as supported. Governing Bundle rules
and entry modifiers also apply to child resources and empty projections; finding
budget exhaustion cannot become a supported absence. Values from unsupported
resources or datatype paths remain explicitly unsupported.

## Identity and reference scope

An occurrence is an internal sequence identifier, scoped by its document,
not a resource ID or business key. Resource summaries keep these facts separate:

- resource type, declared base, logical `id`, and `meta.versionId`;
- each business `Identifier.system` plus `Identifier.value`;
- Bundle `fullUrl`, containing-Bundle membership, contained-resource owner and JSON pointer;
- canonical `url` and business `version`, which is not `meta.versionId`.

A business identifier may identify multiple Appointment resources; all stay in
the evidence and projection. Equal logical IDs at different bases stay distinct.
A reference's display text is never an identity.

Relative `type/id` references inside a Bundle use the RESTful fullUrl root of
the containing entry. An URN entry or an entry with no fullUrl does not borrow a
global base. Standalone resources may use their explicitly declared base.
Nested and sibling Bundles have separate reference scopes, including URN and
logical Identifier matching. An inner Bundle cannot borrow an outer or sibling
entry. Version-specific references match both unversioned fullUrl and
`meta.versionId`. Multiple candidates remain ambiguous. Contained `#id` and
parent `#` references stay within their container; they cannot cross Bundle
entries. Logical references match exact qualified Identifier pairs and keep
multiple matches explicit. Canonical references split URL/business version/
fragment; without an explicit version, multiple retained versions remain
ambiguous rather than guessing which one is current.

An unmatched absolute reference reports external. This is a description of
retained evidence, never permission to connect. No reverse/transitive patient
relationship is inferred from another relationship.

## Typed dataset compatibility

FHIR projections reuse `dataset.Value`, `dataset.Row` and `dataset.Provenance`
as typed carriers under the separate `readmit-fhir-dataset/v1` schema. Each
value retains exact text, precision, timezone and code system; per-field
provenance adds JSON pointers, FHIR datatypes, canonical parts and primitive
companions. Row IDs retain occurrence multiplicity. `Provenance.Offset` is
`-1`: a JSON pointer is not an HL7 selector or an asserted v2 byte offset.

The existing `readmit-dataset/v1` reader, its date/null rules, v2 cases and HL7
selectors are unchanged. In particular, a FHIR partial date is not sent through
the legacy full-date converter, and a FHIR primitive-array placeholder is not a
legacy null. Consumers must select the new contract explicitly. A projection
is not a search-completion receipt or an assertion evaluator.

Coding code/system/display and Quantity value/unit/system/code remain distinct.
The reader performs no terminology lookup, UCUM conversion, unit equivalence or
patient-identifier normalization. An absent code system stays undeclared.

Required absent values, invalid or ambiguous selections, unsupported selected
complex values and resource/byte/value limits make the projection unusable.
Every call re-derives every value from the decoded document's retained bytes,
so there is no sealed projection to edit or reseal. Refusing HTTP failures and
request bodies as observation datasets is the REST layer's outcome
classification in [FHIR HTTP](fhir-rest.md).

## Bounds

Decoding is bounded: 16 MiB per JSON body, 100,000 nodes, depth 64, 1 MiB per
decoded scalar, 4 KiB per JSON pointer and 8 MiB total pointer storage, 4,096
resources and findings, 10,000 selector matches, 16 selector steps, 32 columns
and 32 MiB projected output. Exceeding a bound is refusal or an explicit
unusable status, never a successful prefix.

Acquisition context, sealed retention and tamper verification belong to the
layer that performs the exchange: [FHIR HTTP](fhir-rest.md) for REST
interactions and [connected FHIR](connected-fhir.md) for lifecycle
observations. All evidence remains private customer-local material, including
identities and queries.

## Fixed primary contracts

Implementation uses the permanent R4 4.0.1 definitions, not a rolling build:
[JSON representation](https://hl7.org/fhir/R4/json.html),
[primitive/composite datatypes](https://hl7.org/fhir/R4/datatypes.html),
[resource identity](https://hl7.org/fhir/R4/resource.html),
[reference and canonical scope](https://hl7.org/fhir/R4/references.html),
[Bundle resolution](https://hl7.org/fhir/R4/bundle.html#references),
[extensions](https://hl7.org/fhir/R4/extensibility.html), and
[CapabilityStatement](https://hl7.org/fhir/R4/capabilitystatement.html).
The finite resource field declarations follow the corresponding fixed R4
resource pages. Capability resource-type names use the fixed
[R4 ResourceType vocabulary](https://hl7.org/fhir/R4/codesystem-resource-types.html),
including valid resource types outside the eight qualified projection families;
unknown names cannot satisfy a requirement. Only its code spellings are compiled,
with version and source hash recorded in `resource_types.go`; no descriptions or
translations are copied. No external terminology catalogue, validation package or
implementation guide is bundled by this change.
