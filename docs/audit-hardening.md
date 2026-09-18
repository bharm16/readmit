# Architecture audit hardening

The 2026-09-18 full audit examined all 195 tracked files at
`806fa6d9a22e5cb1c458453ea218b0b02b852f82`, including complete reads of all 116
source and test files. This delivery implements its four Strong findings.
Existing artifact versions, byte identities, strict JSON, independent fixture
expectations, and the single-binary release contract remain the authority.

## Required behavior

1. **Derived fixture proof.** Reopening an export must reject invented ledger
   values even when both retained observation copies, result assertions, case
   metadata and file identities are coherently resealed. Source occurrences,
   canonical ACKs, the unchanged requests, empty initial state and the declared
   built-in receiver mode must support the final ledger. The same proof check
   runs after fresh execution and during export reopening. This checks internal
   consistency; it does not authenticate the files or repeat the private
   known-value scan. Independently authored expected ledgers remain separate
   from the receiver implementation.
2. **Artifact paths.** Output reservation resolves raw filesystem traversal
   before lexical joins and refuses writes inside retained case, run, result,
   synthetic-family, review and report evidence. The public writer still owns
   exclusive creation. Strict directory readers retain their root-symlink
   refusals and open the physical artifact named by a path containing
   `symlink/..`. A receiver rejects colliding case and observation destinations
   before accepting a session, including aliases on case-insensitive filesystems.
   Capture provenance identifies the physical source it actually read. Export
   orchestration retains resolved review/private roots for all subsequent child
   access. Relative CA references preserve raw traversal, and replay preparation
   snapshots the explicitly selected physical certificate file.
3. **Diagnosis interpretation.** Repetition support belongs with field-state
   selection and decoding. Selecting a different set of diagnosis rules cannot
   cause a required scalar's first unexpected repetition to gain meaning.
   Empty/null first repetitions are also unsupported; ACK reference fields
   follow the same rule. The intentional first-PID-3 policy is preserved.
4. **Case reconstruction limits.** Rebuilt event and correlation records use
   the writer's incremental 16 MiB record-file budget before comparison.
   Small stored inputs cannot trigger unbounded whole-graph serialization just
   because many ACKs ambiguously reference many same-ID messages. Ambiguous
   correlations are still preserved when they fit the existing artifact limits.

Regression tests exercise the existing public export, command, receiver,
diagnosis and case-reader interfaces with real temporary files and loopback
connections. The new failure cases were observed before their fixes. Tests that
deliberately forge retained evidence construct replacement cases in separate
staging directories and install them directly; production writers do not gain
an exception for modifying sealed evidence.

## Remaining audit candidates

**Occurrence indexing: implemented.** The receiver now uses the case module's
existing next-occurrence policy. Its duplicate MLLP suffix splitter is deleted.
The transport-chunk adapter still owns direction and observed-time reconciliation;
network framing retains its distinct fail-fast behavior. Existing partial-ACK,
buffered-read-ahead and malformed-capture tests protect this shared policy.

**Snapshot composition: deferred.** Report and export verification still reopen
nested evidence through their verified readers. Sharing one in-memory snapshot
would require changing interfaces across case, run, result, diff and diagnosis
modules. No accepted inconsistent packet or established latency requirement
justifies that expansion in this change. Retain the existing semantic checks;
revisit when a measured workload or consistency requirement supplies a concrete
acceptance test. This decision does not claim concurrent filesystem replacement
is safe or that large artifacts have been benchmarked.

No ADR needs reopening. The changes enforce the existing evidence and
interpretation contracts rather than introducing a new artifact format,
database, rules engine or storage abstraction.
