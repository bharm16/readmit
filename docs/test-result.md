# Test result directory: readmit-result/v1

```text
NEW_RESULT/
  result.json
  spec.json
  initial-observation.json
  observation.json
  identity.sha256
  run/                       # independent immutable readmit-run/v1 bundle
```

The spec is the exact input bytes. Observation files are exact captured live-file
bytes, including a bounded partial/invalid snapshot when useful for diagnostics.
ACK-only results omit both observation files. Early execution/configuration errors
may omit the run, observations, or an unreadable spec; the matching result references
are null. A missing observation is never represented as an empty successful ledger.
The runner writes observations and result **alongside** the finalized run, never
inside it. All files request mode 0600 and the new directory mode 0700.

`result.json` records:

- `schema:"readmit-result/v1"`, `state:"complete"`, `contains_source_values:true`,
  and `export_policy:"customer-local-only"`.
- `status`: `pass`, `assertion_failure`, or `execution_error`; `error_class` is a
  fixed diagnostic class or the empty string. No arbitrary operating-system error
  or source path appears in a class.
- `spec_identity`: SHA-256 of exact spec bytes and `spec:{path,size,sha256}`.
- `input_bundle_identity`, and `run:{path:"run",identity}` when replay completed.
- `target`: the actual sealed replay target record (including CA digest), and
  `target_identity`: SHA-256 of deterministic target-record JSON plus LF. This is
  configuration identity, not software-version identity or endpoint authentication.
- `observation_boundary`, `initial_observation` and `final_observation` descriptors.
  Each descriptor is `{path,size,sha256}`.
- `receiver_session_id` and `receiver_mode` for a fully receipt-bound ledger result;
  empty for ACK-only or execution-error results. These distinguish controlled
  fixture sessions and modes independently of transport configuration.
- Ordered `assertions`, each containing the original `assertion` including its
  expected value, typed `observed` value or null, and `status` (`passed`, `failed`,
  `not_evaluated`). An execution error leaves every assertion unevaluated.

The completion marker is written last. Identity is SHA-256 of
`readmit-result/v1\n`, then every relative path and content in bytewise sorted path
order, including all files under `run/` and its own identity marker. Only the root
`identity.sha256` is excluded. Each path and content has an eight-byte unsigned
big-endian length prefix. The marker is the lowercase digest plus LF. Identity is
independent of filesystem timestamps and absolute paths; it detects content change,
not authenticity. No executable reset hooks are stored.

## Verified offline Go reader

```go
artifact, err := testrunner.Open(resultDirectory)
// Handle err before using artifact.Result, artifact.Spec, artifact.Run,
// artifact.InitialObservation, or artifact.FinalObservation.
```

`Open` verifies bounded regular files, rejects symlinks/unexpected files, checks
directory and referenced file identities, opens the run through `replay.Open`,
checks source selection/target mappings, rejects replay transformations and intended
payload changes forbidden by v1 specs, validates same-session receipts and the
exact ordered observation list, and reevaluates all assertion results. It never
opens the original case, target, or live observation path, and never sends bytes.
Copying the complete result directory preserves its readability without the
original environment. Early errors are retained diagnostic claims; unavailable
external files cannot be independently reconstructed by this offline reader.

`Prepare(specPath)` is local-only and returns a sealed preview plan. `Execute(ctx,
plan,newDirectory)` executes that plan; `Run(ctx,specPath,newDirectory)` additionally
persists local preparation errors when storage permits. Transport outcomes are
machine results, not Go errors. Go errors indicate inability to produce verified
complete evidence (including invalid output placement or storage failure).

Bounds: 1 MiB spec, 8 MiB result, 8 MiB per observation, 128 MiB complete directory,
and at most 16016 files. Large repeated assertion output produces the execution
class `assertion_evidence_limit`; original run and observation evidence remain.
Original results, specs, observations, ACK/ERR text, and replay transformation
records contain local source values. Share-oriented packets must be generated from
approved derived artifacts with new test executions, never copied from these
original files and relabeled.
