---
status: accepted
date: 2026-09-27
---

# Observation completion requires the entire declared interval

Use `internal/observeinterval` for new connected observation lifecycle and
completion. It records readiness and a baseline before stimulus, retains samples
while stimulus runs, and completes only after a full monotonic post-stimulus
horizon or a separately configured, observed processing barrier scoped to run,
work and destination. Expected values/counts and stable samples cannot complete
this version. Runner and transport deadlines remain distinct safety boundaries.

The existing `observewindow/v1` quiet-period contract remains unchanged. New
connected tests/plans use v3 with pinned interval policies, and the existing test
workflow selects connected-run configuration/result v2. No desktop navigation,
review-token or save contract changes here.

Live capture reuses the receiver's durable capture journal and finalizes a normal
case once. It never edits sealed evidence or introduces a second byte writer.
A logical live source has a stable identity, mapped explicitly to one run-owned
capture. Scope exclusions preserve original occurrences and are re-evaluated on
readback. Missing/unhealthy/truncated coverage cannot support absence.

Snapshots retain their actual acquisition/source provenance. Polling guarantees
only observed samples; a bounded horizon cannot rule out future output, and a
fresh API request cannot certify upstream freshness. Old business timestamps are
not replaced by collection timestamps. Independent owned targets and deterministic
clocks test these limits.

Nested verification is tied to one retained outer byte snapshot, including
source authority and captured spool health. Recovery is read-only and never
resends, resumes or silently joins another runtime. Partial evidence survives
cancellation and process death; releasing sockets does not remove that evidence.


Business elapsed-time stamps and actual I/O recording/acquisition timestamps are
separate clock domains. The retained transport completion marker binds the
interval lifecycle and observations to this actual replay. An observed barrier
precedes the final business-state acquisition it certifies. Transport uncertainty
is sticky through subsequent observation failures and cancellation. None of
these checks changes the frozen v1/v2 horizon limits or historical readers.
