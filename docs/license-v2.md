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

These tickets consume the contract; the local trial and operation guard are implemented below.
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
| Expiry stops new execution and authoring; started bounded runs finish (D6) | The operation guard checks every new job; bounded admitted work finishes and releases capacity | #116 |
| Free read-only reviewers (D6) | A document with `authors.seats: 0` and `runners.instances: 0`, or no entitlement: reading is never gated | — |

## Limits, stated

**A disconnected copy is a second authority.** An admission record is exactly
as authoritative as the file. A copy on another machine, a restored backup or a
snapshot rolled back holds the same capacity again, and the engine cannot tell
one copy from another. The same is true of an entitlement store: restoring one
restores the activation it held. The product makes no claim of globally
reliable enforcement on disconnected copies; what it claims is that one
authority, kept in one place, admits no more than it was granted.

**Pure verification is decided from the supplied clock.** It can report an old term active after rollback. New work uses the D6 operation guard below, with separate versioned local
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

- **Global disconnected enforcement.** Local operation admission cannot detect copied authority records.
- **Vendor deployment.** The administration API is described in
  [commercial administration](commercial-administration.md). The vendor's account ledger and its authenticated payment events
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

## Complete local evaluation and operation admission

New authoring and execution now require an explicitly selected signed v2
entitlement. No policy flag, absent or corrupt activation, a released
activation, an unassigned author/device, missing capability, a superseded issue,
or an ended term refuses **before** work starts. The CLI, desktop, hub writes
and customer runner use `internal/operationguard`; core evidence readers keep
no licensing dependency. v1 verification and its bound-device meaning remain
unchanged; v1 does not grant named-author operation admission.

The three operation capabilities are `author`, `execute` and `hub`. Authoring
checks a signed named-author/device assignment. Execution admits one process instance
against the selected signed runner authority. A suite/queue holds one instance for its bounded lifetime and rechecks term, clock and authority before each new job without charging a slot per test. It releases its instance on ordinary error or
completion, and retains uncertain capacity after interruption for explicit
`license runner reconcile`. A long-running runner checks each new job; the hub
checks each new authenticated write and runner admission. A running bounded job
keeps its already admitted permission through expiry, at most seven days. No
expiry deletes, replays or stops evidence recovery. Reads, verification, exports,
license management, backups and restoration remain free. The frozen desktop
practice, `report --scenario siu-reschedule-v1`, and `sample synth`, `sample
capture` and `sample index` remain ungated; these sample commands accept only
the pinned synthetic bytes/inputs, never arbitrary evidence or targets.

### Explicit customer activation

The issuer supplies the signed entitlement and its public trust document. The
operator writes one private `readmit-operation-policy/v1` file selecting local
absolute paths. All members are required; an unused author/device or
runner-authority/admissions pair is explicitly empty. Files belong outside
retained evidence.

```json
{
  "schema": "readmit-operation-policy/v1",
  "entitlement": "/private/readmit/entitlement.json",
  "trust": "/private/readmit/trust.json",
  "state": "/private/readmit/clock.json",
  "author": "alice",
  "device": "workstation-a",
  "authority": "local-runner",
  "admissions": "/private/readmit/admissions.json"
}
```

```sh
readmit --operation-policy /private/readmit/operation-policy.json license operation activate
readmit --operation-policy /private/readmit/operation-policy.json license operation status
readmit --operation-policy /private/readmit/operation-policy.json test spec.json --send --output new-result
readmit --operation-policy /private/readmit/operation-policy.json license operation resolve
readmit --operation-policy /private/readmit/operation-policy.json license operation release
```

`activate` creates clock state and a runner admission record explicitly, never
changes signed dates and never replaces an existing state. An interrupted
activation may leave an empty runner record before the clock is published. An
explicit activation retry reuses only an empty record naming the same signed
organization and authority. A record with any admission history cannot initialize
a missing clock; it needs explicit retained-state recovery. `release`
irreversibly marks this local operation activation released; a new explicitly
created activation under a valid signed document is a separate record. The
older `license import/release` stores remain separate verification/device
records; releasing an operation uses the command above.

