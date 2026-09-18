# Field-label provenance

`internal/dictionary/fields-v251.json` contains field-position/name pairs adapted
from the v2.5.1 segment classes in
[nHapi revision 2495edd1e23a85ab9146cb03947c17d45120cf1f](https://github.com/nHapiNET/nHapi/tree/2495edd1e23a85ab9146cb03947c17d45120cf1f/src/NHapi.Model.V251/Segment).
Only the numbered field labels were extracted from each class's field-list
documentation. The format was changed to JSON; no parser, model implementation,
clinical definitions, cardinality rules, or code-system tables were copied.

The source repository distributes these files under
[Mozilla Public License 2.0](https://github.com/nHapiNET/nHapi/blob/2495edd1e23a85ab9146cb03947c17d45120cf1f/LICENSE).
The adapted JSON is distributed under the same license. Its complete preferred
source form is that JSON file; it is included alongside the executable in every
archive as `dictionary/fields-v251.json`. The full license is included at
`licenses/nhapi-MPL-2.0.txt`. Original nHapi authors retain their applicable
copyrights; the adaptation does not claim ownership of the HL7 standard.

The field-label set covers MSH, MSA, ERR, PID, PV1, SCH, EVN, NTE, RGS, AIS, AIG,
AIL, AIP, and OBX. It is selected only when MSH-12's version component is `2.5.1`.
Unknown positions and other versions use positional names. This finite label
set is not a conformance dictionary or the future SIU semantic fixture profile.

Syntax references: [HL7's MLLP block format](https://hl7.eu/refactored/transport01mllp.html)
and [v2.5.1 delimiter rules, including optional omissions](https://hl7.eu/HL7v2x/v251/std251/ch02.html). These pages are
references, not bundled content. Future externally sourced definitions require
their own provenance and redistribution review.

[D1](product-decisions.md#d1--profile-metadata-and-supported-meaning) selects the
sources for the future multi-version library. That decision does not expand
the contents, rights assessment or supported meaning of this existing label
set; #45 must record and review each new extraction separately.
