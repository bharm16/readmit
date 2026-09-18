# Desktop shell

The desktop application opens a workspace folder, lists what that folder
declares it holds, and verifies one case bundle at a time. It is the same engine
the command line runs: `internal/desktop` is a typed Go facade over the same
internal packages, and the interface calls it directly. No command output is
parsed, and no HL7 or case bundle semantics exist in TypeScript.

The shell is a separate Go module in `desktop/`, built with cgo and a platform
webview. The released command line stays a static `CGO_ENABLED=0` build and does
not contain any of this. See
[ADR-0005](adr/0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md).

## Building it

```sh
cd desktop/frontend && npm ci && npm run build
cd .. && go build -o build/readmit-desktop .
```

The interface is bundled into `frontend/dist` and embedded in the executable, so
`npm run build` must run before `go build`. `npm run build` type-checks first: a
binding that no longer matches the facade fails there. The desktop build is not
part of the release archives and is unsigned.

## Operations and states

Every operation returns exactly one state. Unknown, unsupported and incomplete
artifacts are never reported as completed.

| State | Meaning |
| --- | --- |
| `empty` | The operation succeeded and there is nothing to show. |
| `busy` | Another operation is running; this one did not start. |
| `cancelled` | The dialog was dismissed, or the operation was cancelled. |
| `failed` | The operation could not be completed. |
| `permission_denied` | This account cannot read or write the chosen folder. |
| `completed` | The operation finished and the result is present. |

| Operation | What it does |
| --- | --- |
| `SelectWorkspace` | Opens the host's native folder dialog, then opens that folder. |
| `OpenWorkspace` | Opens a folder already known, such as a recent one. |
| `CreateSampleWorkspace` | Writes the sample workspace into the chosen folder and opens it. |
| `OpenCase` | Verifies one listed entry as case evidence. |
| `OpenProject` | Reads the project document of a folder. |
| `OpenRevisions` | Reads the editable project document: its notes, drafts and recorded revisions. |
| `SaveNote` | Creates or replaces one editable note of a project. |
| `RecentWorkspaces` | Lists previously opened folders, most recent first. |
| `Search` | Finds what one open workspace declares and what its project registers. |
| `Shell` | Describes the window: regions, statuses, commands, appearance, privacy. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. |

Exactly one operation runs at a time. A second request reports `busy` rather
than racing the first, and a finished operation always releases the slot,
including after a failure or a cancellation, so the next request proceeds.
`RecentWorkspaces` and `Shell` are the exceptions. `RecentWorkspaces` reads one
small local file and `Shell` reads nothing at all, so neither claims the slot
and both stay available while an operation runs: the recent list, the command
palette and the privacy status work whenever the window is open. `Shell` cannot
fail in the facade; it still carries a state, because the binding itself is
unavailable while the application is starting, and the window says so rather
than drawing itself with no commands and no privacy status.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`OpenRevisions`, `SaveNote` and `Search` are not, because each runs to
completion under its own size limits once it starts. The window enables the
Cancel control only while an interruptible operation runs; `Escape` reaches the
same operation whenever the palette is not open, and cancelling when nothing is
running does nothing.

## Workspaces and artifacts

A workspace is a folder. Opening it lists each immediate entry with the contract
that entry **declares**: listing never verifies evidence. An entry that is neither a
case bundle directory nor a project document this release supports — a file, a
symbolic link, a folder with no readable manifest — is listed as `unsupported`
with the reason, never hidden and never counted as evidence.

`OpenCase` is the verification step. It runs the same reader `readmit timeline`
runs, which checks completion, identity, payload hashes and every record before
any count is reported, and it refuses a workspace entry named by anything other
than one entry of the open folder. Verified evidence is reported as counts and
the bundle identity: no message bytes, field values, or original source paths
cross the boundary into the interface.

## Projects

A folder holding a `project.json` lists that entry as a `project` artifact
carrying the contract the document itself declares. The canonical document is
found by its fixed name, exactly as a case bundle's manifest is, but nothing is
concluded from that name: the entry is decoded, and an entry this release cannot
read is listed as `unsupported` with the reason. `OpenProject` then returns the recorded document: the
project settings, the declared interface versions, and every registered case
with its title, tags, owner, status, linked incidents and **the case identity
the command line recorded**. That is the same value `OpenCase` reports for the
same evidence and the same value an exported packet names, because a project
records the bundle identity rather than deriving one of its own.

