# Interface specifications

Open **Interface requirements** from a retained HL7 message or a finding to
read the specification associated with that interface project. A finding with
no occurrence or field opens the whole retained case's specification; Readmit
does not invent a message, code, or field for it.

**Add specification** records a name, one exact saved local profile revision,
and optional UTF-8 `.txt` or `.md` documents. Each document is copied into the
saved specification with its SHA-256 byte identity. A document is limited to
64 KiB; a specification holds at most 16 distinct documents. The documents
remain descriptive provenance and do not become executable constraints.

The supported profile editor opens the pinned revision. Saving an edit creates
a new profile revision. The specification continues to reference its original
revision until **Associate an edited profile revision** explicitly saves a new
specification revision. Old revisions remain readable. Conflicting saves refuse
publication and keep the previous revision.

For a selected field, the displayed provenance separates base metadata and
local profile declarations. Applicability uses the actual retained message's
version and family. An incompatible declaration does not supply another
interface's constraints. Evaluation is a separate explicit action through the
existing profile evaluator; unavailable or unsupported coverage remains
visible. Documents and local declarations are not proof of receiver requirements
or external deployment.

**Return to selected message** restores the retained source and field, or the
originating finding, with values hidden. Saving specifications or profiles does
not rewrite original source messages or change an external engine.

A specification file placed directly in the project remains visible with an
unretained-state reason until a specification is explicitly authored and saved.
Discovery creates no revision. Invalid current documents and unsupported future
schemas stay distinct; the original files are kept, and no revision-dependent
editor is offered for them.
