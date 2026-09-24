# Credential references

readmit holds no credentials. A `readmit-secrets/v1` document registers
**references** to credentials that stay in an operating system credential store
or a customer-managed secret provider: what a credential is for, what it may be
presented to, how to read it back from its store, and when the operator last
rotated it. There is no flag, argument, prompt or file through which a
credential value enters readmit, and nothing renders one.

That is what makes a configuration shareable. A target configuration, a project
document, a run manifest, a report and local browser state carry a reference or
carry nothing, because a value was never available to be written into them. See
[ADR-0006](adr/0006-credentials-are-referenced-never-stored.md) for why readmit
runs the store's own program instead of linking a platform keyring.

```sh
readmit secret add --secrets secrets.json --name lab-mllp \
  --store os-keychain --address 10.4.12.7:2575 \
  --command /usr/bin/security \
  --argument find-generic-password --argument -w --argument -s --argument readmit-lab \
  --max-age 720h
readmit secret show --secrets secrets.json
readmit secret rotate --secrets secrets.json --name lab-mllp
readmit secret scan --secrets secrets.json target.json run-2026-09-18
```

## Commands

| Command | What it does |
| --- | --- |
| `secret add --secrets FILE --name NAME` | Registers a reference to a credential held in a store, creating the document if it is not there yet |
| `secret update --secrets FILE --name NAME` | Changes where a registered reference reads its credential from, or what it may be presented to |
| `secret rotate --secrets FILE --name NAME` | Records that the credential behind a reference was replaced in its store |
| `secret show --secrets FILE` | Shows every registered reference, its scope and its rotation state, with the credential masked |
| `secret scan --secrets FILE PATH...` | Checks configuration, manifests, reports, logs and local browser state for a known credential value |

## The document

```json
{
  "schema": "readmit-secrets/v1",
  "references": [
    {
      "name": "lab-mllp",
      "store": "os-keychain",
      "purpose": "mllp-endpoint",
      "address": "10.4.12.7:2575",
      "command": "/usr/bin/security",
      "arguments": ["find-generic-password", "-w", "-s", "readmit-lab"],
      "generation": 3,
      "rotated_at": "2026-09-18T09:00:00Z",
      "max_age": "720h"
    }
  ]
}
```

The document rejects unknown and duplicate members and unknown versions. It is
written deterministically with references in one canonical order, is owner-only
(`0600`), and is replaced atomically: a new file is written in full and renamed
over the previous one, so a reader never sees a partial document, and the folder
holding it is synced before the command reports it written, so a power loss
cannot undo a replacement readmit reported. An interrupted write is retained
beside it and reported, never reused.

`store` is `os-keychain` or `customer-managed`. It is the operator's declaration
of where the credential lives, recorded as made. readmit runs the program it was
given and cannot establish what answered, so the declaration is never proof.

`purpose` is the single use a reference may be bound to. This release knows two,
`mllp-endpoint` for a credential presented to an MLLP endpoint and
`source-endpoint` for one a read-only transfer program is given to reach an
approved [evidence source](source.md), and `secret add --purpose` records which.
An unknown purpose in a document is refused rather than treated as any other one,
and binding a reference demands the purpose the caller needs, so a reference
registered for one is refused wherever the other is needed and neither is ever
silently widened into the other.

`command` is the **absolute path** of a program that prints the credential on
standard output, and `arguments` select which credential. The program is never
looked up on `PATH`, never run through a shell, and must be a regular file. On
macOS that is typically `/usr/bin/security`, on Linux `secret-tool`, and for a
customer-managed provider it is that provider's own client.

An argument is a locator, never a credential: arguments are visible to every
process on the machine. readmit never puts a value there, and never prints the
arguments it was given back to you — `secret show` reports how many there are.
If one does contain a credential, `secret scan` finds it, because the secret
reference document is always one of the files a scan checks.

## Scope

A reference names exactly one purpose and one endpoint address. Binding a
credential compares that address byte for byte with the address actually
configured. Nothing resolves a name to decide whether a credential applies, so
no DNS answer widens the scope of a stored credential, and a credential
registered for one endpoint is refused rather than presented to another.

A credential for a different purpose is a different reference. `secret update`
can change the store, the address, the locator and the rotation interval; it
cannot change the name or the purpose, so an existing credential's reach is
never widened by an edit.

## Rotation

