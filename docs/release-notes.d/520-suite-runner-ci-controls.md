- The suite editor keeps every expected override you did not edit through
  preview and save, and an override that is not typed JSON now stops preview
  and save beside its row instead of being dropped; the edit is kept in the
  retained draft as typed and stays with its row when the row or its table is
  renamed (#520). Each suite picker offers only the artifact it needs — suite
  definitions, prepared suites, release pins, test releases or coverage files
  — so Release file, Earlier release and Later release now choose from the
  workspace's test releases, and a partly filled release pin, requirement or
  exclusion is reported instead of silently left out of what is saved. Rows,
  tables, tests, environments, bindings, pins, requirements and exclusions
  each have their own remove control, and an item another part of the suite
  names shows those references before it goes. Go to runs opens the suite with
  the environment it was prepared against and names the prepared folder and
  release pins you prepared with, stating that the run view does not apply
  those pins; changing the preparation or promotion inputs withdraws the stale
  handoff or approval.
- The runner view is split into Status and jobs, Configuration, Access grant,
  Recovery and Update verification (#520). Send job is offered only for a
  fresh successful preview of the current runner configuration and job file,
  whose prepared input ID is shown read-only, and Cancel job appears only
  while a job runs. Opening a schedule policy file now loads its schedules
  into the editable draft (asking first over unsaved edits) and says so only
  while the draft still matches the file, changing a notification URL
  withdraws its approval, and removing the last schedule leaves an empty draft
  that says the installed policy is unchanged. The CI handoff separates
  Generate workflow, Inspect results and Verify gate, and groups the paths on
  the CI agent apart from the workflow file written on this computer. Field
  labels throughout say what each value is, where it lives and its exact
  format.
