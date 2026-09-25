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

The frontend's declarations of both bound facades and every type they carry,
`desktop/frontend/src/bindings.gen.ts`, are generated from the Go types. After
changing a facade method or any Go type one reaches, run `go run ./bindgen` in
`desktop` and commit the file; `go test ./bindgen` there fails while it differs
from what the Go types declare, in either direction, optionality included.
Resolve a rebase conflict in that file by rebasing the Go changes and running
the generator again, never by editing it; hand-written call policy lives in
`bindings.ts`.

The frontend tests live beside the components and go through
`desktop/frontend/src/testkit`, which installs a stub of the bindings'
exported `Facade` interface at the exact `window.go.desktop.App` surface the
production bindings read. The customer-hub host administration panel has a
second narrow binding, `window.go.hubadmin.Admin`, installed by the same kit
through `installHubAdmin`; its Go package lives in the desktop module so it
can use the hub's readers without adding the hub module to the static CLI.
A test can answer only the calls the real facade
publishes, with the real types; an unanswered call rejects instead of
succeeding quietly. The kit's fixtures carry positions, states and counts,
never an HL7 value, a credential, a machine path or a network address, and
the setup file resets the window and the stub between tests. A workflow that
changes what the window does gets an interaction test through this entry,
driving real user events; the shared-operation parity evidence for it stays
on the Go side, where `internal/desktop` (or, for that narrow hub handoff,
`desktop/hubadmin`) and the domain packages are tested against the same
readers the command line uses.

The setup file also holds React's development owner-stack budget spent
(`src/testkit/owner-stacks.ts`). Otherwise React records a stack for more of
the elements a test creates the longer the test runs, so on a loaded machine
the same test does about twice the work and can exceed its time limit only
under load. The setup fails if React stops keeping that budget where the kit
reads it.

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
  `afterEach` ends it and removes the root. `launch({ fileSizeLimit })` runs
  the application on a full disk: the bridge lowers its own file size limit
  once it has started, so any write that would make one file larger than that
  many bytes is refused, and launching again without it gives the disk room.
- `press(user, button)` presses a control once the window offers it. Pressing
  a disabled control does nothing, so every step after real work uses it.
- `chooseFolder(path, title)`, `chooseFiles(paths, title)`,
  `nameNewFolder(path, title)` and `dismissDialog(kind, title)` answer the next
  host dialog, before the action that opens it. Name the dialog title the
  facade shows. Each answers only as the host's dialog can: a folder dialog
  returns a folder that exists when it is answered, never a file or a folder
  not there yet; a save dialog, where a person names a new folder a writer
  will create, returns a name in a folder that exists, which need not exist
  itself, and creates nothing. Any other answer fails the journey.
- `writeFile`, `makeFolder`, `makeLink` and `provisionLicense` prepare what a
  person or their vendor put on the machine before the application saw it:
  exported evidence, a folder for a project, a symbolic link to a folder, a
  signed activation folder. A file is private to this account unless
  `writeFile` is given a mode, such as `0o700` for a program an administrator
  staged.
  `provisionLicenseIssues(issues)` provisions several activation folders, each
  with its own term (sequence, expiry relative to now such as `-48h`, grace
  days) and all signed with one fresh key, so a later issue installs as the
  renewal of an expired one. `makePrivateFolder` creates a folder only this
  account can open, such as a runner's root. `digest(path)` is the SHA-256 of
  a file's bytes, the identity a hub or a release names those bytes by, so a
  journey states it from the file rather than from what the window answered.
  `deploymentAuthority()` is a customer's deployment authority
  (`testkit/deployment.js`), written from the update manifest's documentation:
  a fresh Ed25519 key held in memory, whose public key a runner configuration
  pins and whose `manifest({engine, sha256, signed})` is the
  `readmit-runner-update/v1` document it signs over a staged candidate's
  bytes for this machine's platform, or leaves unsigned.
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
- `changeFile(file, content)` rewrites a file already inside the root in
  place, as another program does after the window may have read it — evidence
  changed on disk between two reads — and refuses a file that does not exist.
- `readFile(file)` reads a file the window or the command line wrote, and
  `placeFixture(name, file)` copies one of the checkout's shipped synthetic
  fixtures (`testdata/fixtures`) into the root byte for byte, as a documented
  example is copied.
- `automationAgent(script, variables)` runs a POSIX shell script inside the
  root as a customer's automation agent runs the workflow it was handed: with
  the variables it was provisioned with, which never replace the isolated
  environment. `commandLineExecutable` is the path of the checkout's
  `readmit`, the installed executable such a workflow names.
