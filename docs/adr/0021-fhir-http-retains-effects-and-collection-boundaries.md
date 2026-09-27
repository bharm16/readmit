---
status: accepted
date: 2026-09-27
---

# FHIR HTTP retains effects separately from collection and workflow meaning

IG11 adds a finite pure FHIR request interpreter and the shared `fhirrest`
executor. The interpreter derives interactions and permissions from exact
reviewed HTTP bytes, using the existing `fhirr4` parser for resource and Bundle
meaning. The executor composes scoped network admission, explicit capability
preflight, optional SMART authorization, durable intents and immutable evidence.
It does not introduce another HTTP stack, frontend parser or generic script API.

The additive runtime HTTP v2 contract names only the condition and media headers
needed by these interactions. New page scope names the preceding response digest
and selected resource type. Historical runtime, HTTP, secret and FHIR evidence
contracts retain their fields, limits and meanings. Metadata/page TLS references
are separately declared where operation-scoped references differ; no credential
reference is widened to cover another purpose.

A request intent is durable before execution. Exact response bytes and transport
receipts precede derived outcomes. Lost write responses remain uncertain and are
never retried automatically. Known HTTP responses, pending work, partial batch
failure and successful transport are not downstream workflow verdicts.

Complete pagination, resource identity/version, paging overlap, projection
support and temporal consistency are separate facts. The client never treats
Bundle.total, a page entry count or an unchanged business Identifier as proof of
complete application state. Typed projections share the existing FHIR value
semantics; original pages remain retained even when repeated logical resources
are represented once in a projection.

Offline readers verify a single bounded byte snapshot and reproduce the request
graph, classifications and projections. Passive inspection cannot resolve keys,
refresh metadata, poll a target or execute an interrupted intent. Operational
summaries contain counts and categorical outcomes, while URLs and parameter
values remain protected evidence. Backend integration preserves the existing
redesigned UI and review ownership.
