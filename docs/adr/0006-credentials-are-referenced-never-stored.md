---
status: accepted
date: 2026-09-18
amended: 2026-09-18
---

# Credentials are referenced, never stored, written or rendered by readmit

readmit connects to customer nonproduction systems, so it must know about
credentials. It holds none. A `readmit-secrets/v1` document registers
**references**: which kind of store the operator declared a credential lives in,
the single endpoint address and purpose it may be presented to, the absolute
path of the program that reads it back from that store, the locator arguments
that select it, and the operator's record of when it was last rotated. Nothing
in readmit accepts a credential value as a flag, an argument, a prompt or a file
it writes, and nothing renders one: a value exists only inside the one command
that resolved it, carried by a type that masks itself under every formatting
verb and refuses to be serialized at all.

That is what makes configuration shareable. A target configuration, a project
document, a run manifest, a report and local browser state carry a reference or
carry nothing, because a value was never available to be written into them.

## Amendment: the same rule covers key material

Storage protection needs a key, and a key is the same kind of thing as a
credential: a value readmit must know *about* and must not hold. The rule
therefore extends unchanged. A `readmit-protection/v1` control registers a
**reference** to a key — the absolute path of the program that prints it, the
locator arguments that select it, the operator's record of rotation, the
retention period packages written under it declare, and the at-rest storage
control the operator declares for the volume. The key is read through the very
same bounded mechanism (`secret.Read`, extracted from `secret.Resolve` so there
is one), lives inside a single command as a `secret.Value` that masks itself and
refuses to be serialized, and is never written to any artifact, document or log.
readmit does not become the thing that stores key material it just refused to
store for credentials, and it still has no store of its own to write to.

The encryption itself is AES-256-GCM with HKDF-SHA-256, both from the standard
library, for the reason [ADR-0001](0001-go-single-binary-release-matrix.md)
gives: five `CGO_ENABLED=0` targets and a smoke test that runs the exact
archives with an empty PATH, so any crypto need "must have a pure-Go path or be
dropped". `crypto/ed25519` set this precedent for entitlements in
[ADR-0007](0007-offline-entitlements-are-signed-documents-verified-locally.md).
Cobra remains the only direct third-party dependency of the released executable.

Three limits follow from holding no key, and the documentation states all three
rather than implying a control readmit cannot deliver:

- **Rotation is a record.** readmit never reads a previous key, so it cannot
  verify one was replaced. A package records the generation current when it was
  written, and a failed key check plus a differing generation is the only way
  readmit can name a rotated-away key.
- **Retirement is not revocation**, exactly as it is not for a signing key in
  ADR-0007. A retired control still opens every package it wrote, for anyone
  holding the key, and readmit cannot destroy a key it never held or reach a
  copy someone already has.
- **Deletion is not erasure.** Unlinking a package does not overwrite it on a
  solid-state device, a copy-on-write filesystem, a snapshot, a backup, a
  replica or a recipient's machine. readmit offers no overwrite, because on the
  storage people actually use an overwrite would be a claim it cannot keep.
  Encryption with a key the operator controls is the control that outlives
  deletion's limits, and it too has an end: a recipient who has already opened a
  package holds the plaintext.

The declared storage control is recorded as made and never verified, for the
same reason `store` is: readmit runs on five static targets and cannot
interrogate FileVault, BitLocker, LUKS, a hypervisor or a backup system, and a
declaration readmit cannot check is a declaration, not a property.

`readmit-secrets/v1` gains no member and changes no byte. A protection control
is a separate contract, because a key is bound to evidence at rest rather than
to one endpoint address, and widening `purpose` would have changed the meaning
of an existing version rather than adding a new one.

## Why a resolution program rather than a keyring binding

[ADR-0001](0001-go-single-binary-release-matrix.md) requires five
`CGO_ENABLED=0` targets and says any storage or crypto need "must have a pure-Go
path or be dropped"; `tools/smoke.py` runs the exact archives with an empty
PATH. Nearly every macOS Keychain, libsecret and Windows DPAPI binding needs
cgo, so a linked keyring would either break the static build or restrict the
delivery to the desktop module, where the command line could not use it.

