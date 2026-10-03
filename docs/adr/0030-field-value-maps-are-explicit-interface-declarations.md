---
status: accepted
date: 2026-10-03
---

# Field value maps are explicit interface declarations

A `readmit-field-value-map/v1` record belongs to exactly one interface project.
It records a name, an actual HL7 edition, exact canonical source and destination
selectors, their authored meanings, optional table identity and provenance,
source-to-destination entries, and optional explicit run/configuration revision
references. A shared table never broadens selector scope. Each source value has
one destination; duplicate sources, malformed CSV and incomplete scopes refuse
the whole draft. Values and meanings are bounded authored data, not copied
clinical evidence.

The existing project catalog publishes immutable whole-object revisions using
its intent/base conflict owner. CSV import reads one bounded UTF-8 document into
a draft, never publishes automatically. Export writes validated CSV to a new
explicit destination. Its strict columns repeat all declaration metadata, so
reimport can verify scope rather than assume it from a table number. Association
references are retained exact existing run/environment/test revisions and mean
only that the author recorded a declaration, not that a receiver deployed it.

Contextual inspection re-verifies source identity and exact selected field,
reads its value through the shared Go parser, and requires explicit reveal
before exposing a mapped destination. Absent, undecodable, hidden, incompatible,
not-applicable and unmapped remain distinct. Maps do not execute, transform
messages, modify external engines or establish receiver errors. There is no
cross-interface inheritance or automatic mapping application.

Incomplete authored map and entry edits use the existing private editor-draft
store under strict `readmit-field-value-map-editor/v1`, with exact source IDs,
selector/edition and optional catalog base revision. The editor restores only
an exact matching scope, with reveal off. The draft contains authored entries,
not inspected field values or consent. The existing bounded store can refuse
retention; that failure remains visible. Saving or explicitly discarding drops
the private draft through the same retention owner.

The draft's actual source edition may remain explicitly unknown or omitted.
Choosing an authored map edition never fills that source context. Publication
still requires the map's strict edition and selectors; contextual inspection of
an unknown source edition remains incompatible and exposes no destination.
