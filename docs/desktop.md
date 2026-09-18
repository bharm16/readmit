# Desktop shell

The desktop application opens a workspace folder, lists what that folder
declares it holds, verifies one case bundle at a time, and finds the occurrences
of that case that matter through a filtered grid over its index. It is the same
engine the command line runs: `internal/desktop` is a typed Go facade over the
same internal packages, and the interface calls it directly. No command output
is parsed, and no HL7 or case bundle semantics exist in TypeScript.

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
| `InspectOccurrence` | Verifies the grid identity again and reveals one selected occurrence, its navigable tree, escaped raw/decoded values and bounded hex bytes. |
| `OpenGrid` | Renders one bounded window of one case through one index of it. |
| `Filters` | Lists the filters this viewer saved and the one selected now. |
| `SaveFilter` | Stores one named filter and selects it. |
| `SelectFilter` | Records which saved filter the grid applies. |
| `Shell` | Describes the window: regions, statuses, commands, appearance, privacy. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. |

Exactly one operation runs at a time. A second request reports `busy` rather
than racing the first, and a finished operation always releases the slot,
including after a failure or a cancellation, so the next request proceeds.
`RecentWorkspaces`, `Filters` and `Shell` are the exceptions. The first two read
one small local file each and `Shell` reads nothing at all, so none of them
claims the slot and all stay available while an operation runs: the recent list,
the selected filter, the command palette and the privacy status work whenever
the window is open. `Shell` cannot
fail in the facade; it still carries a state, because the binding itself is
unavailable while the application is starting, and the window says so rather
than drawing itself with no commands and no privacy status.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`OpenRevisions`, `SaveNote`, `Search`, `OpenGrid`, `SaveFilter` and
`SelectFilter` and `InspectOccurrence` are not, because each runs to completion under its own size
limits once it starts. The window enables the
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
cross the boundary from `OpenCase`. Deliberately selecting a grid row reveals
those occurrence bytes through `InspectOccurrence` without persisting them.

## Inspecting original values

Select **Inspect** on a grid occurrence, then choose a segment, field,
repetition, component or subcomponent. The selection, its state and byte range,
escaped raw value, decoded value and highlighted raw/hex window all come from
one verified read. The exact-selector form can also address omitted positions;
`empty`, explicit `null`, and `omitted` stay separate. The inspector uses the bundled `readmit-field-labels/v1` labels only when
MSH-12 declares v2.5.1, with the nHapi revision and MPL-2.0 provenance displayed.
Other versions, unknown segments and unknown positions stay explicitly
unlabeled. The bundle contains field labels only, not segment names, datatypes,
cardinality or semantic conformance rules.

Tree navigation shows at most 100 immediate children; Next/Previous children
reaches the rest. Original bytes are shown 256 at a time, including framing and
terminators. Offsets are zero-based, half-open ranges within the original
occurrence; add the displayed source offset for the original capture position.
Selecting a part jumps to its bytes, and moving through byte pages preserves
the selected range. Unparsed occurrences have no field tree but retain every
byte. A selected value larger than 4096 bytes reports `too_large` rather than
showing a truncated value; all original bytes remain available through pages.

The inspector resolves the parser's standard HL7 escapes for selected values.
It displays ASCII (including an omitted/empty MSH-18 default) and explicitly
declared `UNICODE UTF-8`. Other character-set declarations, invalid bytes and
unsupported escapes have distinct indicators and no fabricated decoded text.
Controls, non-ASCII raw bytes and HTML metacharacters are escaped before entering
the webview. Decoded Unicode is displayed with ASCII escapes too; this is a
presentation over the original bytes, never a serialization of them.

Every navigation call reopens the canonical case and compares its identity with
the displayed grid. Changed, missing, incomplete or corrupt evidence refuses
the read. Changing the case, grid page or filter clears the occurrence selection
and inspector, so old values cannot remain beside a new grid. Failed reads clear
the prior inspector result and retain the grid for recovery. This bounded read
runs to completion and cannot be cancelled. No raw or decoded value is written
to evidence, recent folders, saved filters, browser storage or logs.

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

## The message grid

A case holds thousands of occurrences and a window holds a screenful. `OpenGrid`
renders one bounded window of one case: the occurrences the selected filter
kept, in the order the case records them, with where each one is and what it is.
Asking for the next window is another call, so a large case is never drawn at
once and never held in the interface.

