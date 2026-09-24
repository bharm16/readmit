# Evidence protection

`readmit protect` encrypts the sensitive files, indexes, temporary work and
backup copies readmit produces, using a key that stays in an operating system
credential store or a customer-managed key provider. readmit holds no key
material, exactly as it holds no credential: a **protection control** registers
a reference to a key — the absolute path of the program that prints it, the
locator arguments that select it, the operator's record of rotation, the
retention period packages written under it declare, and the at-rest storage
control the operator declares for the volume the evidence sits on.

readmit must not become the thing that stores key material it just refused to
store for credentials. See
[ADR-0006](adr/0006-credentials-are-referenced-never-stored.md) for why readmit
runs the store's own program instead of linking a platform keyring, and
[credential references](secret.md) for the same rule applied to credentials.

```sh
readmit protect register --protection protection.json --name lab-evidence \
  --storage os-volume-encryption \
  --command /usr/bin/security \
  --argument find-generic-password --argument -w --argument -s readmit-lab-key \
  --max-age 720h --retain 2160h
readmit protect pack --protection protection.json --name lab-evidence \
  --output transfer-2026-09-18 run-2026-09-18 target.json
readmit protect inspect transfer-2026-09-18
readmit protect open --protection protection.json \
  --package transfer-2026-09-18 --output opened-2026-09-18
readmit protect discard transfer-2026-09-18
```

## Commands

| Command | What it does |
| --- | --- |
| `protect register --protection FILE --name NAME` | Registers a control: a declared storage control, a reference to the key that encrypts its packages, and a retention period |
| `protect rotate --protection FILE --name NAME` | Records that the key behind a control was replaced in its own store |
| `protect retire --protection FILE --name NAME` | Stops a control writing new packages; it still opens the packages it wrote |
| `protect show --protection FILE` | Shows every registered control, its declared storage, rotation and retention, with the key masked |
| `protect pack --protection FILE --name NAME --output NEW_DIRECTORY PATH...` | Writes an encrypted transfer package from files and directories, leaving them unchanged |
| `protect open --protection FILE --package DIRECTORY --output NEW_DIRECTORY` | Decrypts a package into a new directory |
| `protect inspect PACKAGE` | Reports what a package declares about itself, without a key |
| `protect discard PACKAGE [--override-retention]` | Unlinks the files a package declares and states what that does not establish |

