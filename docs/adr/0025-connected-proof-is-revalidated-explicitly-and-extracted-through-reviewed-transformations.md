# ADR 0025: connected proof is revalidated explicitly and extracted through reviewed transformations

Status: accepted. Amends [ADR 0024](0024-connected-proof-is-retained-whole-and-extracted-value-free.md).

## Context

ADR 0024 retained connected lifecycles whole, verified and re-analyzed them
offline and shared them only as a value-free extract. Two parts of IG14 were
left as stated limitations rather than implemented:

- A retained FHIR validation was only reinterpreted from the validator's
  retained output. Reinterpreting a saved report cannot detect an error in the
  original validation run, and IG14 requires re-evaluation where the pinned
  dependencies are installed.
- The only disposition a disclosure policy offered was `exclude`. The resulting
  extract says what was observed ("two records where one was expected") but not
  the reviewed records and requests a reader needs to understand it.

A documented limitation is not a waiver. Both are now implemented, and the
passive and conservative behaviors stay available.

## Decision

### Revalidation is an explicit, separate analysis

- Opening, verifying, exporting or extracting a packet stays passive. None of
  them starts a program, downloads anything or contacts a target.
- `readmit report connected revalidate PACKET --capability INSTALLED` is the
  one operation that starts the validator again. For each retained validation
  it:
  - takes the retained resource bytes and the historical request (profiles,
    requirements, fail severities, timeout, output bound);
  - uses them only when they are the plan's declared validation;
  - runs them with the administrator's installed capability, only when its
    identity is exactly the historical capability pin;
  - runs through ADR 0022's worker: networking disabled, isolation verified
    before start, bounded execution.
- The capability copy retained inside a packet is evidence. It is never
  accepted as the installed capability, and nothing packet-supplied is
  executed.
- The result is a sealed `readmit-connected-revalidation/v1` written beside
  the packet. It records the historical and new outcomes by identity and
  whether they agree, differ, or had no historical outcome to compare. The
  packet and its historical verdict are never changed.
- A validation is reported as not run again, with the reason, when:
  - no capability is installed, or the installed one is a different pin;
  - no validation was retained;
  - the request was not the declared one;
  - the engine is missing, or refuses the isolation;
  - the worker times out, crashes or exceeds its output bound.
- Unavailable terminology is the validator's own undecided state. None of these
  is ever a pass, and no newer validator is substituted.
- Verification of a revalidation needs its packet. It re-derives the historical
  side from the packet, and requires every new result to have run on exactly
  the historical input with the historical request identity.

### Transformed extracts are a new policy and extract version

- `readmit-connected-disclosure-policy/v1` and the value-free
  `readmit-connected-extract/v1` are unchanged and remain the default for a
  reviewer who needs only outcomes.
- `readmit-connected-disclosure-policy/v2` maps every surface to `exclude` or
  `transform`. It can transform only FHIR resources, typed records, the
  identity mapping and HTTP exchanges. Opaque surfaces can only be excluded:
  narrative, extensions, attachments, non-FHIR bodies, v2 messages, validator
  diagnostics, setup resources, authored definitions and lifecycle records.
- Explicit rules keep or pseudonymize FHIR elements by resource type and path,
  keep, pseudonymize or redact typed columns, and keep or pseudonymize query
  parameters. Anything not named is excluded.
- Some content is excluded whole, with the reason stated:
  - a resource carrying a modifier extension, because it cannot be shown
    without it and still mean the same;
  - Binary content;
  - references and request URLs outside the declared servers.
- Pseudonyms are an HMAC under a customer-local key file
  (`readmit report connected pseudonym-key`). Equal originals get equal
  pseudonyms, so these stay consistent:
  - a logical ID in `id`, in every relative or declared-server reference, in
    typed identity columns, in bound identities and in request paths;
  - a business identifier across resources, rows and token searches, which
    keep their system and pseudonymize their code.
- The key, and any original-to-derived mapping, is never written into an
  extract. The same key and policy reproduce the same bytes, so a previewed
  identity can be approved and published later.
- `readmit-connected-extract/v2` is built only from derived material. Its
  Markdown and HTML renderings are generated from the extract document alone,
  and its reader regenerates them.
- The extract is blocked by any of:
  - an unmapped surface;
  - credential material;
  - a residual scan hit: any original value the policy did not keep, found in
    the extract, its decoded strings or a rendering.
- Publication writes only a fresh preparation whose bytes are exactly the
  approved identity; a changed packet, policy or key needs review again.
- The extract's evidence class is `reviewed-transformed-extract`. Every
  transformed exchange, observation and binding states that it is derived and
  names its original by position, never as an original observation, response
  or finding. Its equivalence is always `unverified`: disclosure review is not
  behavioral equivalence, and a pseudonymized case is not a reproduced
  regression without separate actual executions. Nothing here is a legal
  de-identification determination.
- Each extract version's reader refuses the other.

## Consequences

- Original evidence, reviewed transformed extracts and reproduced regressions
  remain three distinct contracts.
- Revalidation is qualified in CI through a stand-in local container engine
  that the installed container command line drives. The real staged validator
  image on a Linux arm64 engine remains the owner's opt-in qualification
  (`READMIT_FHIR_VALIDATOR_CAPABILITY`).
- The redesigned Report and Share views (#589) bind:
  - revalidation, as a separate read-only analysis;
  - the choice between the value-free summary and the transformed extract,
    within #560's existing preview and approval flow.
