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
| `RecentWorkspaces` | Lists previously opened folders, most recent first. |
| `Cancel` | Stops the operation that is running now, when it can be interrupted. |

Exactly one operation runs at a time. A second request reports `busy` rather
than racing the first, and a finished operation always releases the slot,
including after a failure or a cancellation, so the next request proceeds.
`RecentWorkspaces` is the exception: it reads one small local file and does not
claim the slot, so the list stays available while an operation runs.

`Cancel` cannot retract bytes an operation has already written. Choosing a
folder and listing it are interruptible; `OpenCase` and `OpenProject` are not,
because each runs to completion under its own size limits once it starts. The
window offers the Cancel control only while an interruptible operation runs.

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
carrying the contract the document itself declares, read the same way a case
bundle directory reports the contract its manifest declares — never inferred
from the file name. `OpenProject` then returns the recorded document: the
project settings, the declared interface versions, and every registered case
with its title, tags, owner, status, linked incidents and **the case identity
the command line recorded**. That is the same value `OpenCase` reports for the
same evidence and the same value an exported packet names, because a project
records the bundle identity rather than deriving one of its own.

Reading a project verifies no evidence and rewrites nothing, so a recorded
identity reaches the window exactly as it was written. Whether a registered case
is still the evidence the project recorded is what `readmit project show`
reports. Creating a project, registering a case, and changing a title, tag,
owner, status or linked incident are command-line operations in this release;
the shell reads projects and does not write them. See
[interface investigation projects](project.md).

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
nothing is fetched at run time. Browser storage holds no evidence. Diagnostics
are fixed sentences that never repeat a path, a file name, an argument, or a
value.

## Not supported in this release

- Creating or changing a project from the shell, and any rename, archive or
  delete operation. The shell opens folders and reads artifacts; projects are
  created and managed with `readmit project`.
- Importing evidence, editing, message grids, search, and comparison.
- Artifacts other than case bundle directories and project documents. Run
  bundles, results, reviews, reports, specs and family records are listed as
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
