- Added retained proof for connected lifecycle runs. `readmit report connected
  assemble` keeps actual v2 and FHIR lifecycle results in a sealed
  customer-local packet (`readmit-retained-packet/v2`) that verifies and
  re-analyzes offline on another machine, names any changed snapshot, check
  set, response, pin, environment or completion record by its evidence
  surface, and reports current reanalysis apart from the original verdict.
  `report connected compare` separates changed check definitions, inputs,
  environment, protocol boundary, target, profile and validator, completion
  policy, collector and engine, and matches observed records by their declared
  keys with multiplicity. `report connected export` and `review` render a
  typed report in the five existing formats. `report connected extract`
  publishes a value-free extract only when a reviewed policy maps every
  evidence surface and no token or key is retained; an extract is never
  described as an equivalent reproducer, and a reproduction claim needs a
  retained replay failing the same way. v1 packets and reviews are unchanged.