readmit therefore runs the program the operator declared and reads the
credential from its standard output. One mechanism covers both halves of the
requirement: `/usr/bin/security find-generic-password -w` or
`secret-tool lookup` is an operating system credential store, and a customer
secret manager's own client is a customer-managed provider. Nothing about the
release build changes, no dependency is added, and Cobra remains the only direct
third-party dependency of the released executable.

The program is named by **absolute path** and is never looked up on PATH, so the
empty-PATH smoke tests are unaffected and no PATH entry can substitute another
program for the one the operator chose. `store` records which kind of storage
the operator declared; it is a declaration recorded as made, never a verified
property, because readmit cannot establish what answered it.

## Considered options

- **A pure-Go reimplementation of each platform credential store.** Rejected.
  Keychain, libsecret and DPAPI are security interfaces; reimplementing their
  wire formats and unlock semantics in three ways is exactly the two-implementations
  failure this product exists to find, and a mistake there loses a customer's
  credential rather than a test result.
- **A cgo keyring confined to the `desktop` module.** Permitted by
  [ADR-0005](0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md),
  but it would give the desktop shell a capability the command line does not
  have, so the same configuration would work in one entry point and not the
  other. Both entry points call the same internal packages; a credential path
  that only one can take breaks that.
- **readmit storing credentials itself, encrypted at rest.** Rejected. It puts
  readmit on the critical path of a customer's credential lifecycle, requires a
  key-management decision this product has no business making, and contradicts
  the area requirement to keep secrets in the customer's own store.
- **Writing credentials into the store as well as reading them.** Rejected for
  this release. Read access is the least privilege that does the job: readmit
  cannot overwrite or destroy a customer's credential, and a rotation performed
  with the store's own tooling is recorded here rather than driven from here.
  The cost is that a recorded rotation is the operator's assertion; readmit does
  not read a previous value and so cannot verify one was replaced, and the docs
  say exactly that.
- **Reading a credential from a terminal with echo disabled.** Rejected. It
  needs `golang.org/x/term`, a second direct dependency of the released
  executable, to gain an input path this design deliberately does not want.
  There is no way to type a credential into readmit, which is stronger than
  masking one.

## Consequences

- `docs/stack.md` no longer lists secret storage as absent. Access control is
  still the operating-system account and filesystem permissions; credentials are
  now referenced from an OS or customer-managed store rather than being
  "explicitly configured network credentials" in a configuration file. No
  database, ORM, hosted backend or application authentication system is added.
- `readmit-target/v2` adds the credential reference. `readmit-target/v1` is read
  unchanged and is still the version every target readmit itself generates; a v1
  target that declares a credential is refused rather than read as though v1 had
  always allowed it, and neither version is migrated into the other. No member
  was added to any released version.
- `readmit-run/v1` is unchanged. A run records the transport it used and names
  no credential reference, so this ticket changed no evidence contract and no
  run identity. Recording the reference in run evidence is a future contract
  change, not an addition to v1.
- A credential is bound to exactly one purpose and one endpoint address, matched
  byte for byte against the address actually configured. Nothing resolves a name
  to decide whether a credential applies, so no DNS answer can widen the scope
  of a stored credential. A reference for another purpose is a different
  reference, not an edit.
- Rotation is a recorded generation and time, checked against a declared maximum
  age. A reference that declares no interval reports `not-declared`, never
  `current`: an unknown rotation state is not a passing one. `secret rotate`
  records nothing unless the declared store answers for that reference.
- `secret scan` resolves the registered credentials and checks named files and
  directories, including the secret reference document itself, for those exact
  bytes, their JSON escapes and their standalone base64 encodings. It reuses the
  residual check of `internal/exportreview` rather than adding a second one.
  Following [ADR-0004](0004-derived-evidence-and-generated-export.md), a clean
  scan is one check on known values: it is not an assessment that no credential
  exists anywhere, and a credential it was never told about is not covered. A
  credential that cannot be resolved is an error, never a clean scan.
- Executing an operator-declared program is deliberate and bounded: absolute
  path only, a regular file, no PATH lookup, no shell, the credential never
  passed as an argument, the provider's own diagnostics discarded so a noisy
  failure cannot write a value into readmit's output, bounded output, and a
  five-second timeout after which the credential is unavailable rather than
  empty. The secret reference document is an explicitly selected local file, and
  is as trusted as any file whose path a person types.