`generation` and `rotated_at` record that the operator replaced the credential
**in its store**, using that store's own tooling. readmit holds read access to a
credential and nothing more: it never writes to a store and never overwrites a
customer's credential. `secret rotate` requires the declared store to answer for
that reference before it records anything, so a generation is never recorded
against a credential readmit cannot read — but readmit does not read the
previous value and therefore cannot verify that one was replaced. A recorded
rotation is the operator's assertion, not proof.

`max_age` declares how long a recorded rotation stays current. `secret show`
reports `current`, `overdue`, or `not-declared` when no interval is declared.
An undeclared interval is never reported as current: an unknown rotation state
is not a passing one.

## Masked editing

A reference is the editable object. The credential is masked as `********`
everywhere a value would otherwise appear, and the mask is a fixed string whose
length says nothing about any value. This is stronger than hiding a rendered
value: there is no path by which a credential reaches a rendering.

- No command accepts a credential as a flag or an argument.
- A resolved credential is carried by a type that masks itself under every
  formatting verb, including `%x` and `%#v`, and refuses to be serialized to
  JSON at all, so it cannot reach an artifact by being printed or marshalled.
- A credential is resolved only by `secret rotate`, to check that the store
  answers, and by `secret scan`, to have something to check for. Both discard it.
- Diagnostics never repeat a credential, a locator argument or a provider's own
  error output: the store's standard error is discarded, so a provider that
  fails noisily cannot write a value into readmit's output.

## Reading a credential

Resolution runs the declared program directly, with the locator arguments,
inheriting the environment so a customer provider finds its own configuration.
Output is bounded to 64 KiB and one trailing line ending is removed. A program
that exits nonzero, produces nothing, or has not answered within five seconds
leaves the credential **unavailable** — never empty, and never a pass.

## Checking for leakage

`secret scan` resolves the registered credentials and checks every named file,
and every regular file under every named directory, for those exact bytes, their
JSON escapes and their standalone base64 encodings. The secret reference
document itself is always checked.

Every path you name is resolved, following symbolic links, so naming a link
checks what it points at: that is the path you asked about. An entry found
**beneath** a named directory is never followed. A symbolic link inside the
tree, and anything that is not a regular file, is counted as an entry the scan
did not read rather than being opened, so walking a tree cannot leave it, loop,
or block on a device. Every report states how many entries it did not read, so a
scan never implies it inspected more than it did.

Point it at what you are about to share or keep: a target configuration, a run
or result directory, a report packet, a log file, a project directory, and the
desktop shell's local state.

```sh
readmit secret scan --secrets secrets.json target.json run-2026-09-18 report-packet
```

| Exit status | Meaning |
| --- | --- |
| `0` | No checked location held a known credential value |
| `1` | At least one did; the locations are listed, never the value |
| `2` | The scan could not run — an unreadable path, or a credential that did not resolve |

A credential that cannot be resolved is status `2`, never a clean scan: an
unavailable check does not pass.

### Limits

This is one check on known values, exactly as the
[export review](redact.md) residual check is, and it reuses that
implementation. It states its own limits in every report:

- Only the credentials registered in the document it was given are checked. A
  credential it was never told about is not covered.
- Only exact bytes, JSON escapes and standalone base64 are matched. Another
  encoding, a truncation, an encrypted copy, or a value transformed on the way
  into a file is not detected.
- A clean scan says that these known values were not found in these files at
  this moment. It is not an assessment that a file is safe to share, and it
  establishes no legal status.
- Files are read in bounded amounts: at most 8,192 files, 16 MiB per file and
  256 MiB in total. Every bound is applied to what the tree declares before
  anything is read, so a larger tree is refused and checked in parts rather than
  partly read and reported clean.
- Entries the scan did not read are counted, never inspected. An `Entries not
  read` count above zero means the tree holds something — a symbolic link, a
  device, a socket — that this scan says nothing about.

## What is not here

readmit never writes to a credential store, never creates or deletes a stored
credential, and never presents a credential itself: this release's MLLP
transport opens a connection and sends the bytes of a case, and
[`readmit-run/v1`](run-bundle.md) records the transport it used, naming no
credential reference. Recording a reference in run evidence is a future contract
change, not an addition to a released version.

A credential bound to something other than an MLLP endpoint is a separate
contract rather than a second `purpose` here, because widening `purpose` would
change the meaning of `readmit-secrets/v1` rather than adding a new version.
[Evidence protection](protect.md) registers a key that way, and an
[external observation](observe.md#authentication-references) registers the
credential its HTTPS endpoint is read with the same way. All of them read a
value back through the one bounded mechanism this page describes; there is no
second way to obtain one.