In the desktop privacy pane, either choose a supplied folder containing
`operation-policy.json`, or import without hand-authored JSON: verify the
received entitlement and trust document through native file dialogs, choose the
author, device and runner authority from what the verified document itself
assigns, choose a private activation folder, and let the pane write the
documents and the policy there. The selected path is retained in
`readmit-desktop-operation-selection/v1` outside evidence; creation, selection
and activation are separate actions. The pane also installs later issues
(renewals and the one approved extension, refusing transfers), exports the
installed document byte for byte, and shows and settles the runner authority's
admissions; it shows the UTC high-water and rollback/release state. A missing
policy never prevents opening the application or reading a workspace, and every
license-management action works with no activation at all. Hub identity binding
and startup selection are documented in [the hub guide](../hub/README.md); the
customer-facing commercial portal destination is documented in
[the desktop shell guide](desktop.md#commercial-account-and-checkout-destination-readmit-commercial-destinationsv1).

### Visible time and refusal recovery

`readmit-operation-clock/v1` requires `schema`, `organization`, `sequence`,
`high_water` (whole-second UTC), `rollback` and `released`. It contains no work
identifiers, evidence hashes or machine fingerprints. Admission re-verifies the
selected signed claims and re-reads state under an exclusive update. It retains
the greatest UTC seen and in-process monotonic elapsed time, including elapsed
fractions accumulated across operations. A backward correction of at most five
minutes never reduces effective time. A larger rollback is recorded and latched:
fix wall UTC to at least the retained high-water, then explicitly `resolve`.
A later clock sample alone does not clear that latch.

Missing/corrupt state is not an implicit activation. Concurrent metadata updates allow up to 100 brief, cancellation-aware lock retries before refusing; no execution is retried. An interrupted
`clock.json.incomplete` update is retained and blocks new work; recovery retains
and inspects that file outside Readmit before removing the lock, preserving the
largest known high-water. No recovery command lowers time. A falsely advanced
clock can require waiting or an owner-supported replacement activation; it is
never silently corrected backwards. Renewal must carry an organization sequence
at least as new as the retained state and preserve the organization's signed
assignment. Time advancement is recorded even when it proves the term expired.

The clock is local evidence, not tamper-proof hardware. Deleting/replacing local
files, restoring VM snapshots, or copying authority records can defeat local
history or duplicate disconnected capacity. There is no periodic call-home,
and a revocation is learned only when updated trust or entitlement files arrive.

### Issuer policy and exactly one extension

`internal/trial` is the vendor-side issuer API. It takes authenticated explicit
activation or scheduled offline issuance, configuration, named assignments and
a signing key supplied only in memory. `Issue` returns a strict
`readmit-trial-account/v1` and exact signed `readmit-entitlement/v2` bytes.
The issuer commits the returned account under its organization lock/CAS before
releasing those bytes; retrying delivery reuses the retained bytes. Its sequence
allocator is shared with paid/admin issuance, so purchasing never resets the
organization sequence. Production signer custody, approval authentication and
durable vendor service deployment remain owner responsibilities.

The adopted issuer configuration is a strict `readmit-trial-policy/v1`:

```json
{
  "schema": "readmit-trial-policy/v1",
  "plan": "evaluation",
  "days": 30,
  "extension_days": 14,
  "grace_days": 0,
  "authors": 3,
  "devices_per_author": 2,
  "runners": 1,
  "capabilities": ["author", "execute", "hub"]
}
```

An activation starts at its explicit UTC instant; a scheduled issue names its
future UTC start. Downloading starts nothing. The initial signed end is exactly
30 days later. Three named humans each receive up to two assigned devices,
one runner instance is assigned to the customer authority, and hub operation
is included. Read-only reviewers require no entitlement. No card, automatic
conversion or trial grace is present. The account stores the original end and
one extension decision; an explicitly approved `Extend` signs a later sequence
with the original end plus 14 days, zero grace, and refuses a second extension
or an approval after that fixed extension end. Cancelling an issuer operation
returns no delivery; failed signing changes no account. Trial and paid issuing
remain separate: paid renewal configuration carries `grace_days: 14`, and the
same operation guard honors that signed grace. The engine infers no price or
plan name.

### Running command-line recipes with an activated license

Read-only commands and the frozen walkthrough work directly without this setup.
For a shell workflow that creates or executes other work, select your supplied
policy explicitly. In a POSIX shell, this wrapper keeps existing recipe commands
literal while passing the documented flag on every invocation:

```sh
READMIT_EXE=/absolute/path/to/readmit
READMIT_POLICY=/private/readmit/operation-policy.json
"$READMIT_EXE" --operation-policy "$READMIT_POLICY" license operation activate
readmit() { "$READMIT_EXE" --operation-policy "$READMIT_POLICY" "$@"; }
```

Activate once only; if it is already activated, use `license operation status`
instead. These are shell variables passed as arguments, not environment-variable
configuration discovered by the engine. In PowerShell use
`$ReadmitExe = 'C:/tools/readmit.exe'`, `$ReadmitPolicy = 'C:/private/operation-policy.json'`
and `function readmit { & $ReadmitExe --operation-policy $ReadmitPolicy @args }`.
Services and CI examples pass the flag explicitly and do not use this wrapper.

Existing sealed v1 reports retain their exact historical `RERUN.md` bytes so they
still verify. Use `report prepare` and follow the newly prepared workspace's
instructions with this release; its runnable copy names the explicit activation
flag. Historical direct execution snippets are not an implicit license selection.
For retained customer packets, preserve the original packet, copy/rebind outside
it as documented, and run new sends through the wrapper above. Their read-only
verification and exports remain available without activation.
