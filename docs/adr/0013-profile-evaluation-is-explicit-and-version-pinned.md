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
