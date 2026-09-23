# Validation workflow

## While implementing

Use an isolated worktree from the latest `origin/main`. Inspect open PRs and
active worktrees before touching shared workflows, release manifests, or shared
test helpers. Incorporate merged prerequisites before final validation; preserve
the other change's contracts, fixtures and registrations during integration.

Run `make test-focused PKGS='./internal/example ./internal/caller' ARGS='-run TestName'`
at the affected behavior and its callers. Widen the selection when a shared
contract changes. Go test compilation checks types; `make check` verifies the
compiler, formatting and vet. Keep successful local test caches available.

For Python tooling, use `python3 -m unittest discover -s tools -p 'test_NAME.py' -v`.
For frontend edits, run `npm ci && npm run build` in `desktop/frontend`; this
includes TypeScript checking. Run the frontend behavior tests with `npm test`
in the same directory: Vitest in run mode, deterministic and offline. The
desktop module is a separate Go build.

The frontend tests live beside the components and go through
`desktop/frontend/src/testkit`, which installs a stub of the bindings'
exported `Facade` interface at the exact `window.go.desktop.App` surface the
production bindings read. A test can answer only the calls the real facade
publishes, with the real types; an unanswered call rejects instead of
succeeding quietly. The kit's fixtures carry positions, states and counts,
never an HL7 value, a credential, a machine path or a network address, and
the setup file resets the window and the stub between tests. A workflow that
changes what the window does gets an interaction test through this entry,
driving real user events; the shared-operation parity evidence for it stays
on the Go side, where `internal/desktop` and the domain packages are tested
against the same readers the command line uses.

A journey that must cross the real facade — several screens, real files, a
close and reopen, a behavior no stub can answer honestly — goes through
`desktop/frontend/src/testkit/journey.tsx` instead, in
`desktop/frontend/src/journeys/NAME.journey.tsx`, and runs with
`npm run test:journeys` (it needs the Go toolchain: the global setup builds the
`desktop/journeybridge` program and the command line once per run). There is
no stub: every call reaches the real `internal/desktop` facade over real files,
carried with Wails' call semantics, except that a call Wails would leave
unanswered is rejected. The kit's verbs:

- `Journey.create()` owns a new temporary root; `launch()` starts the
  application over it and mounts the production window; `dispose()` in
  `afterEach` ends it and removes the root.
- `press(user, button)` presses a control once the window offers it. Pressing
  a disabled control does nothing, so every step after real work uses it.
- `chooseFolder(path, title)`, `chooseFiles(paths, title)` and
  `dismissDialog(kind, title)` answer the next host dialog, before the action
  that opens it. Name the dialog title the facade shows.
- `writeFile`, `makeFolder` and `provisionLicense` prepare what a person or
  their vendor put on the machine before the application saw it: exported
  evidence, a folder for a project, a signed activation folder.
- `close()` waits for the window to settle, then ends the process as closing
  the window does; `crash()` ends it at once and abandons what was running;
  `launch()` again reopens over the same files.
- `calls` and `callsTo(method)` record every call the window made and what the
  facade answered; `commandLine(args)` runs the checkout's `readmit` over the
  same root, for parity with what the window wrote.
- `enter(user, field, text)` replaces what a field holds once the window
  offers it, and `byContent(pattern)` matches a line the window composes from
  several elements, such as `Run: passed · Stop reason: passed`.
- `startDownstream(exportFile, mode)` starts the independent downstream
  scheduling system of `testkit/downstream.js` on a loopback port: an MLLP
  receiver written from the protocol description, sharing no readmit code,
  that keeps an appointment ledger, exports it as a CSV inside the root after
  every message, and has a real defect (`defective`) and its fix (`fixed`). It
  can reset its ledger, hold an acknowledgement, report every message it
  received — the independent witness to what a send did — and count the
  connections open to it now, which a connectivity check holds while it runs.
  `backdate(file, ms)` ages a file, for a stale export.

`src/journeys/steps.tsx` holds the steps several journeys take — activating the
vendor's license, creating a project, importing an export, configuring the
downstream target, authoring and running an acknowledgement test — each taken
through the window and checked against what it then shows, and the way a
timing is logged. Reuse them; a step only one journey takes stays in that
journey.

A journey fails at close for a dialog nobody answered, an answer nobody used, a
call Wails would have rejected or a call that never finished, and no verb
places a file outside the journey's root. Never call the facade directly to
stand in for a step a person takes, and state each expected outcome from the
scenario's own facts rather than from what the engine answered. Use synthetic
evidence only; the processes see an empty PATH and a home inside the root.

A new command, bound facade method, hub route, hub binary operation, vendor
portal operation, Makefile target or `tools/` script needs a row in
`docs/capability-ledger.json` (`readmit-capability-ledger/v2`, read by
`internal/capability`). A row names its `backend` operation
(`{"package": "internal/x", "name": "Func"}` or `"Type.Method"`), the
canonical `inputs` and `outputs` it reads and writes (`readmit-*/vN`
contracts, or `hl7` for raw message bytes), and its `prerequisites` from the
closed set in `internal/capability`; a command's activation prerequisite must
match its declared operation-guard admission. Once it is `implemented` it
names the `gui_test` that drives it — a component test
(`desktop/frontend/src/X.test.tsx`) or a journey
(`desktop/frontend/src/journeys/X.journey.tsx`), by its exact title — and the
`parity_test` (`{"package": "internal/desktop", "name": "TestX"}`) that proves
it reaches the shared engine; an open row names no test and its owner is the
open issue that will add its screen. A row that is not customer work carries a
typed `disposition` instead; a Makefile target or tools script is a `tooling`
row disposed as `developer-tooling`, with no backend.
`go test ./internal/capability` resolves every reference against the tree, so
rename a test, an operation or a contract together with its row.

