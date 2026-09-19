# Named authors and active runners: readmit-entitlement/v2

`readmit-entitlement/v1` counts bound devices. It cannot say which human a
device belongs to, how many devices one human may work from, or how many
execution instances may run at once, and the adopted
[D6–D8 policies](product-decisions.md#d6--evaluation-and-clock-policy) need all
three. `readmit-entitlement/v2` says them explicitly. It is a second contract
beside v1, not a revision of it: v1 documents, signatures, readers and the
device-count meaning are unchanged, and a v1 seat count is never doubled to
stand in for two devices per author.

Everything [offline organization entitlements](license.md) says about v1 holds
here too. Verification is local and total, the trust store is the same file,
Ed25519 is the only algorithm, evidence is never gated, nothing is read from
the machine and nothing is sent anywhere.

```sh
readmit license verify entitlement.json --trust vendor-keys.json --author a.nguyen --device ws-0413
readmit license import entitlement.json --trust vendor-keys.json \
  --author a.nguyen --device ws-0413 --output ~/.readmit/entitlement
readmit license runner init entitlement.json --trust vendor-keys.json \
  --authority ci-pool-main --output /srv/readmit/ci-pool-main.json
readmit license runner admit /srv/readmit/ci-pool-main.json entitlement.json \
  --trust vendor-keys.json --instance build-4821 --lease 2h
readmit license runner release /srv/readmit/ci-pool-main.json --instance build-4821
```

## The document: readmit-entitlement/v2

```json
{
  "schema": "readmit-entitlement/v2",
  "entitlement": {
    "id": "ENT-0002",
    "organization": "example-hospital",
    "plan": "example-plan",
    "sequence": 1,
    "issued": "2026-09-18T00:00:00Z",
    "not_before": "2026-09-18T00:00:00Z",
    "expires": "2027-09-18T00:00:00Z",
    "grace_days": 14,
    "authors": {
      "seats": 3,
      "devices_per_seat": 2,
      "assignments": [
        {"author": "a.nguyen", "devices": ["lt-0091", "ws-0413"]},
        {"author": "b.okafor", "devices": ["ws-0512"]}
      ]
    },
    "runners": {
      "instances": 1,
      "authorities": [
        {"id": "ci-pool-main", "instances": 1}
      ]
    },
    "capabilities": ["replay", "synth"]
  },
  "signature": {
    "key_id": "vendor-2026a",
    "algorithm": "ed25519",
    "value": "<base64 ed25519 signature>"
  }
}
```

`id`, `organization`, `plan`, `sequence`, `issued`, `not_before`, `expires`,
`grace_days` and `capabilities` mean exactly what they mean in v1. The two new
members replace v1's `scope`:

| Member | Meaning |
| --- | --- |
| `authors.seats` | The purchased author seats. A seat is a named human |
| `authors.devices_per_seat` | How many devices the issuer lets one author work from at once: `1` or `2`. The contract bounds it at two; the document still carries the number so a verifier reports what was signed |
| `authors.assignments` | The named authors, each with the devices assigned to them. At most `seats` authors; at most `devices_per_seat` devices each; unique and sorted |
| `runners.instances` | The purchased runner capacity, counted in execution instances active at once — never tests, messages or hosts |
| `runners.authorities` | The customer-controlled authorities that admit instances, each with the share of `instances` it holds. The shares sum to at most `instances` |

An author identifier and an authority identifier are names the organization
chooses and the vendor signs, exactly as a device identifier is. Two authors
may share a device; each holds their own seat. An author may be named with no
devices yet, so a seat can be bought before it is assigned. A document may
grant authors and no runners, or runners and no authors.

The signature covers `readmit-entitlement/v2`, a newline, and the deterministic
encoding of the claims in the order above. The prefix differs from v1's, so a
signature made under either contract cannot be replayed under the other.
`readmit license verify` reads the declared version and applies that version's
reader; a v1 document relabelled as v2 fails the v2 reader's strict decode, and
a version neither reads is refused as unsupported.

### Issuer vectors

`internal/entitlement/testdata/vectors/` holds `entitlement-v1.json`,
`entitlement-v2.json` and `trust.json`: the documents above, signed with a
test-only seed by applying `crypto/ed25519` directly to the hand-authored
signing input rather than through readmit's signer. The tests recompute that
signature the same way, independently of the package, and assert that
readmit's own signer and encoder reproduce each vector byte for byte, so an
issuer implemented elsewhere can check itself against the same bytes. The seed
is a literal in the tests and is not a signing identity; no production key
exists in this repository.

## Activation: readmit-entitlement-store/v2

`license import` of a v2 document takes `--author` and `--device` together and
refuses the pair unless the document assigns that device to that author. The
store directory is the same shape as a v1 store, with `activation.json` under
`readmit-entitlement-store/v2` recording the author, the device, the import
time and any release. `show`, `renew`, `export` and `release` read whichever
version a store holds; neither version is migrated into the other, and
`readmit-entitlement-store/v1` gains no member.

`license renew` installs a later issue that still assigns this device to this
author. A reissue that moves the author to other devices, or names other
authors, is a **transfer**: it is refused as a renewal here and imported where
it now applies.

## Reissue and transfer

Assignment is administrator-managed and signed, so every change is a reissue at
the next `sequence`, as in v1:

1. An administrator asks the vendor for a reissue naming the new assignment —
   an author's replacement laptop, a seat handed to a new hire, an authority
   given more instances.
2. `readmit license release STORE` on any device the reissue no longer assigns.
   The activation is recorded as released and grants nothing afterwards.
3. `readmit license import` the reissue on each device it newly assigns, and
   `license renew` it on devices it still assigns.

Releasing is a local record, not a proof to the vendor: restoring a copy of a
store restores the activation it held. Seat accounting is settled by the
issuer's reissue, not by that file.

## Runner admission: readmit-runner-admission/v1

Runner capacity is enforced by a **customer-controlled authority**: one local
file the organization keeps where it can reach it from the hosts that run
work, named in the entitlement, with a share of the purchased instances. There
is no vendor lease service, no call-home, no hardware binding and no daemon.

```json
{
  "schema": "readmit-runner-admission/v1",
  "organization": "example-hospital",
  "authority": "ci-pool-main",
  "admissions": [
    {"instance": "build-4820", "admitted": "2027-03-01T09:00:00Z", "lease_until": "2027-03-01T10:00:00Z", "released": "2027-03-01T09:41:12Z"},
    {"instance": "build-4821", "admitted": "2027-03-01T09:30:00Z", "lease_until": "2027-03-01T11:30:00Z"}
  ]
}
```

| Command | What it does |
| --- | --- |
| `license runner init ENTITLEMENT --trust T --authority ID --output NEW_FILE` | Starts an empty record for an authority the entitlement names |
| `license runner admit RECORD ENTITLEMENT --trust T --instance ID --lease DURATION` | Admits one instance if the authority has a free instance |
| `license runner renew RECORD ENTITLEMENT --trust T --instance ID --lease DURATION` | Extends an admission's lease inside the term; a stale instance reporting in becomes active again |
| `license runner release RECORD --instance ID` | Records that the instance finished or was cancelled |
| `license runner reconcile RECORD --instance ID` | Records that an operator established the instance is gone |
| `license runner show RECORD ENTITLEMENT --trust T` | Reports what is held against what is granted |

An admission is in exactly one state at an instant:

| State | When | Holds capacity |
| --- | --- | --- |
| `active` | Admitted, not settled, lease not ended | Yes |
| `stale` | Lease ended without a release | **Yes** |
| `released` | The instance handed its admission back | No |
| `reconciled` | An operator settled it for the instance | No |

The rule the record exists to hold: **capacity held by an interrupted instance
is not reused until someone settles it.** A host that crashes, a job a
scheduler killed, a network partition that outlived the lease — each leaves an
admission stale, and `admit` refuses with `runner capacity is held by an
admission whose lease ended without release; reconcile it before admitting
another instance` rather than guessing the instance is gone. Two things clear
it: the instance itself renewing, which resolves the uncertainty its silence
created, or an operator reconciling, which records the decision that it
stopped. readmit checks neither; it cannot see the instance, so it records what
it was told and says who told it.

The lease is the instance's declaration of how long it may go quiet: at least
one second, at most seven days, and renewed as often as the instance needs
while the term lasts. An instance that runs longer than its lease and does not
renew becomes stale while it is still running; the same rule that surfaces a
crash surfaces that, and renewal clears it.

Admission and renewal follow the term. Both are refused once the grace window
closes, and admission is refused before `not_before` as well, so a runner that
expires stops taking new work; grace admits and renews exactly as the active
state does. An admission made inside the term keeps the lease it already holds
after expiry — at most seven days — and is still released and reconciled, so
already-started bounded work finishes and its capacity is settled honestly
rather than extended without end.

Every decision is made against the record on disk under one exclusive update.
The replacement file is created exclusively first, so two processes admitting
at once cannot both read the same free instance; the record is then re-read,
the decision made, and the result renamed into place. A refused decision leaves
the file untouched. An interrupted update leaves `<record>.incomplete` behind,
which is retained and reported rather than overwritten, and recovery is moving
it aside outside readmit, as it is for an entitlement store. A record is bound
to its organization and authority: an entitlement of another organization, or
a reissue that dropped the authority, is refused by name.

A record retains every admission it has made, bounded at 1024. Past the bound a
new record is started beside it once nothing in the old one is held.

## Policy mapping for #116, #118 and #119

These tickets consume the contract; none of their policy is implemented here.
The members below are where each adopted decision is carried, so the engine
stays free of plan and price constants.

| Adopted policy | Carried by | Consumer |
| --- | --- | --- |
| 30-day evaluation with three named authors and one runner (D6) | `not_before`/`expires` 30 days apart, `authors.seats: 3`, `authors.devices_per_seat: 2`, `runners.instances: 1`, `plan` labelled by the issuer | #116 |
| One approved 14-day signed extension; no automatic trial grace (D6) | A reissue at the next `sequence` with `expires` moved and `grace_days: 0` | #116 |
| Paid renewals receive 14 days of grace (D6, D7) | `grace_days: 14` on paid issues | #118 |
| Author-seat and runner-capacity catalogue items, annual term (D7) | `authors.seats`, `runners.instances`, `not_before`/`expires` | #118 |
| Renewal, cancellation, downgrade, upgrade (D7) | Each is the next organization-scoped `sequence`; cancellation issues nothing and the paid-through term stands | #118 |
| Two active author devices per seat, administrator-managed transfer (D8) | `authors.devices_per_seat: 2` and `authors.assignments`; transfer is a reissue plus `license release` | #119 |
| Runner capacity counts active execution instances (D8) | `runners.instances`, `runners.authorities`, and the admission record | #119 |
| Expiry stops new execution and authoring; started bounded runs finish (D6) | `license runner admit` refuses when expired; `renew`, `release` and `reconcile` do not. Which commands ask `Allows` is #116's | #116 |
| Free read-only reviewers (D6) | A document with `authors.seats: 0` and `runners.instances: 0`, or no entitlement: reading is never gated | — |

## Limits, stated

**A disconnected copy is a second authority.** An admission record is exactly
as authoritative as the file. A copy on another machine, a restored backup or a
snapshot rolled back holds the same capacity again, and the engine cannot tell
one copy from another. The same is true of an entitlement store: restoring one
restores the activation it held. The product makes no claim of globally
reliable enforcement on disconnected copies; what it claims is that one
authority, kept in one place, admits no more than it was granted.

**The term is decided from this machine's clock.** As in v1, a clock set
backwards revives an expired entitlement and readmits instances. **The clock
guard selected in D6 is not delivered here.** It is separate versioned local
state and an operation-admission rule owned by #116 — visible UTC high-water
state, in-process monotonic time, a tolerated five-minute correction, explicit
resolution of a larger rollback, and defined handling of missing or corrupt
guard state — and it changes no v1 or v2 signature. Reading existing evidence
never acquires it.

**Revocation reaches a machine when a file does.** A verifier that never
contacts the vendor cannot learn that a licence was revoked after signing.
Revocation arrives as an updated trust store or a replacement document, and not
before. A VM snapshot restored to before an expiry, or before a release, is
outside what a fully offline check can defeat, and the product says so rather
than adding a call-home to try.

**Nothing about the machine or the work is in either document.** Author,
device, authority and instance identifiers are names the organization chose.
No hardware fingerprint, serial number, MAC address or hostname is read, and
no case title, endpoint, patient identifier or evidence hash is a member of any
contract here. The admission record holds instance identifiers and instants,
and nothing an instance did.

## Not supported in this release

- **Gating any command.** `license runner admit` is the one refusal added, and
  it is invoked explicitly by the runner integration, not by `test`, `replay`
  or any command that reads evidence. Which capabilities gate which commands is
  #116's decision.
- **The trial clock guard and the administration interface.** #116 and #119
  respectively. The vendor's account ledger and its authenticated payment events
  are [purchasing through a separate portal](billing.md); the portal itself,
  prices and real invoices stay outside this repository.
- **Issuing.** No command signs a v2 entitlement; the vectors are signed with a
  test-only seed.
- **Counting across authorities.** Each authority sees its own record. The
  issuer divides capacity among authorities in the document; the engine does
  not sum across files.
- **Migrating a v1 store or document into v2.** A v1 store keeps working under
  v1 and reports a v2 document as unsupported for renewal; moving an
  organization to named authors is a fresh `import` of a v2 issue.
