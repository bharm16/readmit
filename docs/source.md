# Collecting from an approved source: `readmit source`

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


`readmit source` brings evidence into readmit from a place the **customer**
controls and an operator explicitly approved: an export directory this machine
can already open, or a remote export reached by running the customer's own
read-only transfer program. It stages the original bytes in a new directory with
a receipt of what it did, and [`import --collection`](import.md#importing-a-staged-collection)
turns that directory into a [case bundle](case-bundle.md) under the plan the
receipt records.

```sh
readmit source diagnose exports.json \
  --framing mllp --terminator cr --encoding utf-8 --direction inbound --member .mllp
readmit source collect exports.json --output collected --receipt collection.json \
  --framing mllp --terminator cr --encoding utf-8 --direction inbound --member .mllp
readmit import --collection collection.json --folder collected \
  --output incident.case --receipt import.json
```

| Subcommand | What it does |
| --- | --- |
| `source diagnose SOURCE` | Reports what access to the declared source was **actually** available, and collects nothing |
| `source collect SOURCE --output NEW_DIRECTORY --receipt NEW_FILE` | Stages the source's evidence unchanged, with the receipt of what was collected |

| Exit code | Meaning |
| --- | --- |
| 0 | `diagnose`: access is available for the declared scope. `collect`: the collection completed. |
| 1 | A document could not be read, a declaration was refused, or a destination was not approved. Nothing was staged. |
| 2 | Access is not available, or the collection did not complete. What was staged is not the whole of the declared scope. |

## The one rule

**A collection that could not run never becomes evidence that nothing was
there.** A source nobody had permission to list, one that lost its connection
part way through a transfer, one whose entry was refused by a quota, one this
release does not support, and one the operator cancelled each collected zero
entries — and so did a genuinely empty export directory. The counts are
identical and the evidence is not.

Every one of those is an **execution error**, named in the same source-neutral
vocabulary [`readmit observe`](observe.md) owns rather than a second one
invented here: `complete` is the only status under which the counts describe the
source, and every other one maps to a durable-run execution error. Unknown and
unsupported are not pass, and a timeout is not a negative result.

## The declaration: readmit-source/v1

The document is explicitly selected, never discovered: there is no default
source, no implicit file and no environment variable that supplies one.

```json
{
  "schema": "readmit-source/v1",
  "name": "scheduling-exports",
  "kind": "transfer",
  "scope": "appointments",
  "address": "sftp.lab.example:22",
  "classification": "nonproduction",
  "command": "/usr/local/libexec/readmit-sftp",
  "arguments": ["--host", "sftp.lab.example", "--path", "/exports/appointments"],
  "secrets_file": "secrets.json",
  "credential": "lab-sftp",
  "quota": {"max_entries": 64, "max_entry_bytes": 4194304, "max_total_bytes": 33554432},
  "retry": {"attempts": 2, "backoff": "250ms"}
}
```

`schema`, `name`, `kind`, `scope`, `quota` and `retry` are required of every
kind. Unknown members and unsupported schema versions are rejected; there are no
in-place migrations, and a later version string is reported as unsupported
rather than read as this one.

The remaining members belong to **one kind each** and are refused on any other,
so a declaration cannot carry a remote address that nothing dials or a local
root that nothing opens.

| Member | Kind | Meaning |
| --- | --- | --- |
| `root` | `directory` | The directory whose own entries are collected |
| `address` | `transfer`, `api` | Explicit host and numeric port the source is reached at |
| `classification` | `transfer`, `api` | The recorded environment class: `nonproduction`, `production`, `unclassified` |
| `command`, `arguments` | `transfer` | Absolute path of the read-only transfer program, and the arguments that select the source |
| `secrets_file`, `credential` | `transfer` | The [credential reference](secret.md) the program is given, declared together or not at all |

`scope` is load-bearing. A collection is only ever a statement about the
declared scope, and it appears in everything the two subcommands retain.

### The three kinds

| `kind` | How it is reached | This release |
| --- | --- | --- |
| `directory` | The entries of one directory this machine can already open — a local export folder, a mounted share, a tree another tool already synced | Collected |
| `transfer` | By running the operator's declared read-only transfer program, which is how an SFTP export is collected | Collected |
| `api` | A documented read-only application interface | **Not collected.** See [the read-only API collection contract](#the-read-only-api-collection-contract) |

A collection reads **one directory's own entries**. A subdirectory is counted as
an entry it did not read; name it as its own source. An entry that is not
regular file bytes — a symbolic link, a device, a socket — is counted rather than
followed, so listing a tree can never leave it, loop, or block on a device.
Every report states how many entries it did not read, so a collection never
implies it saw more than it did.

Which entries are in scope is the **import plan's** declared members, not a
second selection rule: `--member .hl7` here means exactly what it means for a
folder an import reads. Each collected entry is then streamed under the rest of
that plan, so a collection refuses exactly what an import of the same bytes
refuses and reports what an import would find.

### Quota

Every member of `quota` is required and none has a default: how much of a
customer's evidence readmit may pull is the operator's declaration, never a
number this release picked for them.

| Member | Bound |
| --- | --- |
| `max_entries` | 1 to 4,096 |
| `max_entry_bytes` | 1 to 16 MiB, and never above `max_total_bytes` |
| `max_total_bytes` | 1 to 128 MiB |

A quota is applied to what the source's own listing **declares**, before
anything is read, so a source past a limit is refused in a moment and nothing is
staged. A collection past a quota is **refused, never cut short**: a prefix of a
source is not the source. An entry that turns out to be larger than
`max_entry_bytes` while it is being read is refused the same way and reported as
an entry that was not collected.

### Retries safe for reads

`attempts` is 1 to 3 and `backoff` is a Go duration from `0s` to `5s`. An
`attempts` of 1 declares that a failed read is a failed read.

A retry never turns an uncertain read into a confident one:

- **Every attempt starts the entry again from nothing.** A partial staged copy
  is removed before the next attempt, so a collected entry was read whole by one
  single attempt and its digest is over exactly the bytes that attempt read. No
  entry is ever stitched together out of several.
- **Only a delivery failure is retried** — an entry that could not be opened, a
  stream that stopped, a transfer program that did not complete, and a source
  that returned a different number of bytes than it declared, which is what a
  transfer that dropped after a clean exit looks like.
- **A refusal the bytes themselves caused is never retried**, such as framing
  that contradicts the declared framing: reading the same bytes again produces
  the same refusal, and repeating it would only make a declaration error look
  intermittent. Neither a quota refusal nor a cancellation is retried either.
- The receipt records the attempts each entry took, so an entry that needed two
  says so rather than looking like one that needed one.

### Duplicate detection

Two entries holding identical bytes are the same evidence. The first the source
listed is staged; the second is recorded as a `duplicate` naming the entry it
repeats, and is not staged again. Identity is the SHA-256 of the entry's own
bytes, following [ADR-0002](adr/0002-case-bundles-are-directories-not-a-database.md):
**no timestamp, no absolute path and nothing about the machine** enters it.

Sameness is decided by the bytes alone. A name is what a source *calls* an entry,
not what the entry is, and this is deliberately the same shape as an excluded
member in an [import](import.md): the entry is accounted for with its reason
rather than omitted, and the receipt — not the staged directory — is where the
source's own listing is reconstructed from. If you need one staged copy per name,
collect the two names as two sources.

The receipt's own `identity` is a digest over the staged entries' names and
contents in the same scheme, so the same evidence collected twice identifies the
same way, on any machine, at any time.

## Approved destinations

A `transfer` or `api` source reaches off this machine, so it goes through the one
destination rule [`internal/sendpolicy`](replay.md) owns, exactly as a replay
does: a `production` classification refuses every collection; a name is resolved
at the moment the question is asked rather than when the declaration was written;
and an unrecorded class, an unresolvable name, a name resolving to several
addresses and an address outside every approved destination are each **denied**.
`--policy FILE` selects the `readmit-send-policy/v1` document explicitly; nothing
supplies one implicitly, and a literal loopback address is the only destination
available without one.

**A recorded class is a claim, never an authorization.** Labelling an export
server nonproduction is not proof that it holds nonproduction data.

A `directory` source reaches no destination and is asked nothing about one. The
diagnosis says so explicitly rather than leaving the member blank.

## Source permission diagnostics

`readmit source diagnose` reports what access **was actually available**, and
establishes it by attempting the access it reports. An entry is readable because
the diagnosis opened it and read from it, never because its recorded mode bits
looked permissive. An entry it could not open is counted as unreadable, never
skipped. A source it could not list at all is reported as a source it could not
list — not as a source with no entries.

It writes a `readmit-source-access/v1` document with `--report NEW_FILE`, and
with `--json` writes it to standard output instead of the summary. The report
destination is checked before the source is reached, by the receipt's rule
below. It stages nothing, collects nothing and modifies nothing else.

```json
{
  "schema": "readmit-source-access/v1",
  "source": {"name": "scheduling-exports", "kind": "transfer", "scope": "appointments"},
  "checked_at": "2026-01-03T11:00:00Z",
  "status": "complete",
  "run_state": "",
  "reason": "",
  "listed": true,
  "credential": {"state": "bound", "reference": "lab-sftp", "rotation": "current", "reason": ""},
  "destination": {"schema": "readmit-send-decision/v1", "allowed": true, "...": "..."},
  "declared": 12, "selected": 10, "readable": 10, "unreadable": 0, "not_read": 2,
  "declared_bytes": 41232,
  "quota": {"max_entries": 64, "max_entry_bytes": 4194304, "max_total_bytes": 33554432},
  "quota_exceeded": []
}
```

`credential.state` is `none`, `bound` or `refused`. A reference that is missing,
registered for another purpose, or scoped to another endpoint is **refused**,
which is reported as a refusal rather than as no credential. Binding reads no
value: it settles what a credential may be used for, never what it is.

`source collect` binds the same reference before it reads anything, so a
mis-scoped credential is refused by name rather than surfacing later as a
transfer program that would not answer.

## Credentials by reference

Following [ADR-0006](adr/0006-credentials-are-referenced-never-stored.md),
readmit holds no credential. A source names a reference registered in a
[`readmit-secrets/v1` document](secret.md), and this release adds the one purpose
a collection needs:

```sh
readmit secret add --secrets secrets.json --name lab-sftp \
  --purpose source-endpoint --store customer-managed --address sftp.lab.example:22 \
  --command /usr/bin/secret-tool --argument lookup --argument service --argument readmit-lab
```

`source-endpoint` is read access to evidence and nothing else. A reference
registered for `mllp-endpoint` is refused wherever a source needs one, and the
reverse, because binding demands the purpose the caller needs and compares the
scoped address byte for byte. Nothing resolves a name to decide whether a
credential applies.

The value exists only inside the one command that read it: it is written to the
transfer program's **standard input**, followed by one newline, and nowhere else.
It is never an argument, because an argument is visible to every process on the
machine. It is never an environment variable readmit sets, never written to a
file, and it is carried by a type that masks itself under every formatting verb
and refuses to be serialized at all — so a receipt, a diagnosis, a log and a
console summary can each be shared without carrying one, because none of them
ever held one.

## The read-only transfer program

readmit speaks no SSH. An SFTP source is collected by running the **customer's
own** read-only transfer client, by the absolute path the declaration gives,
never looked up on `PATH` and never through a shell — the same mechanism
[`secret`](secret.md) already uses to read a credential out of a store readmit
does not own. readmit runs the program it was given and **cannot establish what
answered**, so the declared address is what a destination decision is about, and
never a verified property of what the program connected to.

The contract is two verbs, appended after the declared arguments:

| Invocation | What it must print on standard output |
| --- | --- |
| `COMMAND ARGS... list` | One line per entry: the entry's byte count in decimal, a tab, and the entry's name |
| `COMMAND ARGS... get NAME` | Exactly that entry's bytes |

- A name must be **one name inside the source**: never empty, `.`, `..`, a path
  with a separator, an absolute path or a reserved device name. A listing that
  names anything else is refused, so a transfer can never stage a file outside
  the collection directory.
- The program's own standard error is **discarded**, so a transfer that fails
  noisily cannot write a credential or a patient value into readmit's output.
- A nonzero exit is a failed read. One invocation is bounded to five minutes and
  to one listing of at most 1 MiB; a program that has not answered leaves the
  source unavailable — never empty, and never a pass.
- A `get` that returns a different number of bytes than its listing declared is a
  source readmit cannot vouch for, and the entry is reported rather than staged
  as a shorter one that looks complete.
- A collection that stops gives the program one second to exit and release its
  output before readmit closes it, so a cancellation is acknowledged in a moment
  rather than when a program's own children happen to finish.

**The program carries the operator's authority, and readmit adds none.** readmit
runs what the declaration names, with the arguments it names, as the account
running `readmit`. It does not sandbox the program, restrict what it opens, or
check where it connects: only the declared *address* is put through the
destination decision. Declaring a transfer program is therefore the same kind of
decision as declaring the program that reads a credential out of a store, and it
is the operator's to make. Point it at a client you would run yourself.

### A worked wrapper around stock `sftp`

No stock client prints the two verbs above, so the operator supplies the few
lines that adapt one. This is the whole of it for OpenSSH's `sftp`, which is not
shipped with readmit and is written by the operator, reviewed by the operator,
and stored where the operator chooses:

```sh
#!/bin/sh
# readmit-sftp HOST REMOTE_DIR <list|get> [NAME]
# readmit writes the credential on standard input. This writes nothing back to
# the source, and puts nothing but the entry's own bytes on standard output.
set -eu
host=$1 remote=$2 verb=$3
read -r SSHPASS
export SSHPASS
case $verb in
list)
  printf 'ls -l %s\n' "$remote" |
    sshpass -e sftp -q -b - "$host" |
    awk '/^-/ { printf "%s\t%s\n", $5, $NF }'
  ;;
get)
  printf 'get %s/%s /dev/stdout\n' "$remote" "$4" |
    sshpass -e sftp -q -b - "$host"
  ;;
esac
```

The one thing to get right is that `get` puts the entry's bytes on standard
output and **nothing else**: a progress line or a banner mixed in is bytes
readmit would stage as evidence. readmit checks the length against the listing,
so a stray line is caught rather than staged silently, but it is caught as a
failed read rather than as the misconfiguration it is.

Any client works the same way — `lftp`, `rclone`, a vendor's own transfer tool, or
a program that reads from a mounted share. readmit asks only for a listing and a
stream, so the adapter is small and the authentication stays where the customer
already manages it.

This is deliberately not an SSH client inside readmit. The released executable is
one statically linked binary for [five targets](adr/0001-go-single-binary-release-matrix.md)
with Cobra as its only direct third-party dependency; an SSH and SFTP stack is a
change to that release story rather than a feature of this command, and running
the customer's approved client is both smaller and honest about who is
authenticating the host.

## The read-only API collection contract

An `api` source is **declarable and not collected** in this release. Declaring
one is read and validated; `source diagnose` records it with status
`unsupported`, which is an execution error, and `source collect` refuses it by
name. Unsupported is not an empty source, and it is not a pass.

A conforming API collection, when one is approved, satisfies all of the
following. Nothing below is implemented here, and no vendor interface is named,
claimed or assumed:

1. **Read-only by construction.** Only methods that are defined by the interface
   to have no side effect. No create, update, delete, acknowledge, mark-as-read
   or state transition of any kind, including one a vendor documents as harmless.
2. **A documented, licensed interface.** The contract is the vendor's own
   published documentation for an interface the customer is licensed to use.
   readmit infers no interface from observed traffic: observed data does not
   define the expected contract.
3. **An explicitly approved destination.** The same `readmit-send-policy/v1`
   decision a transfer source is held to, reached before the first request.
4. **Credentials by reference only**, under the `source-endpoint` purpose, scoped
   to the one address, never stored and never rendered.
5. **Bounded by the same declared quota**: entries, bytes per entry and bytes in
   total, applied to what the interface declares before anything is read, and
   refusing rather than truncating.
6. **An explicit watermark and pagination.** Collection resumes from a position
   the source itself orders by. A page that could not be fetched is an execution
   error covering the whole collection; it is never a short last page.
7. **Original bytes retained.** The response body is staged exactly as received.
   A decoded projection is a derived revision, never the evidence.
8. **Every failure named.** Unavailable, throttled, partial, truncated,
   unauthorized and unsupported are each their own named execution error, and
   none of them is an empty result.

No claim of Epic, Oracle Health, Rhapsody or Corepoint certification follows from
generic file, transfer or API support, and none is made here.

## The receipt: readmit-source-collection/v1

The receipt is written to the file named by `--receipt`, beside the staged
evidence rather than inside it. Its destination must not exist, must not be
inside retained evidence, and is checked before anything is collected, so a
receipt destination that is already taken cannot leave staged evidence behind
that nothing describes. Folders it is named in that do not exist yet are
created, owner-only, when it is written, and never inside retained evidence.
The desktop window collects through the same operation.

```json
{
  "schema": "readmit-source-collection/v1",
  "source": {"name": "scheduling-exports", "kind": "directory", "scope": "appointments"},
  "collected_at": "2026-01-03T11:00:00Z",
  "plan": {"schema": "readmit-import-plan/v1", "...": "..."},
  "status": "complete", "run_state": "", "reason": "",
  "quota": {"...": "..."},
  "entries": [
    {"name": "a.hl7", "state": "collected", "declared_size": 214, "size": 214,
     "sha256": "...", "records": 1, "occurrences": 1, "attempts": 1,
     "duplicate_of": "", "reason": ""},
    {"name": "b.hl7", "state": "duplicate", "duplicate_of": "a.hl7", "...": "..."},
    {"name": "notes.md", "state": "excluded",
     "reason": "name does not end with a declared member suffix", "...": "..."}
  ],
  "totals": {"declared": 3, "collected": 1, "duplicates": 1, "excluded": 1,
             "unreadable": 0, "not_read": 0, "bytes": 214, "records": 1, "occurrences": 1},
  "identity": "..."
}
```

Every entry the source listed carries a state, so a collection accounts for its
whole listing rather than reporting only what it kept — including a collection
that stopped early, whose remaining entries are recorded as entries it never
reached. `totals.declared` counts the entries readmit deliberately did not open
as well, so what a receipt lists is `declared` minus `not_read`. `declared_size` and `size`
are separate members because a source that declared one length and produced
another is a source readmit cannot vouch for. `plan` is the import plan recorded
verbatim, so the declarations a collection ran under stay with the bytes it
staged.

A receipt carries no verdict about the [case bundle](case-bundle.md) bounds. A
collection stages a directory; how many sources, occurrences and bytes an import
of that directory may write is decided by the import that reads it, against the
whole container it is given, and it refuses rather than truncates. Answering it
from a collection's own totals would be a claim about evidence nothing checked.
To see what a stream holds and which case bounds it is already past without
writing anything, use [`readmit corpus scan`](corpus.md).

A collection that did not complete keeps what it staged — bytes that were read
are evidence — and its `status` is what says they are not the whole of the scope.

## Importing the collection

`readmit import --collection collection.json --folder collected --output
NEW_DIRECTORY --receipt NEW_FILE` is the handoff from a collection to evidence.
It reads this receipt strictly, as the contract above declares it: every member
is required, a member the contract does not declare is refused, and a receipt
whose `identity` is not the digest of the collected entries it records is
refused. It refuses a collection whose `status` is not `complete`, and imports
the staged folder under the `plan` the receipt records only when the folder
holds exactly the collected entries with the sizes and digests recorded for
them. Nothing is imported under a plan the collection did not run under, and
[import](import.md#importing-a-staged-collection) describes each refusal. The
desktop window's finalize step runs the same operation.

## Console output and privacy

Both summaries report counts, the declarations the person made and the statuses
this release names. The collection summary names each entry, because an entry
name is what a person acts on and is already in the receipt they chose to keep;
neither command displays a byte of the evidence at all, and neither has a
`--show-values`, because `readmit timeline CASE --show-values` is the one place
evidence is read. No credential, no locator argument, no source root and no
transfer address reaches either document or either summary.

Nothing is uploaded. A `directory` source accesses no network at all, and a
`transfer` source accesses only what the operator's own program accesses.

## Explicitly not supported

- **No SSH or SFTP implementation inside readmit.** A remote export is collected
  by running the operator's declared read-only transfer program. readmit does not
  authenticate a host, verify a host key, or establish what answered.
- **No API collection.** See the contract above. Declaring an `api` source is
  read; collecting from one is refused.
- **No integration-engine export adapter.** Mapping a vendor export is
  [`import --recipe`](mapping.md); a supported engine export format is separate
  work that is not in this release.
- **No recursion.** One directory's own entries are collected. A subdirectory is
  its own source.
- **No writing to a source.** A collection reads. It never creates, renames,
  moves, deletes, acknowledges or marks anything at the source, and there is no
  flag that would.
- **No scheduling, watching or polling.** One collection is one foreground run
  with cancellation. There is no daemon, no timer and no retry loop that outlives
  the command.
- **No merging into an existing collection.** `--output` is a new directory. A
  second collection is a second directory beside the first, because original
  evidence is immutable and a collection is never rewritten in place.
- **No case bundle.** A collection stages bytes and a receipt.
  [`import --collection`](import.md#importing-a-staged-collection) turns them
  into evidence, so there is one ingestion path into a case rather than two.
- **No desktop-only source behaviour.** The desktop capture screen saves,
  reopens, diagnoses and collects a registration, and finalizes a collection,
  through these same operations and readers, and adds none of its own; see
  [capture, collect and listen](desktop.md#capture-collect-and-listen).
