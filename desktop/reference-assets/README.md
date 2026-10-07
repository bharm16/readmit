# Built-in HL7 definitions

The desktop build embeds `library.zip` here. It contains all 14 supported HL7
editions and is validated by `tools/referencebundle` before compilation. A native
package must pass `readmit-desktop --check-hl7-library`; a source checkout without
the complete library is not a complete desktop distribution.

`make install-desktop` builds this resource from the maintainer's retained library
and removes the staging file afterward. `--reference-library` (or
`READMIT_HL7_REFERENCE_LIBRARY`) selects a supplied build input. This is a build
step, never a customer setup task or a runtime download.

Source catalogs retain their exact provenance and coverage gaps. The source
material and generated ZIP remain outside Git while the exact-content
redistribution decision in #627 is pending. Bundling for the owner's local app
does not record permission to publish that content.