The [desktop shell](desktop.md#privacy-review-protected-export-and-support-sharing)
reaches these operations from its protection panel: a control is registered as
a structured form, the document is shown with the key masked and the locator
arguments counted, and packages are packed, inspected, opened and discarded
under the control a person selects. Retiring a control there is `protect
retire`, written as the same bytes; the window asks first, then shows the
control retired, offers it for no new package and still opens what it wrote.
The window never renders key material, a rotation is recorded only when the
declared store answers, and every refusal — a missing key, a retired control, a
retained package — is this command's own. Packing a package writes it beside
the evidence; moving it anywhere remains a separate deliberate act.

## The protection document

```json
{
  "schema": "readmit-protection/v1",
  "controls": [
    {
      "name": "lab-evidence",
      "storage": "os-volume-encryption",
      "state": "active",
      "command": "/usr/bin/security",
      "arguments": ["find-generic-password", "-w", "-s", "readmit-lab-key"],
      "generation": 2,
      "rotated_at": "2026-09-18T09:00:00Z",
      "max_age": "720h",
      "retain": "2160h"
    }
  ]
}
```

The document rejects unknown and duplicate members and unknown versions. It is
written deterministically with controls in one canonical order, is owner-only
(`0600`), and is replaced atomically: a new file is written in full and renamed
over the previous one, so a reader never sees a partial document. An interrupted
write is retained beside it and reported, never reused.

`command` is the **absolute path** of a program that prints the key on standard
output, and `arguments` select which key. The program is never looked up on
`PATH`, never run through a shell, and must be a regular file. It is read
through the same bounded mechanism a credential is read through: output bounded
to 64 KiB, the provider's own diagnostics discarded, and a five-second timeout
after which the key is **unavailable** rather than empty. readmit refuses fewer
than 32 bytes of material. That is a length check, not a strength assessment:
readmit cannot measure entropy and does not claim to.

An argument is a locator, never key material: arguments are visible to every
process on the machine. readmit never puts a key there, and never prints the
arguments it was given — `protect show` reports how many there are.

## Declared storage is declared, never verified

`storage` is `os-volume-encryption`, `customer-key` or `none-declared`. It is
the operator's declaration of the at-rest control on the volume that holds the
evidence, **recorded as made**. readmit builds for five `CGO_ENABLED=0` targets
and does not interrogate FileVault, BitLocker, LUKS, a hypervisor's disk
encryption, a network filesystem or a backup system. `protect show` prints the
declaration with the words `(declared, never verified)` beside it, and a
declaration is never reported as a verified property. A control that declares
`none-declared` is a control whose only cryptographic protection is the
encrypted transfer package.

What readmit does apply itself is narrow, and this is all of it:

- Every file readmit writes here is created exclusively and owner-only (`0600`),
  and every directory owner-only (`0700`). On a filesystem without POSIX modes —
  notably Windows — the mode is not an access control, and this list is then one
  item shorter.
- **`protect pack` writes no plaintext temporary copy.** It reads a source and
  writes ciphertext straight into the destination `internal/artifactpath`
  reserved. A package that cannot be completed is removed rather than left
  looking like one. Every file of a package is synced, the descriptor last,
  and the package directory and its entry in the folder holding it are synced
  before it is reported; a package whose directory cannot be synced is written
  in full, kept, and reported as not confirmed against a power loss. This is a statement about `protect pack` and about nothing
  else — see [what this does not cover](#what-this-does-not-cover).
- Content **and the package index** are encrypted. A sensitive index is
  sensitive data, not harmless metadata, so the names and sizes of packed files
  live inside the encrypted index and never in the plaintext descriptor.

### What this does not cover

Recording these explicitly matters more than the list being short.

- **readmit encrypts nothing implicitly.** Case bundles, run and result
  directories, review and report packets, project documents, revisions and the
  desktop shell's local state are written in the clear, exactly as before, and a
  registered control changes none of that. A package is something you ask for.
- **Other commands do stage plaintext in temporary directories.** `report`
  builds its packet in a working directory under the **system** temporary
  directory, which is very often not the volume whose control an operator
  declared here; `redact export` stages derived case evidence under the private
  local-state directory it was given. Both remove their staging directory when
  they finish, with the deletion limits below applying to that removal too.
  `protect` applies no control to either of them, and declaring
  `os-volume-encryption` on one volume says nothing about the volume the system
  temporary directory is on.
- **The descriptor's own members are not covered by the authentication tag.**
  `created_at`, `retain_until`, `generation` and `control` are plaintext beside
  the ciphertext. Altering them cannot make an entry decrypt, but it can change
  what `protect inspect` reports, so a retention state read from a package
  someone else handed you is their claim, not a verified fact.
- **A declared volume control is a sentence in a file.** It is reported with
  `(declared, never verified)` and nothing in readmit reads it to decide
  anything.

## The encrypted transfer package

A package is a new directory. It never rewrites the evidence it was made from:
original evidence is immutable, and encryption produces a derived artifact
beside it ([ADR-0004](adr/0004-derived-evidence-and-generated-export.md)).

```
transfer-2026-09-18/
  transfer.json     the plaintext descriptor
  index.bin         the encrypted index: the packed names and sizes
  e0001.bin         one encrypted file
  e0002.bin
```

```json
{
  "schema": "readmit-transfer/v1",
  "package": "6f1c0ab29d4e7358a0b5c6d7e8f90123",
  "created_at": "2026-09-18T12:00:00Z",
  "control": "lab-evidence",
  "generation": 2,
  "cipher": "aes-256-gcm",
  "derivation": "hkdf-sha256",
  "salt": "...",
  "key_check": "...",
  "retain_until": "2026-12-17T12:00:00Z",
  "index": { "id": "index", "nonce": "...", "bytes": 168, "sha256": "..." },
  "entries": [ { "id": "e0001", "nonce": "...", "bytes": 43, "sha256": "..." } ]
}
```

The descriptor is the only plaintext file. It names the control whose key opens
the package, so a recipient knows which key to reach for, and it holds **no key
material, no packed file name, no packed file size and no content**. Each entry
is AES-256-GCM over the file, under a content key derived with HKDF-SHA-256 from
the referenced key and this package's random salt, with a fresh random nonce.
The authenticated data of every entry binds the contract version, this package's
identifier and that entry's identifier, so an entry cannot be renamed into
another entry's place or moved between two packages written with the same key.

`key_check` is a second value derived from the same key and salt. It lets a
refusal say *the key is wrong* rather than *something failed*, and it gives an
attacker nothing the authentication tag did not already give them. It confirms
that the package was written with the key you supplied. It is **not**
authentication of who wrote the package: a hash is not source authentication,
and neither is this.

`sha256` is an integrity check over the **ciphertext**. It names a truncated or
replaced entry before decryption is attempted. The plaintext is deliberately not
hashed in the descriptor: a plaintext digest would let anyone holding the
package confirm a guess at the content without ever holding the key.

`readmit-transfer/v1` and `readmit-transfer-index/v1` are strict JSON read the
way every other artifact is read ([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)):
unknown members, unknown versions, an unread cipher and an unread derivation are
each refused. Unsupported is not a pass. A change to either is a new version
string with a reader for every older version, never a member added here.

## Key lifecycle

`generation` and `rotated_at` record that the operator replaced the key **in its
own store**, using that store's own tooling. `protect rotate` requires the
declared store to answer for that control before it records anything, so a
generation is never recorded against a key readmit cannot read — but readmit
does not read a previous key and therefore cannot verify that one was replaced.
A recorded rotation is the operator's assertion, not proof.

`max_age` declares how long a recorded rotation stays current. `protect show`
reports `current`, `overdue`, or `not-declared` when no interval is declared. An
undeclared interval is never reported as current: an unknown rotation state is
not a passing one.

Every package records the generation that was current when it was written.
Opening one derives the key check from whatever the control reads **now**:

- The check matches: the package opens.
- The check fails and the generations differ: the refusal says the package
  records an earlier generation than the control now reads.
- The check fails and the generations agree: the refusal says the package was
  not written with the key this control reads.

That is the whole of what readmit can tell you. It never held either key, so it
cannot distinguish a rotated key from an unrelated one by any means other than
the generation the package recorded — and if an operator rotates in the document
without rotating in the store, the old package still opens and readmit has no
way to notice.

**Retirement is not revocation.** `protect retire` stops a control writing new
packages and changes nothing about the packages it already wrote: those still
open, with the same key, for anyone who holds it. readmit cannot destroy a key
it never held, cannot reach a copy of a package someone else already has, and
issues no revocation anyone would consult. Withdrawing access to already-shared
evidence is a key-management action in the operator's own store, followed by the
knowledge that whoever already decrypted a package still has the plaintext.

## Retention

`retain` declares how long packages written under a control are meant to be
kept. Each package records the resulting `retain_until` instant, and `protect
inspect` and `protect discard` report `within-retention`, `past-retention`, or
`not-declared` when no period was declared. An undeclared retention period is
never reported as within retention.

`protect discard` **refuses** a package that is still within its declared
retention period, and names the instant it was declared to, so the state gates
the destructive action rather than being reported once the package is gone.
`--override-retention` discards it anyway, and the output records that the
declared retention was overridden. A package that declares no retention period
is not gated at all: readmit reports `not-declared` rather than implying that an
undeclared period was satisfied.

Beyond that gate, retention is a **record and a report**. readmit deletes
nothing on a schedule, runs no background task, and consults no clock but the
one the command was given — which on the command line is the machine's clock, so
moving it changes the answer. Nothing outside `protect discard` is gated:
retained evidence, project documents and reports are kept or removed by the
operator with their own tooling, and the limits below apply to that too.

## What encryption here does and does not establish

**Does.** The content and the index of a package are unreadable, at rest and in
transit, to someone who does not hold the referenced key. Alteration of a
package — including alteration with a coherently rewritten descriptor — is
refused rather than decrypted. Entries cannot be moved between packages.

**Does not.**

- It is not authentication of **who** wrote a package. Anyone holding the key
  can write one that opens. There is no signature here; the signed-document
  machinery in readmit is for entitlements
  ([ADR-0007](adr/0007-offline-entitlements-are-signed-documents-verified-locally.md)),
  not for evidence.
- It protects nothing from a holder of the key, including a recipient who has
  already opened a package.
- It says nothing about the **sources** it was made from. Those files are still
  on disk, under whatever control the volume has, and `protect pack` never
  alters or removes them.
- It says nothing about the plaintext an open writes. Opening a package ends the
  protection the package carried; the output directory is protected by the
  volume's own control and an owner-only file mode, and by nothing else.
- It is not de-identification. A packed message still holds every identifier it
  held. Removing identifiers is [transformation and export review](redact.md),
  and that workflow states its own limits.
- It establishes no legal status, and no certification of any kind.

## What deleting does and does not do

This is the part most often over-read, so it is stated plainly.

`protect discard` **unlinks** the files a package's descriptor declares and
removes its directory. It refuses a directory holding anything the descriptor
does not declare, before removing anything, so it can only remove what readmit
wrote. It does not overwrite the bytes, and readmit does not offer an overwrite,
because on the storage people actually use an overwrite would be a claim it
cannot keep:

- **A solid-state device** does not overwrite in place. Wear levelling writes
  the new block elsewhere and leaves the old one readable by the controller
  until it is reused. Overwriting a file does not reach it.
- **A copy-on-write filesystem** (APFS, Btrfs, ZFS, ReFS) keeps the previous
  version by design. A snapshot keeps it deliberately, and for as long as the
  snapshot is kept.
- **Backups and replicas** already hold their own copy, on their own schedule,
  under their own retention. Deleting here reaches none of them.
- **A recipient** who received a package still has it. Nothing in readmit can
  reach it, and nothing in readmit can revoke it.
- **The filesystem journal, the page cache and swap** may hold bytes that no
  file-level operation touches.

So: deletion in readmit means *this path no longer resolves to this content on
this machine*. It is not erasure, it is not destruction, and it is not a
retraction. Where content must be genuinely unrecoverable, the control that
works is encryption with a key that is destroyed — and readmit never held the
key, so destroying one is an action in the operator's own store, on every copy
of it, with the same limits applying to the store's own storage.

The same limits apply to every other removal readmit performs: a package that
fails midway through being written is removed, and an output directory that
fails midway through being opened is removed, and neither removal erases
anything either. readmit reports both as removals, never as erasures.

## Limits

- At most 8,192 files, 16 MiB per file and 256 MiB in total per package. Every
  bound is applied to what the tree declares before anything is read, so a
  larger tree is refused and packed in parts rather than partly packed.
- A packed name is recorded as a relative path: a named file under its own base
  name, a named directory under its base name and the paths beneath it. Two
  paths that would be recorded under the same name are refused rather than one
  silently replacing the other.
- Entries that are not regular files — a symbolic link inside a tree, a device,
  a socket — are counted, never opened, so walking a tree cannot leave it, loop
  or block. The count is recorded in the encrypted index and reported by
  `protect open`, so a package never implies it holds more than it does.
- A destination must be new, must be outside retained case, run, result, review
  and report evidence, and is reserved through `internal/artifactpath` like
  every other artifact readmit writes. See [audit hardening](audit-hardening.md).
- Key material is read into memory for the duration of one command. Go's garbage
  collector may copy it, and readmit does not claim to scrub it.

## What is not here

readmit never writes to a key store, never creates, rotates or deletes a stored
key, and never chooses a key for you. It encrypts nothing implicitly: a
protection control applies to the packages you ask for, and readmit does not
silently encrypt case, run, result, review or report evidence in place. Retained
evidence is immutable, so encrypting it is producing a new artifact beside it,
which is exactly what `protect pack` does.

There is no key escrow, no recovery key, no password-derived key and no
interactive prompt. A key that is lost is a package that cannot be opened, and
readmit has nothing to offer in that case, which is the honest consequence of
never holding the key.
