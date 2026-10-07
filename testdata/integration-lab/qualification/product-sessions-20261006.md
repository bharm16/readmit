# Production saved-test qualification against owned OIE/HAPI sessions

On October 6, 2026 (America/Chicago), a production Readmit CLI ran a saved
connected SIU booking/rescheduling test against actual OIE 4.6.0 and HAPI FHIR
JPA 8.6.0/R4 4.0.1 with PostgreSQL 16.11. The host was macOS 27.0.1 arm64;
the pinned targets ran on Docker's Linux/aarch64 platform. This qualifies the
local CLI/reference-target boundary. It does not qualify native Desktop use,
packaged customer runners, Windows/amd64, a customer EHR or the remaining
acceptance in #589/#593.

## Product results

| Target revision | Product state / verdict | Independent HAPI Appointments | Transport |
| --- | --- | --- | --- |
| Defective `lab-083128530cfb6997126b0f61` | complete / fail | 2 | AA for booking and rescheduling |
| Corrected `lab-09cda4315467a28140cabef5` | complete / pass | 1 | AA for booking and rescheduling |
| Reintroduced `lab-89fc87a07ab64c9be257138d` | complete / fail | 2 | AA for booking and rescheduling |

Each run invoked the production executable's public `connected prepare`,
`test` preflight and `test --send` commands over a saved v5 definition. No lab
stimulus client sent these messages. The controller prepared only the declared
Patient, Practitioner and Location; it verified an empty Appointment baseline.
The independent read-only observer acquired actual HAPI responses. Both bad
revisions failed the duplicate-specific row-count and unique-business-key
checks, while both positive ACK checks passed. The corrected revision passed
all checks. Each phase used the same 30-second full observation horizon.

The independently compared typed assertion arrays were identical in all three
runs. Wire assertion arrays were also identical after removing only their
original-source occurrence reference, which changes with the generation's
source bytes. Their aggregate oracle SHA-256 was
`1d6b68b6d121eb4dc4069ca82da479d58ac34d5952aabca38166fb431a1ddfb5`.
`session/product-cycle.json` retains per-phase fingerprints and the comparison.

The exact production executable SHA-256 was
`8deaf40fa482e8a7d69ff8608f2844018dff40d0f455feb340337ba3241edb3f`.
All three runs used the same Go 1.27.1 darwin/arm64 executable built from clean
implementation commit `050054da56a65efc520b6442e0cd835d57de3085`.
Subsequent review fixes concern the controller's interrupted-start export and
host tunnel shutdown fence, plus tests/documentation/evidence; they do not
change the production Go executable. Every run retains its actual embedded
build information. The exact binary and original private debug logs were kept
outside Git in the task-owned local qualification directory.

## Retained acquisitions

The [compressed acquisition package](product-sessions-20261006.tar.gz) is
7,037,217 bytes (SHA-256
`d597bab87f43e109ab760b2739df6adf59e99428d0ae8ca22a1d0f77785f0ac3`).
It uses ordinary tar hardlinks for identical acquired bytes, retaining every
original logical file and manifest while avoiding repeated CapabilityStatements.
It contains two directories:

- `session/`: byte-manifested generation records, actual channel exports,
  runtime/image/config hashes, connection/protocol/CA identity, original source
  bytes and generation substitution provenance, prerequisite receipts, full
  immutable product runs, actual SIU/ACK bytes, all observed HTTP responses and
  intervals, independent HAPI witnesses, assertion fingerprints, scoped reset
  receipts, listener shutdown receipt and verified container/volume/network
  teardown. `product-development` and `development-attempts` preserve the
  separately identified development evidence.
- `reference/`: a fresh run of the original 24-revision, three-boundary reference
  qualifier, with its acquired inputs/ACKs/resources, independent oracle,
  completion-bound hashes and verified teardown.

The package contains no credential providers, operator policy, private keys,
SMART tokens or database passwords. Session and product exporters checked the
actual generated secrets before publication. Public certificates or their
hashes are retained as trust identity. All clinical inputs are the repository's
independently authored, wholly fictional lab fixtures.

The operator stopped and reset the final generation, verified the exact owned
resources empty, stopped its loopback tunnels, and removed/verified only that
Compose project's containers, volumes and network. Completed product evidence
was preserved. The production CLI reopened all three retained results after
that target teardown; its offline exit/verdict receipts are in
`session/offline-after-teardown.json`.

The fresh original reference run passed all three boundaries in all 24
revisions, including security checks, delayed defects, scoped resets and
verified teardown. Its completion-bound acquisition hashes were rechecked
before export and by the default offline test.

## Earlier failures and verification

The earlier development attempts are not relabelled as completed product runs.
They refused before sending for three concrete configuration problems: an
Environment/target name mismatch, an HTTP budget smaller than HAPI's actual
2.1 MiB CapabilityStatement, and HAPI Bundle self-links naming its internal
HTTP origin. The driver corrected its authored binding and explicit budget;
the target now uses HAPI's pinned Apache proxy address strategy with allowlisted
forwarded host information. Response bodies were never rewritten. Original
command/error receipts remain under `session/development-attempts` and the
original private working logs remain outside Git.

`tools/test_retained_lab_sessions.py` opens the archive by default without
Docker, verifies every session/product member, compares the unchanged oracles,
checks original sent SIU bytes and acquired AA ACKs, independently checks the
actual duplicate/one/duplicate HAPI resources, and invokes the original
24-revision acquisition-integrity verifier. The live lab remains opt-in;
required PR checks and broader product acceptance scope are unchanged.
