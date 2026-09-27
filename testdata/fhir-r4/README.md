# Owned FHIR R4 fixtures

These files contain wholly fictional people, locations, appointments, orders
and results. `urn:owned:*` systems are explicit fixture authorities. No customer
record, real patient identifier, production endpoint or external service is
involved; `.test` addresses and URNs are reference identities only.

The collection's literal facts are deliberately independent of the Go model:

- Patient `p1` at two distinct bases represents two occurrences with different
  identifier systems and versions. The first is inactive, has month-only birth
  precision and an extension-only middle given-name slot.
- Two appointments share one business identifier. Their logical IDs, starts,
  statuses and participant relationships differ and must never be collapsed.
- The order's subject/encounter links, observation's order and contained performer,
  and report's version-specific observation link have explicit expected targets.
- The measured quantity is the exact decimal `9007199254740993.1200`; display
  unit and UCUM code retain their spelling. No unit conversion is expected.
- The capability fixture records positive, false and absent support claims.
  It is not evidence of permission or real server compatibility.

Tests add independent negative/ambiguous resources inline. They do not generate
expectations by calling the parser or resolver being tested. These fixtures
qualify the named projection behavior, not an implementation guide or clinical
workflow. The fixed source contracts are linked in `docs/fhir-r4.md`.
