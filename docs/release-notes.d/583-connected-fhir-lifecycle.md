- Added connected FHIR lifecycle tests (`readmit-connected-test/v5`). One test
  can send HL7 v2 through a real integration and check the FHIR or application
  state it produced, or send reviewed FHIR R4 requests and check their declared
  downstream effect. Server-assigned IDs and versions come only from actual
  responses and only address later requests. FHIR observations read complete
  paged searches and state whether they read an authoritative application API,
  a delayed replica or a reference FHIR store. Response checks, optional local
  validation and downstream checks are reported apart; a 2xx response or a
  valid resource is never downstream success. Capability, FHIR version and
  authority mismatches stop before any effect, a lost write response is never
  resent, and retained results re-evaluate offline. The existing `test`,
  `run status` and `connected prepare` commands run it; v1–v4 tests and results
  are unchanged.
