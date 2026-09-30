- Create variant makes a smaller or edited case from the open one without
  changing it (#558). It opens on the messages chosen in the list, or on the
  included-message picker when none is chosen. Included messages are on the
  left with Dependencies for linked ACKs and earlier messages with the same
  identity; the ordered changes are on the right: Replace value, Clear field,
  Rebase identifiers, Shift dates, Move entry, Duplicate entry and Exclude
  entry, each with only the fields its type takes, Move up, Move down, Remove
  change and Undo. A change the evidence does not support is refused and the
  variant stays as it was. Preview shows the actual difference — messages in
  and out, every changed field before and after, relations kept or broken and
  profile support — and a broken relation blocks Save. Save variant publishes
  the variant with its lineage in one save and opens it; a variant with
  sequence changes is written under the new `readmit-transform/v1` derivation.
  The reproducer panel, its output folder and its separate Add to project form
  are gone, and so are the transformation plan editor and preview in Reports,
  which is now Export review.
- Compare in a case's menu compares it with another named case or variant
  (#558): Field, Earlier, Later and Change, records matched only by the record
  keys you choose, and every message only one side holds or that an ambiguous
  key matched kept as its own row. A named normalization policy — rules that
  ignore a field's difference or compare it as a timestamp or a number — is
  saved as an object of the project and decides which differences are shown;
  Original differences shows every raw one again. A variant shows its lineage
  and plan changes, and Run evidence opens the runs of either case in Compare
  runs. The collection comparison, revision comparison and policy file editor
  panels are gone.
- Minimize failure in a failed run's More menu reduces the run to the fewest
  messages that still fail the chosen checks (#558). It starts from the run's
  exact test version, failed checks and environment; you choose the checks to
  keep failing, per-message or linked grouping and the trial and confirmation
  limits, and one review of the whole series — environment, reset actions,
  grouping and bounds — is the consent to it. The trials, the one running and
  Stop are shown while it runs, and the sidebar keeps it while you work
  elsewhere. A failed reset or an uncertain delivery stops it; nothing is
  retried or resent. Only a reduced result claims a minimum and opens as a
  variant; a search that reached its limit or was stopped claims nothing. The
  controlled reduction panel is gone.
