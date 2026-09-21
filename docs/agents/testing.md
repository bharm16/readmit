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

## Before pushing

Batch related review fixes, resolve integration changes against current main,
and run the affected tests. Run `make test` once on the final local revision.
It runs the full race suite with small resource fixtures and the actual 8 MiB
observation boundary once without race instrumentation. The small boundary and
production boundary assert the same retained-prefix and refused-ACK behavior.
`make test-boundary` addresses the latter directly when that contract changes.

Timed fuzz campaigns belong in CI unless a fuzz failure or target change needs
local investigation. Normal Go tests still exercise seed corpora. For a focused
campaign use `python3 tools/fuzz.py --package ./internal/hl7`; `--list` shows the
selected targets. CI discovers every target and partitions it across three
shards; adding a fuzz function does not require editing a workflow list.

Run `make test-tools`, `make verify`, or `make mutate` locally when changing those
tools or their checked contracts. The verifier and mutation runner also support
`--only NAME`. Every full verification and mutation check remains required in CI.

After a rebase, rerun tests for the integrated changes, then rely on fresh CI for
the final revision. If a failure appears unrelated, retain its exact output and
diagnose the individual failing test. Repeated package passes do not explain a
flake or establish that a failing revision is correct.

## CI and merge

The `quality` check aggregates Go tests, tooling/independent verification and
mutations, all fuzz shards, and the vulnerability scan. It fails if any of those
jobs fails, is skipped, or is cancelled. Packaging and the five native smoke
tests run concurrently, and test the exact archives later used for publication.
The desktop module builds and scans separately, and `desktop` is that workflow's
equivalent stable aggregate: it requires the macOS shell build and the five
`desktop-package` jobs plus the five `desktop-install` jobs, which download,
install, check and remove the exact unsigned artifacts on fresh native runners.
The macOS shell job also executes the frontend behavior tests and publishes
their output as an artifact, so a failing component test fails the `desktop`
aggregate rather than only a developer's local run.
It fails the same way if any of them fails, is skipped or is cancelled. Release credentials remain exclusive to
trusted tag runs; no signing credential reaches any workflow.

Wait for `quality`, `package`, all five `native-smoke` checks, and `desktop` on
the final PR revision before merging. That list is unchanged by the desktop
packaging jobs, because they sit behind `desktop`. Require those stable contexts
in the repository's main-branch protection. After splitting jobs, keep `quality`
and `desktop` as the stable aggregates of their workflows so another worktree
never has to guess which new job names are mandatory. Each PR cancels only its
own superseded workflow runs; main and tag runs keep independent groups.

Each job has its own Go cache scope, including each fuzz shard. Keys include the
compiler, platform, dependency checksums and commit; a prefix restores the prior
generation before the current one is saved. This keeps compiled tests and fuzz
corpora from being overwritten by the faster packaging job. A first cold run
still needs compilation; assess steady-state savings on later commits as well.
