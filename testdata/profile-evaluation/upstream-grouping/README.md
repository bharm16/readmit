# Independent source grouping fixtures

These hand-authored minimum message sequences exercise selected source grammar
paths without using the normalized grammar to generate a passing input. Positive
means the named group order/cardinality level only; required fields are omitted
on purpose so the overall evaluator must never mistake this for complete
conformance. Negative removes the relevant required subject/group.

ADT selects MSH/EVN/PID/PV1. SIU selects MSH/SCH/RGS. ORM selects MSH/ORC
without an optional detail group. ORU selects MSH/OBR/OBX. The source matrix
binds the exact files and source pack hashes. Adopted 2.7.1 and 2.8.2 packs lack
ORM_O01; both fixtures in those cells must produce named unsupported results.
These bytes make no claim that an absent source structure exists.