- `startHub(project, grants)` starts a real customer hub for the project, as
  its operator runs it: the checkout's `readmit-hub` over mutual TLS on
  loopback, its store in a disposable PostgreSQL cluster created inside the
  root and stopped when the journey ends, its own license, and a customer
  identity provider for the people it grants roles. The hub names the client
  configuration folder a window selects, completes a sign-in the window started
  as the person's browser does (`signIn(authorizationURL, subject)`, for a
  five-minute session unless a lifetime in seconds is given), issues a
  runner token (`runnerToken(subject)`), runs the binary's operator operations
  with the service stopped (`operate`) and restarts it with further policies
  (`restart`). `startHub(project, grants, "operator")` serves it
  operator-only instead, without its access policy, its operation policy
  binding the client certificate to the licensed author, and names the
  operator-only configuration a window chooses (`operatorConfig`);
  `serveAs("team")` and `serveAs("operator")` restart it in the mode its
  operator chooses, and once it has served team mode its store stays
  team-enabled. It needs `READMIT_POSTGRES_BIN` naming a PostgreSQL
  installation's `bin` folder; without one the hub is not built,
  `Journey.hubAvailable` is false and a hub journey calls `context.skip()`.
  CI's `hub-journeys` job runs the hub journeys with PostgreSQL 16, beside
  the `hub` job rather than after it; the desktop workflow's journeys skip
  them.
- `colleague(name)` is another person's window on their own machine: the same
  application over its own folder inside the root, driven through its facade
  (`call(method, ...args)`). It stands in for the person whose actions the
  window under test must meet — a concurrent reviewer, the approver of a
  request — and never for a step the person under test takes.

`src/journeys/steps.tsx` holds the steps several journeys take — activating the
vendor's license, creating a project, importing an export, the whole
investigation up to a configured downstream target, authoring an
acknowledgement test, and preflighting and sending it once — each taken
through the window and checked against what it then shows, and the way a
timing is logged. Reuse them; a step only one journey takes stays in that
journey. A control that lists the folder's entries is filled once the window
has read the folder again after a write, so wait for the option before
selecting it.

A journey fails at close for a dialog nobody answered, an answer nobody used, a
call Wails would have rejected or a call that never finished, and no verb
places a file outside the journey's root. Never call the facade directly to
stand in for a step the person under test takes (a `colleague` is another
person's window, never that person's), and state each expected outcome from the
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
it reaches the shared engine. The hub host administration binding alone names
`desktop/hubadmin` parity tests because the hub module imports the root module;
those tests call its real readers and offline functions, and the journey bridge
binds the same method as the production shell. The ledger still resolves every
parity reference. An open row names no test and its owner is the
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
workflow list. To fuzz a changed target in CI, dispatch a campaign on the
branch with `gh workflow run ci.yml --ref BRANCH`.

Run `make test-tools`, `make verify`, or `make mutate` locally when changing those
tools or their checked contracts. The verifier and mutation runner also support
`--only NAME`. Under [ADR-0011](../adr/0011-pre-product-ci-runs-only-correctness-checks.md)
CI runs them only on a manual dispatch and a release tag, so these local runs
are the only check a pull request that changes them gets.

After a rebase, rerun tests for the integrated changes, then rely on fresh CI for
the final revision. If a failure appears unrelated, retain its exact output and
diagnose the individual failing test. Repeated package passes do not explain a
flake or establish that a failing revision is correct.

## CI and merge

