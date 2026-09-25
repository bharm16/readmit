---
status: accepted
date: 2026-09-25
---

# Until the product works, CI runs only correctness checks

Pull requests and pushes to main run the checks that say whether the code
works: the Go suite with race detection (`go-tests`), the hub against
PostgreSQL (`hub`), and the frontend build and behavior tests with the desktop
shell's Go build and tests on macOS (`desktop-shell`). `quality` and `desktop`
remain the two required aggregates.

Everything that proves a release rather than the code runs only when the owner
dispatches a workflow by hand: the release archives and their five native smoke
tests, the five native desktop packages and their five fresh-runner
installations, tooling tests with independent verification and mutation checks,
vulnerability scans, timed fuzzing, and journeys. A `v*` release tag also runs
every gate of `ci.yml` before `publish`. Nothing runs on a schedule.

## Why

The product does not work yet. No package is signed, published or installed by
anyone, yet every push ran 23 checks, 15 of them building and installing
unsigned preview packages on five operating systems. Each check was added by a
ticket written as release acceptance (#1's artifact matrix, #171's desktop
packages, #218's fresh-runner installation) and then became a required check
on every pull request.

Measured over the pull-request runs of 2026-09-19 to 2026-09-25 (161 pull
requests, 295 pushes): about half of the failed checks were in the packaging
and installation jobs; 80% of repeated runs were rebases, each repeating all 23
checks; and 9 runs failed and then passed on the same commit when re-run. A
single run took 6 to 10 minutes; the pull requests that failed at least once
spent a median 39 minutes in CI and used 79% of all pull-request CI time.

## Consequences

- A change that breaks packaging, installation or a native smoke test on one
  platform is found on the next manual or tag run, not on its pull request.
  While nothing ships, that is cheap to fix.
- A dependency with a new advisory is found on the next manual or tag run.
- The desktop shell's Windows-only code (the WebView2 focus guard) is compiled
  and tested only in the Windows package job of a manual run; development
  happens on macOS, which never compiles it.
- A change to `tools/` is exercised by `make test-tools`, `make verify` and
  `make mutate` locally or on a manual run; CI does not run them on a pull
  request.
- A release tag still runs every gate, and `publish` still requires them, so
  this decision never lets an unverified archive be published.
- Required status checks on main are `quality` and `desktop` only.
- ADR-0001's release matrix is unchanged; its CI smoke test runs on manual and
  tag runs under this decision.

## Reversal

Revert this decision when the product works end to end for a person using it,
which is the owner's call. Reversal restores the pull-request and push
conditions of the gated jobs in `.github/workflows/ci.yml` and
`.github/workflows/desktop.yml`, adds `package` and the five `native-smoke`
contexts back to main's required checks if wanted, and marks this record
superseded.
