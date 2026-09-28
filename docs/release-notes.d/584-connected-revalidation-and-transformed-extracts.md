- Added explicit revalidation of retained connected proof. `readmit report
  connected revalidate` runs the installed validator again, locally and with
  networking disabled, on the exact resource bytes a packet retained. It runs
  only when the installed capability is exactly the historical pin, and writes
  a separate `readmit-connected-revalidation/v1` beside the packet. That
  analysis says whether the new outcome agrees with the historical one, or why
  a validation was not run again. Opening or verifying a packet still starts
  nothing, and historical verdicts never change.
- Added reviewed transformed extracts. A
  `readmit-connected-disclosure-policy/v2` keeps or pseudonymizes named FHIR
  elements, typed columns and query parameters. Everything else is excluded,
  and narrative, extensions, attachments, non-FHIR bodies, v2 messages and
  validator diagnostics can only be excluded. `report connected extract` then
  publishes a `readmit-connected-extract/v2` of the transformed resources,
  requests, records and identity mappings, with Markdown and HTML renderings.
  Pseudonyms are keyed by a customer-local file from `report connected
  pseudonym-key`, so references and identifiers stay consistent. Every
  transformed item is marked as derived. The value-free v1 extract is
  unchanged. Neither extract is an equivalent reproducer or a
  de-identification.
