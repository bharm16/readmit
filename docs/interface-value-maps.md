# Interface value maps

Open **Value maps** from a retained HL7 field. Create a named record with the
actual HL7 edition, exact source and destination selectors, both field meanings,
and provenance. Selectors retain segment occurrence, field repetition and any
component or subcomponent position. An optional four-digit table identity and
its edition/provenance do not extend the map to another field.

Add entries with an authored source value, destination value, both meanings,
and a reference or note. Each source value has one destination. **Save map
revision** publishes one complete catalog revision. Editing saves a new revision;
old revisions remain readable. Conflicting bases refuse publication and keep
the draft. Maps record declarations; they never rewrite source messages, apply
a transform, deploy to a receiver or alter an external engine.

**Import CSV** validates one bounded UTF-8 file into an unsaved draft. Save it
explicitly. Malformed rows, duplicate sources, changing scope/metadata between
rows and invalid selectors reject the complete import. **Export CSV** writes to
an explicitly chosen new file and refuses to overwrite an existing file. The
header carries name, edition, selectors, field meanings, map provenance, optional
table identity/edition/provenance, entry values/meanings/provenance, and exact
association declarations. Use an exported CSV as the format template. Files are
limited to 2 MiB and 2,000 entries; values, meanings and notes to 4,096 bytes each.

**Choose configuration declaration** can explicitly associate an exact retained
target configuration revision. The catalog also accepts exact retained run or
test revision declarations. None proves external deployment or changes which
run executes. Cross-interface inheritance is not applied.

Contextual mapping starts hidden. **Show contextual mapping for this source**
reads through the shared Go parser against the original retained source identity,
exact selector and actual declared edition. Unmapped values have no recorded
destination; they are not predicted receiver errors. Hidden, absent,
undecodable, incompatible and out-of-scope states stay distinct. A source or map
revision change resets contextual reveal.

Incomplete authored maps and entry forms use the existing bounded private draft
store. Retention failure remains visible with a retry. After navigation or
restart, restore the draft from **Projects → Drafts to restore**, or reopen the
same original field. Changed or missing sources retain readable authored work
without repinning to other evidence. Leaving, replacing or importing over an
unsaved map asks for explicit discard. Saving or discarding removes the private
draft through its acknowledged retention owner.

**Return to selected message** restores the original occurrence and field with
values hidden. Private drafts retain authored declarations and source IDs;
inspection values, consent and source bytes are never stored in navigation or
session documents.

A map JSON file placed directly in the project remains visible with an
unretained-state reason. Use validated CSV import and an explicit whole-map save
to publish a revision. Discovery does not invent revisions or rewrite files.
Malformed current declarations, foreign-interface records and future schemas
remain visible with distinct reasons and no unavailable revision control.
