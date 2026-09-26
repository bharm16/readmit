- The profile editor lists each segment's fields by selector, position, field
  name, usage and rule origin, and edits the selected field's position,
  condition, repetitions, data type and rule bindings in its own detail panel.
  Terminology sets, assigning authorities and date rules each have their own
  editor, and a field's terminology set, assigning authority and date rule are
  chosen separately by their declared IDs; an edit of one binding no longer
  writes the terminology member. A rule a field still uses is not removed or
  renamed silently: the fields that use it are named. Conditional values are
  authored one by one and a placeholder value is never saved; repetitions offer
  Not specified and Unbounded, neither written as zero; a segment is added from
  a labelled row instead of a prompt, and removing one that holds field rules
  asks first (#518).
- Updating a saved test's profile pin now pins the later compared profile's own
  verified version seal, read through the profile reader, and shows the old and
  new exact identities first. It is withdrawn when a compared profile or the
  test references file changes, and no longer uses the digest of whichever
  profile was last validated (#518).
- In the scenario panel the scenario definition and the generator plan are
  separate documents with separate drafts; Generate no longer offers the
  scenario definition as a plan. Editing a definition withdraws its preview,
  editing a plan withdraws the generated case's handoff, opening a scenario
  over unsaved changes offers Save, Discard or Keep editing, and malformed text
  stays editable without a profile binding being written into it. A library
  file to import is chosen with Browse, and a template's plan is sent from a
  saved plan file or its canonical JSON, as chosen (#518).
- Profile and scenario controls are named for what they hold: files are
  "file", folders "folder", usages and date, offset and binding values carry
  their meaning beside their code, results are headed Validation, Saved
  revision or Save result, and the export confirmation is cleared whenever an
  export input changes (#518).
