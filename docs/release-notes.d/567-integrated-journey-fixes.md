- Choosing an activation folder that was already activated, on this computer or
  by the command line, now selects it instead of refusing it.
- A test created straight from a case's messages now offers environments, cases
  and check groups saved since the Tests list was last shown.
- A File export observation can point at a receiver ledger handoff
  (`readmit-observation/v1`), including an empty one, so a test can read its
  appointment records.
- The review of a run, share or other final action no longer shows "another
  operation is already running" when only a background read held the
  application for a moment; it is prepared again. Compare runs and support
  summaries recover the same way.
- A share whose redaction rows are all resolved no longer stops the Share
  report page from drawing.
- New check groups, profiles and scenarios in Library open in the project that
  is open, not the one open when the window started.
- Import shows Stop while a case is being written, and a large MLLP feed is
  previewed frame by frame, as the import reads it.
- A project search that matches nothing says No matches, and a Files page that
  cannot be read shows why instead of an empty list.
- Team shows the hub's reason when it cannot list projects, and a remembered
  team configuration that no longer validates opens a fresh Connect team sheet.
- An action pressed while the window is reading on its own, such as Create
  test, Save, Test connection or Send, now waits for that short read to end
  instead of being refused with "another operation is already running".
- Editing a credential reference no longer overwrites a change made to it
  elsewhere while the edit was open.
- A variant saved twice from the same click is published once.
- Switching projects while Suites is loading drops the previous project's
  delayed result; Files also clears the previous project's read error.
- Stop also cancels an action waiting for a background read, before it starts.
- A late navigation read no longer steals typing from an open sheet.
- Team sign-in finishes its browser response before closing the callback listener.
- A minimization keeps its progress and Stop control when a read from an earlier
  visit finishes late. Restoring a project note does not wait for the case list.
- A template-required privacy check reads the original project's observation
  when running derived messages, instead of looking beside the portable spec.
- Retained-run analysis has a Stop control that leaves the saved run unchanged.
- Filter dropdowns and list toolbars use their shared heights in native WebKit.
- Saved capture sources show their full responder, quota and retry details;
  scenarios retain whole-plan JSON editing, and Storage offers a read-only
  compatibility preview.
- A claimed schedule occurrence shows Pending result until its outcome is known;
  claiming a slot alone no longer labels it Running.
- Opening a project no longer leaves Cases on Loading when another read keeps
  the catalog busy; a refused read explains why and offers Retry.
- Add runner refreshes the project's environments when opened, retains known
  choices on a read failure, and offers Retry instead of silently emptying them.
- Opening a saved test reads its draft before refreshing choice lists; a failed
  opening read now offers Retry instead of leaving the editor on Reading.
