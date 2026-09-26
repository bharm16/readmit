- The window's fixture check of a scenario library regenerates the pinned
  plan in memory and no longer writes its private regeneration into the
  system temporary folder, so a check — passed, failed or cancelled — writes
  nothing anywhere (#481).
- Generating a scenario in the window writes the generated case through the
  one engine operation that owns the case's provenance, no longer rereads the
  generated streams from the folder it just wrote, and syncs the generation
  directory so a completed generation survives a power loss (#481).
- Opening or saving a document that declares neither the scenario nor the
  order-scenario contract is now refused by name in the window, instead of
  being tried against each reader in turn (#481).