## Before pushing

Batch related review fixes, resolve integration changes against current main,
and run the affected tests. Run `make test` once on the final local revision.
It runs the full race suite with small resource fixtures and the actual 8 MiB
observation boundary once without race instrumentation. The small boundary and
production boundary assert the same retained-prefix and refused-ACK behavior.
`make test-boundary` addresses the latter directly when that contract changes.

Timed fuzz campaigns belong in CI unless a fuzz failure or target change needs
local investigation. They do not run on pull requests; normal Go tests, and so
`go-tests` on every pull request, still exercise every target's seed corpus.
For a focused campaign use `python3 tools/fuzz.py --package ./internal/hl7`;
`--list` shows the selected targets. CI discovers every target and partitions
it across three shards; adding a fuzz function does not require editing a
workflow list. To fuzz a changed target in CI before the next daily campaign,
dispatch one on the branch with `gh workflow run ci.yml --ref BRANCH`.

Run `make test-tools`, `make verify`, or `make mutate` locally when changing those
tools or their checked contracts. The verifier and mutation runner also support
`--only NAME`. Every full verification and mutation check remains required in CI.

After a rebase, rerun tests for the integrated changes, then rely on fresh CI for
the final revision. If a failure appears unrelated, retain its exact output and
diagnose the individual failing test. Repeated package passes do not explain a
flake or establish that a failing revision is correct.

## CI and merge

The `quality` check aggregates Go tests, tooling/independent verification and
mutations, the vulnerability scan, the hub, and the three timed fuzz shards on
the events that run them. It fails if any job its event runs fails, is skipped,
or is cancelled, and if the fuzz shards ran on any other event. Packaging and
the five native smoke tests run concurrently, and test the exact archives later
used for publication.
The desktop module builds and scans separately, and `desktop` is that workflow's
equivalent stable aggregate: it requires the macOS shell build and the five
`desktop-package` jobs plus the five `desktop-install` jobs, which download,
install, check and remove the exact unsigned artifacts on fresh native runners.
The macOS shell job also executes the frontend behavior tests and the
interaction journeys and publishes their output as an artifact, so a failing
component test or journey fails the `desktop` aggregate rather than only a
developer's local run.
It fails the same way if any of them fails, is skipped or is cancelled. Release credentials remain exclusive to
trusted tag runs; no signing credential reaches any workflow.

Wait for `quality`, `package`, all five `native-smoke` checks, and `desktop` on
the final PR revision before merging. That list is unchanged by the desktop
packaging jobs, because they sit behind `desktop`. Require those stable contexts
in the repository's main-branch protection. After splitting jobs, keep `quality`
and `desktop` as the stable aggregates of their workflows so another worktree
never has to guess which new job names are mandatory. Each PR cancels only its
own superseded workflow runs; main, tag, daily and dispatched runs keep
independent groups.

Timed fuzz campaigns run in the CI workflow daily at 07:41 UTC, when dispatched,
and for every release tag, whose `publish` still requires them through
`quality`; they never run on a pull request or a push to main. Each shard first
restores the corpus the latest campaigns on main retained as the
`fuzz-corpus-1` to `fuzz-corpus-3` artifacts, all three of them, so a target
keeps its corpus when a new target moves it to another shard and no cache
eviction loses it; afterwards it retains its own. A failing campaign fails its
run and that run's `quality` check, shown on main's head commit, and uploads
the failing input as `fuzz-failure-N`: put it under the package's
`testdata/fuzz/FuzzName/` and rerun the `go test -run` line the log prints.

Every pull-request run's aggregate records the tree the run tested as an
artifact, `proven-tree-ci-TREE` or `proven-tree-desktop-TREE`. A push to main
first runs `proof` (`tools/proven_tree.py`). When the pushed commit is the
merge of exactly one pull request into main, and a successful pull-request run
of the same workflow for that pull request's final head, from this repository,
recorded exactly the pushed tree, every other job of the workflow is skipped
and its aggregate passes only because every one of them was skipped. That is
the case when the pull request's last run was made against the main commit it
merged onto. Every other push to main runs the whole workflow as the integration
check: a merge on a stale base, a direct push, a record from a run that failed,
is unfinished or has expired, and any error while looking. Tag, daily and
dispatched runs are always complete. A pull request may merge on a stale base,
so the main run after it is the check that proves that merge.

Each job has its own Go cache scope, including each fuzz shard. Keys include the
compiler, platform, dependency checksums and commit; a prefix restores the prior
generation before the current one is saved. This keeps compiled tests and fuzz
corpora from being overwritten by the faster packaging job. A first cold run
still needs compilation; assess steady-state savings on later commits as well.
A pull request's first run restores main's newest generation. A push to main
that skipped its jobs saves none; every complete push to main and the daily run
of each workflow save a new one. The repository's cache storage holds only a
few runs' caches and evicts the least recently used first, so anything that
must survive, such as the fuzz corpus, is kept as an artifact instead.

To measure a change to CI, give `python3 tools/ci_timing.py` the CI and desktop
run ids of one push (`gh run list` shows them). It prints each job's start,
duration, end and wait for a runner in seconds from its run's start, the
critical path through the jobs' `needs`, and, with `--caches`, the Go cache
each job restored or missed. Compare the same kind of push, a pull request's or
a merge's, before and after the change, and again once it has reached main.
