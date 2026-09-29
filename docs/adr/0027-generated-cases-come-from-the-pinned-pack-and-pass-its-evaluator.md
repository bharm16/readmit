---
status: accepted
date: 2026-09-28
---

# Generated cases come from the pinned pack and pass its evaluator

A saved scenario is turned into executable v2 cases by `internal/casegen`, a
new versioned contract (`readmit-case-generation/v1`) beside the unchanged v1
generator. Three decisions shape it.

**The pinned pack decides.** Event availability and message structure come from
the local profile and profile pack the request pins by digest. An event is
available only when the pack declares a rule for the event under its own name
for the profile's version and family, and that rule equals the rule of the
structure HL7's Table 0354 assigns the event. The generator keeps that
event-to-structure table because the v5 pack records aliases as copies without
naming their source; the table never makes an event available, it only names
MSH-9.3. Field positions, withdrawal, types and lengths are read from the same
rules. No other version or family is consulted.

**The evaluator gates positive cases.** Every positive message is read back
through the lossless reader and evaluated by `internal/profileeval`. A message
that reads back differently or fails any requirement other than a workflow
transition makes its event unsupported under that profile; it is never repaired
or emitted. Negative cases are declared, built exactly as declared, evaluated
when they parse, and never gated.

**Content identity is not execution identity.** Output is a pure function of
the request and the two pinned documents. MSH-10 is derived from the
generator version, seed, scenario, row and step by SHA-256, so an independent
verifier recomputes it from the documentation and a retransmission or reorder
keeps it. This narrows ADR-0003's rule that synthetic generation draws from the
seeded PCG generator: the seed still decides every identifier, but through a
documented digest rather than a stream. Each case holds one source per message
and every designed step is a phase. A case is identified by its content, so a
later generation that says the same thing names the case already held rather
than writing it again. Repeated generation answers the same cases; each
execution of a case is a separate run instance.

**A resource's participation changes through its own event, in HL7's
action-code mode unless a site declares a snapshot.** A versioned lifecycle,
`readmit-siu-lifecycle-v2` in a `readmit-scenario/v2` workflow, adds resource
subjects with exactly two events: S18 adds a resource and S20 cancels its
participation; a replacement is an S20 then an S18. v1 scenarios, lifecycles
and saved steps are unchanged and never converted, and S14 changes no resource.
Chapter 10 requires the segment action code on every updating or modifying
trigger, so S18 sends the added resource with A and S20 the cancelled one with
U and filler status Cancelled, S13 restates every booked resource with U, and
S14 sends none (v2.5.1 §2.10.4.2). The snapshot mode (§2.10.4.1) is written only when
the request declares it. The evaluator's reviewed conditions leave those action
codes undecided, because HL7 never enumerates the modifying triggers; the
generator follows the field definition rather than that gap.

**Delays are execution timing, kept by the connected engine.** A declared
delay is recorded per occurrence in the generation record. Case bytes and
message timestamps never carry it. A scheduled connected lifecycle
(`readmit-connected-test/v6`) names the record and the case by row and variant,
and the existing connected send waits each delay on its open connection before
the message's intent and authority check, bounded by the lifecycle's budget and
cancellable. There is no second executor, and the connected engine sends a
generated case only this way.

## Trade-offs

Gating on the evaluator means many editions report `undecided` rather than
`pass` where the evaluator's own inputs are incomplete (unclassified component
usage, unrepresented conditions, `varies`). That is reported, not hidden.
Keeping Table 0354's names in the generator is a second source for one fact;
cross-checking it against the pack keeps the pack authoritative. A site that
modifies, discontinues or deletes a resource (S19, S21, S22) is not served
until a lifecycle declares those events. Holding the phase's one connection open
through a wait means a receiver that closes idle connections ends the send as
it would any closed connection.