It names two entries of the open workspace: the case, and one `readmit-index/v1`
file built from that case by `readmit index build` — see [searching a
case](index.md). The index is the one search path over a case. The grid reads no
message again, keeps no second index of its own, and asks every question about a
value or a decoded state through the index, so the retention an operator
declared is enforced by the index itself.

Both are checked before one row is reported, and checked again for every window:

| What is checked | What happens when it does not hold |
| --- | --- |
| The case verifies as complete, unmodified evidence | Refused; the evidence is untouched |
| The index was built from exactly this evidence | Refused; build the index again from this case |
| The index still matches what was written for it | Refused; the evidence is unchanged, build it again |
| The retention the index declared has not ended | Refused; build it again from this case, or delete it |
| The index retains what the filter asks about | Refused by name, never answered from something narrower |

Repeating the check for every window is deliberate. A grid that carried one
verified case from window to window would serve later windows from evidence
nobody looked at again, which is exactly what
[ADR-0008](adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md)
refuses. A refused index changes nothing and blocks nothing: the case stays
readable, and the remedy is always to build the index again.

### What a row carries

A row is the occurrence's ID, its source, its byte offset and size, its kind and
direction, the observed time the case recorded, and whether the case could
decode it. The offset and size are the byte span in the source that holds the
occurrence, so a row names where the original bytes are. An occurrence the case
recorded no observed time for carries none, which is why a time filter cannot
keep it.

No message content crosses this boundary: no field value, no message byte and no
original source path, exactly as `OpenCase` reports counts and an identity and
nothing else. An occurrence marked as not decoded carries no indexed field at
all, so no question about a field can be answered about it — which is a
different statement from the field being absent. Raw and decoded inspection of
one occurrence is not in this release.

### The number of excluded records

Every grid states how many occurrences the selected filter removed from it. A
filtered view that hides records without saying how many reads as though the
case held nothing else, which is the interface equivalent of a false negative.

| Count | What it is |
| --- | --- |
| `total` | Every occurrence the case holds |
| `matched` | How many the filter kept |
| `excluded` | `total` minus `matched`: how many this view is not showing |
| `undecided` | Retained values this filter's questions could not settle, over the whole case |
| `undecodable` | Occurrences the case itself could not decode, kept or not |

`excluded` never depends on the window being drawn: a page of one row over a
case of ten thousand reports the same exclusion as a page of fifty.

`undecided` says that something this filter asked could not be settled: a value
the index shortened to its retained prefix cannot answer a substring question
either way, so it is counted rather than reported as a miss. The index answers a
question about a field over every occurrence it holds, so this is an **upper
bound** on the uncertainty in the view — a value counted here may belong to an
occurrence the type, source or time axis had already excluded for a definite
reason. It over-states how much is unsettled rather than under-stating it, which
is the only direction a caveat about a filtered view may be wrong in. Questions
about different fields add up; the acknowledgement outcomes are alternatives
about one field, so that axis is counted once rather than once per outcome. The
remedy for a `undecided` that matters is to build the index again retaining
digests, which settle exact questions of any length.

`undecodable` is a fact about the case rather than about this view, the way
`index show` reports it: such an occurrence carries no indexed field at all, so
any question about a field leaves it out, and calling that an absent value would
claim the case does not hold something nobody ever decoded.

A window that renders no row is `empty` with the reason it is empty — a case
holding no occurrence, every occurrence excluded, or a window beginning past the
last one the filter kept — and it still carries the counts, because how many
records were removed is the answer in that case.

### Saved filters

A filter narrows along six axes, and an axis left empty narrows nothing. Within
one axis the values are alternatives; across axes an occurrence has to satisfy
all of them.

| Axis | What it compares | Answered from |
| --- | --- | --- |
| Type | `message`, `ack` or `unparsed` | The occurrence the case recorded |
| Source | The source ID the case names | The occurrence the case recorded |
| Time | The observed time, from one instant up to but not including another | The occurrence the case recorded |
| State | A field decoded as `present`, `empty`, `null` or `omitted` | The index, in any retention form |
| ACK outcome | A literal `MSA-1` acknowledgement code | The index, which must retain `MSA[1]-1[1]` |
| Field value | A field that equals or contains a value | The index, which must retain that field |

