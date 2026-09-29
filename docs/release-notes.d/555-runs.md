- Runs is now the project's run history (#555): each run by the test or suite
  and the exact version it executed, the named environment it reached, when it
  started, how long it took and one result — Running, Interrupted, Incomplete,
  Blocked, Error, Failed or Passed, with Delivery uncertain beside it when true.
  A running run is listed first. Filter narrows by result, environment, test or
  suite and start date; Compare opens two finished runs of a test; Schedules
  opens the runner schedules. The run form, the run folder field, Preview run
  and the separate run details panel are gone.
- Running a test, a suite, the remaining messages of an interrupted run, a
  reviewed test or selected messages is one reviewed send (#555). The review
  shows the exact version, the named environment and its address, the messages
  in send order, the manual setup to mark complete and any reset that runs
  first, with one line saying what Send sends where; a suite shows each test's
  dataset, target, state sharing and dependencies. Nothing is sent until Send,
  a change to what the review showed refuses the Send as stale, and the same
  click never sends twice. A production or unclassified environment is refused
  with Edit environment, and an inactive license with Activate, which reviews
  the run again afterwards.
- A run opens on its own page (#555): while it runs, the target, its progress
  and Stop, which the sidebar keeps while you work elsewhere; once it ends, its
  result, its checks failed and undecided first with what each expected and
  observed — an observation that was not made is Unavailable, never zero, and
  text is Hidden until Show values — the messages sent and each acknowledgement,
  and Details with what it ran against and, for a run that did not finish, which
  messages were acknowledged, uncertain or never attempted. Resume remaining
  reviews only the never-attempted rest when that is safe; Clear stale lock
  removes a lock an ended run left and keeps its evidence. Create report, Run
  again and Analyze with checks, which decides a saved check group as its own
  analysis without changing the run, start from the run.
- Compare runs puts the earlier and later run side by side (#555): each check
  aligned by its identity and definition — a changed definition is a changed
  check, never a regression — what each run was run with compared part by part,
  and up to fourteen more runs of the test counted for Stability.

Reviewed desktop runs retain the selected publication in a bounded, strict
`readmit-run-origin/v1` document beside the project catalog, before any send.
The record binds the retained input and preserves the test or suite version
and displayed names across later edits. Older runs with ambiguous equal
publications keep their historical publication unavailable. Suite reviews
include inherited environment resets; a suite job has read-only recovery
and scoped stale-lock cleanup, while a new suite execution needs a new review.