Reading a project verifies no evidence and rewrites nothing, so a recorded
identity reaches the window exactly as it was written. Whether a registered case
is still the evidence the project recorded is what `readmit project show`
reports. Creating a project, registering a case or a revision, and changing a
title, tag, owner, status or linked incident are command-line operations in this
release.

## Notes and the editable project document

A folder holding a `revisions.json` lists that entry as a `revisions` artifact
carrying the contract it declares, located and decoded the same way. It is the
editable side of a project: the notes and drafts a person maintains, and the
recorded lineage of every revision derived from registered evidence.
`OpenRevisions` returns it exactly as written, and a project that has recorded
neither reports `empty` rather than a failure.

`SaveNote` is the only thing the shell writes into a project, and a note is
working text. It is stored in that editable document, beside the evidence and
never inside it, so a UI edit cannot overwrite an import, a finalized run, a
result, a review or a report: the same output policy that refuses every other
write into retained evidence refuses this one. Writing a note under a name that
already exists replaces exactly that note; a note that names a subject must name
a case or revision the project registers, and one that does not is a draft.
Registering a revision is a command-line operation, because it is a statement
about verified evidence rather than an edit. A project folder this account
cannot write reports `permission_denied`, and a project that already holds as
many notes as this release stores reports the refusal rather than dropping one.
See [interface investigation projects](project.md).

## The window

`Shell` is the window's description of itself, and the interface renders it
rather than keeping a second copy that can drift from the facade. It carries the
regions, how every status reads, the commands, the appearance choices and the
privacy status. The bindings repeat each of those as a closed TypeScript type,
so a region with no content, a command with no action or a status with no
indicator fails the frontend type check instead of becoming a control that
quietly does nothing.

### Focus order

The window has five regions and focus moves through them in the order the
investigation runs:

| Region | What it holds |
| --- | --- |
| `commands` | The workspace actions, the appearance choices, and the search field. |
| `navigation` | The open folder's listing and the recent workspaces. |
| `evidence` | The project document and the cases it registers. |
| `inspector` | The one case verification currently being read. |
| `privacy` | What stays on this machine, and what this product does not do. |

The window renders the regions by walking that declared order and uses no
positive tab index, so the controls inside them are tabbed through in document
order, which is the order above. Every region is a labelled landmark that takes
focus itself without being a tab stop of its own: `F6` and `Shift+F6` move
between the regions, and the palette lists a "Go to" command for each one. The
evidence and inspector panes are separated by a separator that *is* a tab stop
and moves with `ArrowLeft`, `ArrowRight`, `Home` and `End` as well as with a
pointer, so the panes resize without one.

The region order, the statuses, the commands and their shortcuts are declared
once in the facade and tested there, including that every shortcut the palette
shows is a key the interface actually tests for. The frontend type check adds
one thing those tests cannot see: the record of region content and the record of
command actions are keyed by the declared identifiers, so a region the window
forgot to draw and a command with no action behind it are type errors rather
than controls that quietly do nothing. Neither check inspects a rendered window,
so what a region draws once it has an element is not among the things proved
here.

### Status without colour

Every status the window shows — the six operation states, the three artifact
kinds, and the four registered case statuses — has its own word and its own
shape, and no two of them share either. Colour is added on top of both and is
never the difference between two statuses. The word carries the meaning on its
own, because a shape depends on the platform font having the glyph, so the shape
is marked decorative and the word is what an assistive technology reads. A
selected row is marked with a rule and heavier text rather than a tint.

### Commands and search

The command palette lists everything the window can be asked to do, with the
shortcut for the ones that have one. `Ctrl+K` opens it and `Ctrl+F` moves to the
search field; the platform command key is accepted wherever `Ctrl` is shown.

`Search` navigates one open workspace. It reads exactly what the listing reads —
the contract each immediate entry declares — and, when the folder holds a project
document, the cases that document registers: their name, title, owner, tags,
linked incidents, status, interface version, contract, provenance and recorded
identity. It verifies no evidence, opens nothing, records no recent folder and
builds no index; it lists the folder again each time under the same bound.