An occurrence the case recorded no observed time for cannot satisfy a time
bound: unknown is not a pass, and the excluded count says it was left out. A
source a case does not hold matches nothing rather than being refused, because a
saved filter outlives the case it was made for.

Saved filters are one bounded, versioned `readmit-filters/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`filters.json`, beside the recent workspace list in the user configuration
directory:

```json
{"schema":"readmit-filters/v1","filters":[{"name":"rejected acknowledgements","kinds":["ack"],"sources":[],"observed_from":null,"observed_until":null,"ack_codes":["AE","AR"],"fields":[]}],"selected":"rejected acknowledgements"}
```

Unknown members, unknown versions and an omitted declaration are all errors; a
time bound that is not declared is stated as an explicit null. There is no
migration and no repair. A document this release cannot read is reported and
left exactly as written: saving into it is refused rather than replacing it, and
the grid reports the same refusal rather than quietly rendering an unfiltered
view of the case. Listing a workspace, verifying a case and reading a project
are unaffected.

`selected` is what survives navigating from one case to another. It is stored
rather than held in the interface, so the view a person set up is the view they
get on the next case, and the one they get when the window is opened again. An
empty selection applies no filter and excludes nothing. Selecting a name nothing
saved is refused; saving under a name that already exists replaces exactly that
filter. Removing a saved filter is not in this release.

**A saved filter holds what a person typed to filter by.** A value typed to
match an HL7 field is the same patient data that field holds, and it does not
become metadata by being convenient to store. It stays in one owner-readable
file on this machine, it is never written into a case, a run, a result, a review
or a report, and the privacy region names it. This is the one thing the grid
keeps that came from a person reading evidence; nothing read out of a case is
kept anywhere.

## The window

`Shell` is the window's description of itself, and the interface renders it
rather than keeping a second copy that can drift from the facade. It carries the
regions, how every status reads, the commands, the appearance choices and the
privacy status. The bindings repeat that vocabulary as closed TypeScript types
and a facade test requires them to, so a region, command, theme, status or match
kind the facade declares and the bindings do not is a failing test. What the
frontend type check adds on top of that is narrower than it sounds, and worth
stating exactly: a region entered as nothing and a command with no action fail
the build, because the records holding them are keyed by the declared
identifiers and hold an element and a function. A status the facade declares
without an indicator is not a type error — indicators are looked up at run time
and every place that draws one falls back to the plain status word — so that is
checked in the facade, where every operation state, artifact kind and registered
case status is required to have one.

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

`Search` navigates one open workspace. It is not the grid: it finds the things a
workspace and its project declare, and the grid finds the occurrences inside one
case. It reads exactly what the listing reads —
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
message content is the grid, over an index the operator built and declared the
retention of.

### Appearance

The window offers `system`, `light` and `dark`, and text sizes from 100% to
200%. Both start from the system every time the window opens and are written
nowhere. The shell stores recent folder paths and saved filters in separate
owner-only local documents. Saved filter terms may contain patient data entered
by the operator; no values read from case evidence are persisted by the shell.

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
evidence, which is the recent folder list and the filters a person saved. A
saved filter is named there rather than left to be discovered, because it holds
whatever was typed to filter by. That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

## Not supported in this release

- Creating a project, registering a case or a revision, and any rename, archive
  or delete operation. The shell opens folders, reads artifacts and edits notes;
  everything else about a project is `readmit project`.
- Removing a note, and the previous text of one that was replaced.
- Importing evidence, editing evidence, and comparison. No edit the shell makes
  reaches a case, a run, a result, a review or a report.
- Building an index. The grid reads one that `readmit index build` wrote, so
  which fields are retained, in what form and until when stay three declarations
  an operator made explicitly.
- Character-set transcoding, local/formatting HL7 escapes, and semantic dictionary definitions beyond the bundled field labels.
  The inspector reports these limits explicitly and keeps original bytes accessible.
- Removing a saved filter and sharing one between viewers.
- Authoring more than one source or more than one field question per filter in
  the window. The stored contract holds up to 128 sources and 16 field
  questions, and applies every one of them.
- Sorting a grid, and any order other than the one the case records.
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
