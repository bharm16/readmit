---
status: accepted
date: 2026-09-26
---

# Evaluate local constraints explicitly without upgrading metadata claims

`internal/profileeval` is the shared evaluation boundary for CLI profile
checks, profile diagnosis and connected prepared-input checks. Existing v1
profile/pack readers remain storage contracts with unchanged canonical bytes.
The explicit evaluator returns `readmit-profile-evaluation/v1`, binding exact
content and operator versions without modifying source evidence or saved pins.

New `readmit-local-profile/v2` and `readmit-profile-pack/v2` envelopes add
executable ordered group and workflow declarations beside the existing typed
local constraints and pinned metadata. New group matching uses bounded
alternatives; unsupported syntax/types, opaque extensions and missing capture
prerequisites cannot become passes. Clinical lifecycle semantics come from a
named, versioned, reviewed interface contract, never an inferred universal rule.

Base, local, overridden and workflow findings remain separate. A local success
beside absent base metadata is not a complete validation pass. Compilation can
retain the new pins without requiring a desktop change; evaluation remains an
explicit Go operation and a separate result, preserving execution-result/v1.

D1's exact upstream pins are normalized offline at development time. The
normalizer executes no upstream code and adds no runtime dependency. Extraction
and successful reader validation do not approve rights or qualify every cell.
No new upstream pack is bundled until the exact content review and independent
qualification are complete. #577 stays open for those gates and unsupported
required rule families, as detailed in docs/profile-evaluation.md.


## 2026-09-27: explicit component metadata and group bindings

The separate v3 profile and pack envelopes carry composite/component rules and
explicit outer-to-inner workflow parent bindings. They select evaluator v2;
existing v1/v2 document pairs retain evaluator v1. Component selection uses the
lossless parser, including custom delimiters, rather than splitting decoded
text. Unknown component usage, table content and deeper-than-wire composite
metadata remain unsupported. No source table is fetched during evaluation.
The owned 28-cell fixture matrix qualifies finite local interfaces only.
Extractor v2 keeps the new upstream output outside distribution pending the
exact-content owner review recorded in docs/profile-redistribution-review.md.

## 2026-09-28: version-matched choice groups

`readmit-profile-pack/v4` adds one node form, a choice whose repetitions each
match exactly one alternative, and selects evaluator v3 with v2's component
semantics. Earlier packs and every local profile refuse the node. Extractor v3
takes a choice only from the same version's HL7apy group declaration, where
nHapi's generated constructors flatten it, and refuses any disagreement between
the two pinned sources. The new outputs are new pack identities under a new
receipt, and remain withheld pending the exact-content review in #627.
