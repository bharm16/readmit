- Author connected tests in New test: choose Engine output or Application
  records, then order v2 messages and FHIR requests into phases in Inputs,
  each phase reading named observations, and add typed checks of the records
  they read, selected by business key. Conditional create, version-aware
  update and server-assigned identities are typed choices; nothing is typed
  as a URL, script or file, and Create test sends nothing.
- A saved connected test reopens whole. An observation, case, environment or
  FHIR server that changed since the test was written marks the inputs and
  checks it affects until the author resolves them; nothing is removed.
- Import test opens an existing connected lifecycle in the editor. A check
  the editor does not represent stays read-only and is saved back exactly as
  written.
- Add connected tests to a suite, over a dataset whose rows override their
  expected values if needed, and bind each suite environment to its own named
  environments and FHIR server. Approve version records the baseline of every
  test's expectations; every environment keeps the same expectations, and the
  approved version runs in the connected runner.
- Suggest checks proposes typed checks from a completed run of the same
  definition, undecided until accepted, and never a server-assigned identity
  as an expected value.
