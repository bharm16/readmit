# ADR 0024: connected proof is retained whole and extracted value-free

Status: accepted

## Context

IG14 needs a result from an actual external v2 or FHIR lifecycle to be
reopened, re-evaluated, compared and handed to a reviewer without querying the
original sources or substituting the built-in fixture. The v1 retained packet
binds a case, a specification and a `readmit-test/v1` result; a connected
lifecycle already binds its own plan, inputs, pins, observations, responses,
completion records and verdicts in one sealed, offline-verifiable directory.
Its evidence surfaces (FHIR narrative, extensions and attachments, HTTP
queries and errors, typed rows, bound server identities, setup resources,
validator output) are new to disclosure review, and no transformation of them
exists.

## Decision

- A new `readmit-retained-packet/v2` copies whole lifecycle results
  (`readmit-connected-run/v3` and `v4`) byte for byte under `current/`, an
  optional distinct `baseline/` and an optional distinct `replay/`. Its
  manifest states claims re-derived by `connectedrun.OpenFlowEvidence`, which
  verifies exactly as `OpenFlow` does and also returns the tables, bindings
  and validator runs behind the verdicts. v1 packets, reviews and reports and
  their readers are unchanged; each version refuses the other.
- Verification names a changed file by section and evidence surface. A
  resealed outer layer is still refused by the nested seals and re-derivation;
  a rewritten claim is named as the claim the evidence contradicts.
- Reanalysis with the current release is reported beside the sealed original
  engine and verdict and never written into the packet. Validator output is
  reinterpreted from its retained capability; the validator is not rerun, and
  that is stated.
- `runcompare.CompareFlows` compares two verified lifecycles by declared
  dimension (check definition, input, environment, protocol boundary, target,
  profile and validator, completion policy, collector, engine), by check
  (behavior only under an unchanged definition) and by record through declared
  key columns with multiplicity, separating server-assigned identity columns
  from field values. Several changed dimensions are never attributed to one.
- `readmit-portable-review/v2` renders one typed `readmit-portable-report/v2`
  (claims per check kind, protocol-specific step summaries, typed record
  fields, bound identities and inert evidence links) into the five existing
  formats, regenerated and compared on every open.
- The share-oriented export of connected evidence is a value-free
  `readmit-connected-extract/v1`. A `readmit-connected-disclosure-policy/v1`
  must map every surface the packet holds (the only disposition is
  `exclude`); an unmapped surface or any credential material blocks it; a
  residual scan over every value the packet observed or bound must pass; and
  publication requires the exact previewed identity.
- Equivalence is a packet claim decided from retained executions only:
  `reproduced` needs a distinct complete replay of the same plan against the
  same declared environment and target revision failing with the same failure
  signature. An extract's equivalence is always `unverified`.

## Consequences

Original evidence, reviewed extracts and reproduced regressions stay distinct
contracts, and nothing in them requires the original sources. Packets are
larger than v1 (bounded at 60,000 files and 512 MiB). A reviewed extract that
carries transformed FHIR or v2 content, rather than none, needs a reviewed
transformation this release does not have; the policy's single disposition
leaves room for one as a new policy version. The redesigned Report and Share
views bind these operations later; the command line is the only surface now.
