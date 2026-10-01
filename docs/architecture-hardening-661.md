# Full-audit hardening (#661)

The September 30 audit covered the tracked tree at `8b66a965`. Eight Strong
findings are implemented together. Existing protocol contracts, independent
fixture oracles and external qualification gates keep their meanings.

| Finding | Owning module | Observable verification |
| --- | --- | --- |
| Credential-aware response release | `internal/networkaction` | Real TLS targets return raw, JSON-escaped and distinct padded/unpadded base64/base64url fixture echoes; both adapters withhold body/header echoes, retain the actual response status and accept clean controls. |
| Visible choices own a review | `reviewedAction.ts` | A parked A preparation cannot replace B or supply the final token; obsolete reviews are withdrawn. An awaited Send preflight cannot spend a token after the visible choice changes. General and Send reviews use one lifetime. Go still owns single-use authority and input revalidation. |
| Project/object read ownership | `ownedRead.ts` | Older success or refusal cannot populate the current project or object. The frontend tests establish independent hook lanes; `TestReadsAreAnsweredWhileACaptureRecordsAndWritesWait` establishes actual catalog/draft admission beside a recording capture. Grouped draft/observation replies share one issued context and commit only in their owning lane. |
| Whole approved queue | `internal/runqueue` | Dependencies, isolation and parallelism change the pin. Real managed dispatch refuses a changed dependency before the target sees a frame or a local job is created. Dispatch uses the existing scheduler; real target witnesses establish B-before-A and retained skipped jobs after refusal/uncertainty. |
| Owned retained report | `internal/report` | Rendering and portable export work from the verified reading after source relocation; separately supplied folder/packet mismatches refuse. Caller edits to summary metadata, nested expected/observed/comparison values or returned bytes do not change the reading. |
| Owned lifecycle evidence | `internal/connectedrun` | Captured v2/FHIR lifecycles verify and project tables/responses without their source folder; altered captured bytes refuse. Child readers use bounded captured-byte verification. |
| Durable publication | `internal/artifactdir` | Profile import syncs its completion and both directory entries, reports sync failure and refuses retry into existing output. Generation records use exclusive whole-document publication; new generation directories sync their parent after the record, report sync failure and refuse retry into retained output. |
| Coherent demo acceptance | Shared authored demo scenario | DOM and native drivers consume `testdata/acceptance/demo-scenario.json`; the real-facade journey checks creation, fail/pass, comparison, reopen, report and original-evidence export with independent CLI readback. |

## Compatibility

`readmit-prepared-queue/v2` is the identity domain for full queue configuration
and prepared inputs. Queue and schedule documents retain their historical
readers. A historical partial schedule pin will not match the new identity:
revalidation pauses that schedule for a new explicit preparation/review. No
pin, approval or retained artifact is silently rewritten. The legacy customer
runner still dispatches at its capacity of one environment lease.

Retained report summary fields remain useful projections. Their privately owned
reading is authoritative for rendering and sharing. `RetainedPacket.Document`,
`Case` and portable-review methods consume that reading; the older
`BuildDocument` entry rejects a mismatched folder. Historical renderer bytes
remain protected by the existing sealed-version tests.

Captured file maps carry bytes, not claims about a filesystem. `artifactdir.Read`
continues to reject links, special files and acquisition changes. Its snapshot
adapter checks the same file/directory layout and bounds and detaches the bytes;
each domain reader retains its own semantic verification. Runtime execution
continues its fresh authority, configuration, grant and credential checks.

## Qualification limits

The demo is synthetic and uses the existing loopback fixture. Browser/DOM and
fake accessibility-backend checks do not establish installed native behavior,
screen-reader acceptance or external-system equivalence. Actual packaged native
qualification across all five targets remains with #109/#593, including the
separate staged-upgrade journey. This change does not close #589's native
connected Run/Report/Share acceptance or the source-rights gate in #627.
