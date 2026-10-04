---
status: accepted
date: 2026-10-03
---

# Interface specifications keep exact local-profile revisions and separate documents

An interface specification is a project-scoped authored record, separate from
message evidence, receiver commitments and executable local profiles. The strict
`readmit-interface-spec/v1` document records the exact owning project identity,
a name, one selected local ProfileItem catalog revision and its schema/identity/
version/content hash, and bounded copied UTF-8 documentation with its source
name and byte hash. Documentation is descriptive provenance and never becomes
an executable constraint automatically.

The existing catalog whole-object revision owner publishes specification edits
atomically under an intent and base revision. Old revisions remain immutable.
The profile's supported editor and explicit evaluator remain the owners of
constraint edits and evaluation; selecting a newer profile revision requires an
explicit specification save. Neither action changes historical test/evaluation
pins, original messages, a partner document or an external receiver.

Contextual entry from a field/finding keeps exact source identity, occurrence
and selector. Go checks profile applicability against the original declared
edition/family, keeping unavailable and undecided state truthful. Base metadata,
local/overridden rules and descriptive documents retain separate provenance.
No cross-project inheritance, attachments-as-rules or general requirements
comparison policy is adopted. New member meanings require a new schema and an
explicit reader, including empty/null membership at version boundaries.

Documentation is plain text, at most 64 KiB per document and 16 documents per
specification. Reads are local and bounded, render no executable HTML, and
perform no automatic acquisition. Back returns through the existing source
navigation owner. Private sessions retain source navigation, not message values
or copied specification/document contents.
