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
| `RecoverSession` | Restores the retained working session and reopens the run it was watching, read-only. |
| `RecordView` | Retains the workspace, case, region and run this viewer has open. |
| `SaveDraft` | Retains one note that has been typed and not stored yet. |
| `DiscardDraft` | Drops one retained draft, once the note it was an edit of has been stored. |
| `Compare` | Aligns two collections of the open workspace and reports one window of the rows both panes draw. |
| `EditReproducer` | Adds one step to a reproducer plan and reports what it now means over the case. |
| `UndoReproducer` | Removes the last step of a plan and resolves what remains. |
| `BuildReproducer` | Writes the reproducer into a new folder of the open workspace. |
| `AuthorTest` | Answers one stage of a test draft and reports what it now means over the case. |
| `SaveTest` | Writes the generated test spec into a new entry of the open workspace. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. |

Exactly one operation runs at a time. A second request reports `busy` rather
than racing the first, and a finished operation always releases the slot,
including after a failure or a cancellation, so the next request proceeds.
`RecentWorkspaces`, `Filters`, `Shell`, `RecordView`, `SaveDraft` and
`DiscardDraft` are the exceptions. The first two read one small local file each
and `Shell` reads nothing at all, so none of them claims the slot and all stay
available while an operation runs: the recent list, the selected filter, the
command palette and the privacy status work whenever the window is open. The
last three write one small local file each and do not claim it either, for a
different reason: a crash while a case is being verified is exactly when
unstored work has to survive, so refusing to retain it because an operation is
running would lose the state recovery needs most. They are serialized among
themselves, so a reader never observes a partial document. `Shell` cannot
fail in the facade; it still carries a state, because the binding itself is
unavailable while the application is starting, and the window says so rather
than drawing itself with no commands and no privacy status.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase`, `OpenProject`,
`OpenRevisions`, `SaveNote`, `Search`, `OpenGrid`, `SaveFilter`,
`SelectFilter`, `InspectOccurrence`, `Compare`, `EditReproducer`, `UndoReproducer`,
`BuildReproducer`, `AuthorTest`, `SaveTest` and `RecoverSession` are not, because each runs to completion under its own size
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

The interface draws less than that again: the rows of a window are
**virtualized**. The table holds the rows of one viewport plus an overscan on
either side, and the rows before and after them are measured space rather than
elements. Scrolling replaces which rows are drawn, so the number of rows in the
document is decided by the viewport and never by the size of the window or of
the case. A window no larger than the viewport and its overscan is drawn whole,
so a small case behaves exactly as it did. The table states how many rows the
window holds with `aria-rowcount` and each row its position within it with
`aria-rowindex`, because a row that is not in the document is still a row of the
window.

That is why the interface asks for the facade's own window bound rather than a
smaller one. What a window costs to draw no longer follows how many rows it
holds, so one call now covers four times as much of a large case as it did, and
paging to the next window stays what it was: another call that verifies the case
and re-checks the index against it.

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

## Building a reproducer

An incident holds everything that happened; what a vendor or a regression test
needs is much smaller. **Reproducer** is the panel beside the grid that extracts
it: retain the occurrences that matter, keep the setup dependencies they need,
edit supported fields, and write the result as a separate revision.

This is the one place the window writes evidence, and it writes only **new**
evidence. The case being read is never changed: a build creates a new folder of
the open workspace holding a derived `readmit-case/v3` bundle and the
transformation manifest beside it. A folder that already exists, a name that is
not one entry of the workspace, and a destination inside any retained artifact
are each refused by the same output policy that refuses every other write into
retained evidence.

Nothing about what a step means is decided here. The interface sends the plan
and the step; the engine resolves both against the verified case and sends back
what it retained, why, and everything it could not settle — so the panel shows
what a build would write rather than a second opinion about it. A step the
evidence does not support leaves the plan exactly as it was and reports the
refusal, and **Undo the last step** removes the step added last and resolves
what remains, which is why dropping an occurrence and undoing that drop returns
its edits as well.

The panel shows positions, not content: an occurrence ID, its kind, the reason
it is retained and what required it, and for an edit the position it addresses
and where the new bytes landed. Reading a value is still
[the inspector](#inspecting-original-values), deliberately. The plan lives in
the window while it is being edited, is never placed in browser storage, and is
written nowhere except into the manifest of a reproducer that was built.

A reproducer is derived testing data, not a redaction and not an approval to
share. See [extracting and editing a reproducer](reproducer.md) for both
contracts, the two dependency relations, every refusal, the bounds, and how to
register the result as a project revision.

## Authoring a regression test

A case says what happened; a regression test says it should happen again. That
statement is a `readmit-test/v1` document with seven members and three nested
objects, and writing one by hand means opening it in a text editor.

**Regression test** is the panel beside the grid that asks it as questions
instead: what the test is called, which occurrences of the verified case it
sends, which target configuration it sends them to, what decides the outcome,
where that observation is read from, how the fixture is reset, and what the run
should have produced. When every question is answered, the panel writes the spec
into one new entry of the open workspace, and `readmit test` runs that file
unchanged.

Nothing about what an answer means is decided here. The interface sends the
draft and the answer; the engine resolves both against the verified case and the
open workspace and sends back the question that is still open, the initial state
the chosen boundary fixed, the order a run will send the selected occurrences
in, and the targets the workspace offers with the classification each one
records. An answer the evidence or the boundary does not support leaves the
draft exactly as it was and reports the refusal, so a half-answered draft is a
state a person is in and a contradictory one is never reached.

The panel shows positions, not content: an occurrence ID and its kind, a target
name and the contract it declares, an expectation's identifier, operator and the
position it addresses. Reading a value is still
[the inspector](#inspecting-original-values), deliberately, and a position is
chosen by clicking through its field tree rather than typed from memory. The one
thing this panel holds that came from a person is an **expected value they
typed**, which is the same customer-local literal the field it describes holds:
it lives in the window while the draft is open, is never placed in browser
storage, and reaches disk only in the spec that was saved.

A saved test is a document beside the evidence, never inside it, and the case it
names is not changed. See
[authoring a regression test](test-authoring.md) for the draft contract, every
stage, every refusal, the bounds, and what this release does not author.
## Comparing two collections

**Compare** is the panel that answers what changed between two collections of
the open workspace. It is the engine [`readmit diff`](diff.md) runs, called
directly: the window sends two entry names, the fields that identify a record
and the fields to compare, and renders the report it gets back as rows. Nothing
about what is the same record is decided here.

Both collections are verified case bundles named by one entry of the open
folder, and the left one is the case the window has already verified. Every
comparison re-reads both and is bound to the identity the window displayed, so
a comparison beside stale counts is refused rather than shown. Neither collection
is changed, and a comparison writes nothing at all: there is no output to name.

### How records are paired

| What holds | How records pair |
| --- | --- |
| The two collections are copies of one verified case | By the occurrence identity the evidence already carries |
| Anything else | By the field selectors named as keys, together as one ordered composite key |

A control ID a system regenerated is an ordinary field change and never becomes
a pairing, so two collections whose identifiers were all rewritten still pair on
whatever identifies the record itself. Two collections with no known mapping and
no declared key are refused with what to declare, because pairing them by
position would be a guess presented as a result. The keys are echoed back in
their canonical form, so what a row was paired on is visible rather than
remembered.

### What a row is

Both panes are columns of **one** row list, so a row is the same record on both
sides and the two cannot drift apart. Every row states what it is:

| Row | What it holds |
| --- | --- |
| `paired` | A record on both sides, with the positions that differ |
| `missing` | A record the left collection holds and the right one does not |
| `inserted` | A record only the right collection holds |
| `ambiguous` | One candidate of a key that names more than one record |
| `unaligned` | One record no key could place, with the reason |

A record only one side holds keeps a row of its own with the other side stated
as empty, rather than shifting every row after it — an insertion that quietly
re-pairs everything below it is the hidden alignment assumption a comparison
exists to avoid. A duplicated key produces one row per candidate, on the side
that candidate is on: none of them is paired with another, because the evidence
does not say which pairing it would be. The rows are ordered by what the
comparison found — paired, then missing, then inserted, then every ambiguous
candidate, then everything unaligned. That order is not the order either
collection recorded and it is not evidence of chronology.

A row is a window of a comparison, not the whole of it. Every window states
where it begins and how many rows the comparison holds, beside the counts of
everything paired, changed, unchanged, inserted, missing, unaligned and not
compared — and, separately, how many **keys** were ambiguous, which is a count
of duplicated keys rather than of the candidate records they left unpaired.
Every window also names the versioned engine contract the rows were laid out
from and the boundary that was compared, so a comparison of stored messages
never reads as a comparison of everything the two collections hold; the counts
of what each side left outside that boundary are beside it. Asking for the next
window verifies and aligns both collections again, for the same reason the grid
re-checks its case and index.

An ambiguous key is resolved where it was declared: name another field beside
it, and the keys together form one ordered composite key. The window says so on
the rows it refused to pair, because a group that only reports itself leaves a
person with nothing to do about it.

### Positions, not values

A paired row names the positions that differ, each as a canonical
[field selector](selectors.md) with the bundled dictionary's label where both
messages declare the version it covers, and the decoded state each side held
there: `present`, `empty`, explicit `null` and `omitted` stay separate. **No
value crosses this boundary.** Reading what is at a position is
[the inspector](#inspecting-original-values), deliberately, exactly as it is for
a reproducer; the selector a row names is the position the inspector addresses.
Alignment-key values are never shown either, and no comparison is written
anywhere: nothing about this panel reaches a case, a run, a result, a report,
the saved filters or the working session.

**No ignore rule is applied here.** Every difference the comparison found is
shown, including the timestamps and control IDs a person may not care about,
because a view that suppressed some of them without saying so could conceal the
change being looked for. Narrowing a comparison is done by naming the fields to
compare, which the window states beside the result.

Evidence the comparison could not read is listed rather than compared around: an
occurrence nothing decoded, a position whose escapes this release does not
resolve, a value that decoded to bytes that are not UTF-8, and a message
declaring an HL7 version the bundled labels do not cover. Equal fields are not
proof of delivery or of correct behaviour, and the window renders the engine's
own statement of that rather than a summary of it.

## Recovering after an interruption

A window can be closed, lost with its process, or killed in the middle of a
send. What a person had typed and not stored is theirs, and losing it because a
process stopped is a defect; what a send did to a receiver is unknown, and
deciding it because a window reopened would be a lie. The shell keeps those two
apart.

Where this viewer is, and every note they have typed and not stored, live in one
bounded, versioned `readmit-desktop-session/v1` document
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)) in
`session.json`, beside the recent workspace list and the saved filters in the
user configuration directory:

```json
{"schema":"readmit-desktop-session/v1","view":{"workspace":"/absolute/folder","region":"evidence","case":"regression","run":"/absolute/folder/job-001"},"drafts":[{"project":"/absolute/folder","note":{"name":"triage","subject":"regression","title":"First pass","body":"still writing this"}}]}
```

It is separate from finalized evidence in every sense. It is written outside any
case, run, result, review or report — the same output policy that refuses every
other write into retained evidence refuses this one — and it is a per-viewer
file on this machine, never part of a bundle, never in browser storage, and
never sent anywhere. A draft is the editable note type the project document
already holds, so working text retained here is working text
[`project note`](project.md) and `SaveNote` can store; retaining one writes
nothing into the project, and storing it stays a separate deliberate step.

`RecordView` retains the open workspace, the entry selected in it, the region
holding focus, and the durable run being watched. `SaveDraft` retains one note
under the name it will be stored as, replacing exactly that draft; `DiscardDraft`
drops one, which is what the window does once the note has actually been stored,
so recovery offers back only work that is still unstored. A viewer retains at
most 16 drafts, and past that bound the new edit is refused rather than an
existing one being dropped.

`RecoverSession` is what the window calls when it opens. It returns the retained
view and every retained draft, and when the session names a durable run it
reopens that run through the same read-only recovery `OpenDurableRun` uses. The
window reads it once and hands the answer to the panels that show it, so only
one of them claims the operation slot.

In the window this is two panels. **Restored after an interruption** states what
came back — where you were, the state of the run you were watching, and every
unstored note, each of which can be discarded. **Write a note** is the editor:
every keystroke is retained through `SaveDraft` — one retention at a time, always
of the newest text, so a slow earlier write cannot land after a later one —
**Store this note in the project** writes it into `revisions.json` through
`SaveNote` and then discards the draft, and a store the project refuses leaves
the draft retained, because text the project did not take is still unstored work.
A draft may name the case or revision it is about; whether that subject is one
the project registers is checked when the note is stored, not while it is being
typed, because a draft is written before the project is opened. Starting either action in
**Durable test runs** records that folder as the run being watched and waits for
that record before the action runs, so a crash during a send finds the session
already naming the folder that holds its evidence. Renaming a note while it is
being written moves the retained draft rather than leaving one behind under the
previous name.

**Recovery reads. It never resumes, restarts or resends.** A run whose
completion was never recorded stays `interrupted`, a delivery whose effect
nobody knows stays `delivery_uncertain`, and neither becomes a pass because the
window was opened again — see [durable local runs](durable-runs.md) for what
those states mean and what to check before running anything again. Bytes already
written to a receiver stay retained and stay sent: a crash cannot retract them
any more than a cancellation can. Executing again is `StartDurableRun`, which is
a deliberate action and requires a new output folder, so no recovery path can
become a resend. A run the session names but cannot verify is reported as
unverifiable, with its retained evidence untouched; the rest of the session is
restored regardless.

| What is retained | What is not |
| --- | --- |
| The workspace, case, region and run that were open | Anything read out of a case: message bytes, field values, decoded text |
| Notes typed and not stored yet | Notes already stored, which are in the project's own document |
| Nothing else | A verdict, a resumed run, or a second send |

Unknown members, unknown versions, a relative folder path, a case naming
anything but one entry of the open workspace, a region the window does not
declare, and drafts that are unsorted, duplicated or past the note rule are all
errors. There is no migration and no repair. A document this release cannot read
is reported and left exactly as written: retaining into it is refused rather
than replacing it, and the rest of the window keeps working. It is replaced
atomically, so a reader never observes a partial session, and an interrupted
write retained beside it is reported rather than reused.

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
nowhere. The shell stores recent folder paths, saved filters and the working
session in three separate owner-only local documents. Saved filter terms and a
retained draft may contain patient data entered by the operator; no values read
from case evidence are persisted by the shell.

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
project's own document, retained in the working session while it is unstored, is
never sent anywhere, and is never kept in browser storage.

The window states this rather than leaving it to be assumed. The privacy region
names what this product does not do and everything the shell writes outside
evidence, which is the recent folder list, the filters a person saved and the
working session they have not stored. A saved filter and a retained draft are
named there rather than left to be discovered, because one holds whatever was
typed to filter by and the other a note whose subject is the evidence beside it.
That status is part
of the facade, so it is the same fact the rest of the product is built on rather
than a sentence the interface maintains separately, and the frontend sources are
checked to hold no network call and no browser storage at all.

## Not supported in this release

- Creating a project, registering a case or a revision, and any rename, archive
  or delete operation. The shell opens folders, reads artifacts and edits notes;
  everything else about a project is `readmit project`.
- Removing a note, and the previous text of one that was replaced.
- Writing more than one note at a time in the window. The facade retains up to
  16 drafts, recovery returns every one of them, and the command line reaches
  the same notes; the window's editor writes the one draft of the open
  workspace, whichever case or revision that draft says it is about.
- Resuming, restarting or resending an interrupted run, from recovery or from
  anywhere else in the window. The window has no resume, the retained output is
  always refused for a new execution, and an uncertain delivery is never
  resolved by reading. The command line's `run resume` is a separate deliberate
  action into a new output that repeats only never-attempted work and refuses
  after any send; see [durable local runs](durable-runs.md).
- Storing a note on a person's behalf. A retained draft stays a draft until it
  is stored deliberately, and a refused store leaves it retained as unstored
  work rather than discarding it.
- Sharing a working session between viewers or machines, retaining more than one
  session per viewer, and any history of what a draft said before it was
  replaced.
- Retaining an unbuilt reproducer plan or an unsaved test draft across an
  interruption. The working session is one bounded versioned document and it
  gains no member here, so unstored work of either kind is lost with the window;
  a reproducer that was built and a test that was saved are on disk and are read
  back from what was written.
- Running, editing or importing a test. The panel writes a new spec; executing
  one is `readmit test`, and reading a saved spec back into a draft is a
  separate delivery. Suggesting expectations from a run, and approving them, are
  a separate delivery too.
- Importing evidence and changing evidence. No edit the shell makes reaches a
  case, a run, a result, a review or a report: a reproducer is new evidence
  written beside the original, never a rewrite of it, and a comparison writes
  nothing at all.
- Comparing anything but two case bundles of the open workspace, and comparing
  stored acknowledgements rather than stored messages. A run, a result, a
  report and a standalone message file are listed as unsupported entries here;
  [`readmit diff`](diff.md) compares all of them and both boundaries.
- Reading a value in a comparison. A row names the positions that differ and the
  decoded state of each side; the bytes are the inspector.
- Ignore rules, normalization policies, a reviewed baseline, and telling input
  drift apart from target, environment and rule drift. Every difference this
  panel found is shown as it was found; those are separate deliveries.
- Retaining a comparison across an interruption. The working session is one
  bounded versioned document and it gains no member here, so which two
  collections were being compared is lost with the window; comparing them again
  reads both from disk and verifies both.
- Reduction, replay transformations, reordering or duplicating occurrences, and
  comparing two reproducers. The editor retains what a person selected and what
  their declared dependencies require, and makes no claim of minimality; see
  [the reproducer contract](reproducer.md) for what this release does not do.
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
