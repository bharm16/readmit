# Independent offline validator fixtures

These wholly fictional resources and profiles are authored separately from the
worker and its output. `expectations.json` states the oracle before execution.
They contain no customer data and establish no clinical workflow correctness.

The synthetic package requires exact R4 core 4.0.1. It adds a Patient name minimum
and an `active = true` invariant, a complete two-code synthetic CodeSystem and
required ValueSet binding, an intentionally missing ValueSet and an intentionally
unsupported invariant. JSON syntax success is not profile conformance.

The valid/invalid code pair may claim conformance/nonconformance only if the
actual official worker evaluates its local terminology. Otherwise it must remain
explicitly unavailable; disabling terminology cannot make either case pass.
The URL/path probes are hostile input data, never instructions or permission to
fetch, launch a program, or read outside the private work directory.

Synthetic fixture definitions are Readmit-owned qualification content; the package
marks them UNLICENSED rather than asserting a new redistribution grant. They are not the
withheld v2 profile packs and do not include external terminology catalogues.

`outcomes/` holds the actual OperationOutcome the pinned worker produced for
each case. It is written by the opt-in live qualification and read by the
offline policy test. `qualification/` holds the live and containment receipts
for the qualified image. Rerecord both together after rebuilding the image; do
not edit them by hand.
