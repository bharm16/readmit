- Added synthetic connection examples for three test topologies — v2 through an
  engine to a downstream v2 capture, v2 through an engine to the application's
  FHIR state, and FHIR-native input with a declared downstream record. Import
  example in Environments asks for each marked placeholder and saves the
  environments, capture listener and observations through their editors'
  validation; nothing is approved, connected or read from a credential store on
  import. The new connection recipes page covers placement and network paths,
  least-privilege identities, runner operating procedures and what each verdict
  shows.
- The optional local FHIR validator now moves between machines as an offline
  package installed only under its published identity: `readmit validator
  verify`, `install`, `check` and `remove` on a runner host, and Check
  validator, Install package and Remove in a FHIR environment's menu on the
  desktop. FHIR environments select the installed validator, and connected
  runner suites check it before any effect.
