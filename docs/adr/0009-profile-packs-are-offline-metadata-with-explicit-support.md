---
status: accepted
date: 2026-09-18
---

# Profile packs are offline metadata with explicit support levels

Keep Readmit's byte-preserving Go parser. Normalize pinned nHapi and HL7apy
metadata at build time into a shared versioned strict-JSON pack contract, read
by the same Go engine in the desktop and CLI. Metadata sources never become
customer runtime dependencies. Source/version choices and delivery owners are
recorded in [D1](../product-decisions.md#d1--profile-metadata-and-supported-meaning).

Parsing, field labels, structural validation and workflow semantics are distinct
support levels. A pack declares its coverage by HL7 version and message family;
an unsupported combination cannot acquire a passing verdict from another
level. Readmit-authored workflow rules need independent fixtures. A dictionary
does not certify clinical behavior.

## Trade-off

Replacing the tested parser with an upstream object model risks evidence-byte
loss and introduces .NET/Python into the static CLI distribution. Copying prose
or treating upstream metadata as complete conformance authority obscures both
rights and coverage. Offline normalization keeps the proven parser while
allowing metadata updates to have independent versions, provenance and tests.
Each extraction still needs review of the actual content, redistribution
rights, notices and covered source; selecting a source does not grant rights.

## Consequences

The shared pack contract is a separate prerequisite from completing every pack
in #45. Generic editors, correlation and typed operators can use the contract
with independently authored fixtures before the full library is delivered.
Profile-specific acceptance and the seven-version/four-family coverage matrix
remain required. The current v2.5.1 labels keep their documented finite scope.

Existing strict-JSON artifacts keep their member sets. New pack versions use
new contract names and compatible readers as ADR-0003 requires. No external
terminology catalogue is bundled without its own rights review. This ADR
authorizes the architecture; the pack implementation remains tracked work.