A result names the thing it found the way the window already names it — the
entry name, or for a registered case the title the project recorded — the region
that reveals it, and the **fixed name of the declared field that matched**
rather than the text that matched. Nothing read out of a case bundle is in a
result: no message bytes, no field values, and no original source path. Nothing
to search for and nothing that matched are both `empty`, with different reasons.
A project document this release cannot read contributes no registered cases; the
listing already reports that entry as `unsupported`, and search still finds it by
its name.

This is navigation over what the facade already exposes. Searching inside
message content is not this, and is not in this release.

### Appearance

The window offers `system`, `light` and `dark`, and text sizes from 100% to
200%. Both start from the system every time the window opens and are written
nowhere: the shell keeps one file of local state and it holds folder paths only.

## The sample workspace

`CreateSampleWorkspace` writes the frozen `readmit-synth-v1` family into a new
`readmit-sample` folder inside the folder chosen in the dialog. Its declared
generator inputs are the reference vector in
[the synthetic vector](synth-v1-vector.md): seed `0`, base time
`2026-01-01T12:00:00Z`, generator `readmit-synth-v1`, profile `readmit-siu-v1`.
The family is a pure function of those inputs, so the sample is byte-identical
to the family produced by

```sh
readmit synth --seed 0 --base-time 2026-01-01T12:00:00Z \
  --generator-version readmit-synth-v1 --profile-version readmit-siu-v1 \
  --output readmit-sample
```

down to every manifest, record, payload and bundle identity. This evidence is
synthetically generated, not imported: its provenance says so, and the `invalid`
case is deliberately semantically invalid. It is a fixture family, never
customer data.

The destination must be new. A folder that already holds a `readmit-sample`
folder is refused and left exactly as it is, whether that folder is complete
evidence or output an interrupted attempt retained; recovery is to choose a
different folder, or to move the retained output aside outside the application.
A folder this account cannot write reports `permission_denied`.

## Recent workspaces

A workspace that opens is recorded so it can be reopened. The list lives in one
owner-readable file, `recent.json`, in the user configuration directory, under
the versioned contract `readmit-desktop-recent/v1`:

```json
{"schema":"readmit-desktop-recent/v1","roots":["/absolute/folder"]}
```

It holds at most ten absolute folder paths, most recent first, with no
duplicates. It holds nothing read out of a case: no message content, field
values, identifiers, bundle identities, or file names inside a workspace. It is
replaced atomically, so a reader never observes a partial list. Unknown members,
unknown versions and relative paths are rejected; there is no migration and no
repair. A list this release cannot read is reported and left exactly as written,
and opening workspaces still works while it stays unreadable.

## Privacy

Nothing leaves the machine. There is no telemetry, crash reporting, update
check, or analytics, and the interface never sends evidence to an external
rendering service. Everything the window renders is bundled into the executable;
nothing is fetched at run time. Browser storage holds nothing at all. Diagnostics
are fixed sentences that never repeat a path, a file name, an argument, or a
value. A note is text a person typed on this machine: it is stored in the
project's own document, is never sent anywhere, and is never kept in browser
storage.

The window states this rather than leaving it to be assumed. The privacy region
names what this product does not do and everything the shell writes outside
evidence, which is the recent folder list and nothing else. That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

## Not supported in this release

- Creating a project, registering a case or a revision, and any rename, archive
  or delete operation. The shell opens folders, reads artifacts and edits notes;
  everything else about a project is `readmit project`.
- Removing a note, and the previous text of one that was replaced.
- Importing evidence, editing evidence, message grids, and comparison. No edit
  the shell makes reaches a case, a run, a result, a review or a report. Search
  here is navigation over what a workspace and its project declare; searching
  inside message content, and the rebuildable index that needs, is not in this
  release.
- Artifacts other than case bundle directories and the two project documents.
  Run bundles, results, reviews, reports, specs and family records are listed as
  unsupported entries.
- Nested folders. Only the immediate entries of the chosen folder are listed,
  and at most 1024 of them; a larger folder is refused rather than listed in
  part.
- Progress events. Every operation here is bounded and short: listing reads
  directory entries, and verification is bounded by the case reader's own
  limits. Long-running work, and the progress reporting it needs, arrives with
  the operations that have it.
- Installation, upgrade, signing, and a supported desktop platform matrix.
  Continuous integration builds the shell natively on macOS as a build check,
  which is not a support claim.
