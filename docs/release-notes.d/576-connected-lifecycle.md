- Connected v2 tests can now prepare and execute an authorized setup, ordered
  phases with intermediate checks, full observation boundaries and guarded cleanup
  through one engine call and the existing test/run CLI. Results retain complete
  step/check denominators, original inputs and exact observation provenance. Explicit
  owned-resource transition policies and fresh-authority continuation of a proven
  never-attempted phase suffix preserve cleanup guards and prevent automatic replay.