Until the product works, CI runs only correctness checks on pull requests and
pushes to main ([ADR-0011](../adr/0011-pre-product-ci-runs-only-correctness-checks.md)):
`go-tests` (formatting, vet and `make test`), `hub` and `desktop-shell` (frontend
build and behavior tests, and the shell's Go build and tests on macOS). The
shell's Windows-only code is tested only in the Windows `desktop-package` job.
Tooling tests, independent
verification, mutation checks, vulnerability scans, timed fuzzing, the release
archives and their native smoke tests, and the native desktop packages and
their installation checks run only on a manual dispatch
(`gh workflow run ci.yml --ref BRANCH`, `gh workflow run desktop.yml --ref BRANCH`)
and, for `ci.yml`, a release tag. Nothing runs on a schedule. A green automatic
run is therefore no evidence about packaging, installation, the dependency
advisories or the tools.

Journeys are opt-in even then. Every event, a default manual run included,
skips frontend, hub, packaged CLI, and native accessibility journeys, so a
green run provides no journey or interactive-accessibility acceptance evidence.

Journeys remain runnable locally. Explicitly opt into CI execution with
`gh workflow run ci.yml --ref BRANCH -f run_journeys=true` for hub and packaged
CLI journeys, or `gh workflow run desktop.yml --ref BRANCH -f run_journeys=true`
for frontend and native journeys. An opted-in failure still fails its aggregate;
`quality` requires the hub-journeys job to be skipped when not opted in.

The `quality` check aggregates Go tests and the hub, and on a manual or tag
run also tooling/independent verification and mutations, the vulnerability
scan and the three timed fuzz shards, plus the hub journeys when opted in. It
fails if any job its event runs fails, is skipped, or is cancelled, and if a
manual-or-tag job ran on any other event. On a manual or tag run, packaging and
the five native smoke tests run concurrently, and test the exact archives later
used for publication.
The desktop module builds separately, and `desktop` is that workflow's
equivalent stable aggregate: it requires the macOS shell job, and on a manual run the five `desktop-package` jobs plus the five
`desktop-install` jobs, which download, install, check and remove the exact
unsigned artifacts on fresh native runners.
An install check never runs on the runner that built its package: that runner
carries the Go, Node and WiX build setup, the build tree and the frontend's
modules, which a person's machine does not, so a package that works only
beside its build would pass there.
The macOS shell job executes the frontend behavior tests, plus interaction
journeys only when explicitly opted in, and publishes their output as an artifact.
A failing enabled test fails the `desktop` aggregate. The Windows package job runs the shell's own Go tests
on Windows, where its Windows-only code runs.
An install job can also drive the application it installed through the
platform's accessibility API with `tools/native_journey.py`, before removing
it, and publish the receipt and the accessibility tree at each checkpoint as
`native-journey-OS-ARCH`; a failing native journey fails that install job and
so `desktop`. These journeys run on all five targets only when a manual
workflow dispatch explicitly enables `run_journeys`. `NATIVE_JOURNEYS` in
`desktop-package` and `desktop-install` must stay identical, so only an enabled
run builds the provisioning bridge. Automatic installation jobs run without
journeys. The journey steps and every expected outcome live once in that
tool; the per-platform backends in `tools/native/` only read the tree and act
on it, so a new step is written once for every platform. `make test-tools`
checks the driver against a fake backend. Run it locally only on a machine
where you can give up the keyboard: on macOS it answers folder and save panels
with keystrokes, once the application is frontmost, and needs your terminal to
hold the accessibility permission.
The `desktop` aggregate fails the same way if any job its event runs fails, is skipped or is cancelled. Release credentials remain exclusive to
trusted tag runs; no signing credential reaches any workflow.

Wait for `quality` and `desktop` on the final PR revision before merging.
Those two stable contexts are main's required checks in branch protection. After splitting jobs, keep `quality`
and `desktop` as the stable aggregates of their workflows so another worktree
never has to guess which new job names are mandatory. Each PR cancels only its
own superseded workflow runs; main, tag and dispatched runs keep independent
groups.

Timed fuzz campaigns run in the CI workflow when dispatched and for every
release tag, whose `publish` still requires them through `quality`; they never
run on a schedule, a pull request or a push to main. Each shard first
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
is unfinished or has expired, and any error while looking. Tag and dispatched
runs are always complete. A pull request may merge on a stale base,
so the main run after it is the check that proves that merge.

Each job has its own Go cache scope, including each fuzz shard. Keys include the
compiler, platform, dependency checksums and a deliberate cache epoch. A main
run saves one entry for that identity on an exact miss, restoring an older epoch
first when one exists. An exact hit skips the post-job save, so ordinary main
pushes and dispatches do not create another generation. Pull requests, tags and
other branches restore main's entry and save nothing. This keeps separate
compiled tests and fuzz caches from being overwritten by the faster packaging
job. The latest measured main cohort was about 5.8 GB; the stable key avoids
adding a full cohort on every merge. Confirm the repository-wide budget and
fuzz reuse from the API after this policy reaches main.
When source changes without a dependency or compiler change, Go recompiles
changed packages against the saved snapshot; those new outputs are not added
to the cache. Measure PR duration and cache usage before deliberately changing
`CACHE_EPOCH` in `.github/actions/setup-go/action.yml` (for example, from
`stable-1` to `stable-2`). Make that change on main, then measure the new entry
set and fuzz restores before treating the older epoch as disposable. If a
module checksum or compiler pin changes, the new identity likewise seeds on
the next main run; pull requests changing it compile cold until it reaches
main. The fuzz corpus remains a retained artifact, independent of cache
retention.

To measure a change to CI, give `python3 tools/ci_timing.py` the CI and desktop
run ids of one push (`gh run list` shows them). It prints each job's start,
duration, end and wait for a runner in seconds from its run's start, the
critical path through the jobs' `needs`, and, with `--caches`, the Go cache
each job restored or missed and whether it saved one. With `--budget` it prints
the repository's Actions cache in use and every entry grouped by key prefix and
by the scope that saved it (main, a pull request, another branch or a tag),
with how many were restored after they were saved. Compare the same kind of
push, a pull request's or a merge's, before and after the change, and again
once it has reached main.
