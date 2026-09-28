package report

import "bytes"

// An instructionBlock is one named rerun-instruction block sealed into
// packets, with every text any release wrote. The manifest records the
// version only when it differs from the original text, so packets sealed
// before versioning verify unchanged: their instructions identify as the
// original by their bytes. Verification pins the recorded version, never
// the live command surface, so CLI verbs can evolve without invalidating
// sealed evidence. Later versions record "v2", "v3", and so on.
type instructionBlock struct {
	// current is the version assembly writes.
	current string
	// texts holds every text any release sealed, by recorded version. The
	// empty version is the original text, which a manifest omits.
	texts map[string][]byte
}

// write answers the text assembly seals.
func (b instructionBlock) write() []byte { return bytes.Clone(b.texts[b.current]) }

// check reports whether sealed instructions are the text the manifest's
// recorded version names. A packet sealed before versioning records
// nothing and must carry the original text.
func (b instructionBlock) check(recorded string, data []byte) bool {
	text, known := b.texts[recorded]
	return known && bytes.Equal(data, text)
}

// packetInstructionsBlock seals the sample packet's rerun instructions.
// The original text froze the trial commands; the prepared workspace
// builds its own instructions from trialInstructionsFor, so the sealed
// text never moves with the live command surface.
var packetInstructionsBlock = instructionBlock{current: "", texts: map[string][]byte{"": []byte(packetInstructionsV1)}}

func packetInstructions() []byte { return packetInstructionsBlock.write() }

// packetInstructionsV1 is the sample packet's original rerun text, kept
// byte-identical so packets sealed before versioning keep verifying.
const packetInstructionsV1 = `# Reproduce with only the released binary and this packet

This is a synthetic-only demonstration of the built-in SIU fixture. It requires
no source checkout, Go, Python, jq, network service, or author contact. Only local
loopback TCP is used. It makes no production or customer-PHI readiness claim.

Copy the released executable and this entire packet into one new folder. Name
them readmit (readmit.exe on Windows) and packet. Paths containing spaces work.
Open two terminals with that folder as their current working directory. All
commands below use that same directory; do not cd into the sealed packet.
On Windows PowerShell replace ./readmit with .\readmit.exe; the quoted forward
slash paths and all flags below work unchanged. On Unix preserve executable
permission when copying the binary (chmod +x readmit if necessary).

First, in either terminal:

~~~sh
./readmit report verify "packet"
./readmit report prepare "packet" --output "rerun" --address 127.0.0.1:2575
~~~

Both commands must exit 0. Verification is offline. Preparation creates a NEW
directory outside the packet. It preserves the input case identity and all
assertions. Its runnable specs change only case, target and observation path
bindings. Historical specs in the packet are never edited. preparation.json
links the historical spec and packet identities to each runnable spec hash and
the explicit loopback target hash. The preparation has its own hash and does
not seal the workspace: new receiver evidence and results are expected there.

If port 2575 is occupied, select an unused loopback port with --address during
preparation and use that same address in every listen command below. The
workspace RERUN.md prints the selected address. No JSON editing is needed.
If rerun already exists, choose a NEW workspace name and substitute that name
in the commands. Never overwrite or delete evidence to reset a completed run.

## Reset and run each mode

The declared initial state is an empty appointment ledger with zero processed occurrences. Each listen invocation creates a new session, observation file and receiver output. Wait for its Listening: line before running test in the other terminal. --max-messages 2 makes the listener finalize and exit after the two messages; wait for that exit before starting the next mode. A startup or execution error is not the expected defective verdict.

### baseline

Terminal A:

~~~sh
./readmit listen --address "127.0.0.1:2575" --mode defective --max-messages 2 --output "rerun/baseline/receiver" --observation "rerun/baseline/observation.json"
~~~

Wait for Listening:. Terminal B:

~~~sh
./readmit test "rerun/baseline/spec.json" --send --output "rerun/baseline/result"
~~~

Expected exit: 1. Expected result: assertion_failure; 2 ledger records. Both ACK assertions pass. The completed result retains the actual observations, receiver session and mode.

### post-fix

Terminal A:

~~~sh
./readmit listen --address "127.0.0.1:2575" --mode fixed --max-messages 2 --output "rerun/post-fix/receiver" --observation "rerun/post-fix/observation.json"
~~~

Wait for Listening:. Terminal B:

~~~sh
./readmit test "rerun/post-fix/spec.json" --send --output "rerun/post-fix/result"
~~~

Expected exit: 0. Expected result: pass; 1 ledger record. Both ACK assertions pass. The completed result retains the actual observations, receiver session and mode.

### reintroduced

Terminal A:

~~~sh
./readmit listen --address "127.0.0.1:2575" --mode defective --max-messages 2 --output "rerun/reintroduced/receiver" --observation "rerun/reintroduced/observation.json"
~~~

Wait for Listening:. Terminal B:

~~~sh
./readmit test "rerun/reintroduced/spec.json" --send --output "rerun/reintroduced/result"
~~~

Expected exit: 1. Expected result: assertion_failure; 2 ledger records. Both ACK assertions pass. The completed result retains the actual observations, receiver session and mode.

## Verify retained evidence

In Terminal B after all three trials:

~~~sh
./readmit diff "rerun/baseline/result" "rerun/post-fix/result"
./readmit report verify "packet"
~~~

The field diff reports two unchanged sent messages. The ledger assertions distinguish the outcomes. The sealed packet must still verify with the same identity. Rerun session IDs, timestamps, ports, target hashes and result identities may differ from historical evidence; case identity and assertion semantics must not. ACK receipt differences are not the appointment defect.

## Interrupted or repeated trials

If a listener remains running after a failed test, stop it with Ctrl-C and wait for it to exit. Preserve the partial evidence. Run report prepare again with a new workspace name, then start fresh listeners and use the new workspace paths. Never reuse an observation file, receiver output or result destination. The command refuses existing destinations. There are no executable reset hooks or expressions in a spec.
`

// retainedInstructionsBlock seals the retained packet's rerun instructions.
var retainedInstructionsBlock = instructionBlock{current: "", texts: map[string][]byte{"": []byte(retainedInstructionsV1)}}

func retainedInstructions() []byte { return retainedInstructionsBlock.write() }

// retainedInstructionsV1 is the retained packet's original rerun text, kept
// byte-identical so packets sealed before versioning keep verifying.
const retainedInstructionsV1 = `# Rerun retained evidence

Verification is offline: readmit report verify-retained PACKET
No endpoint is contacted and no historical path is resolved by verification.

Keep this packet immutable. Copy case/ and the root spec.json into a separate
new workspace. If a baseline exists, its own case is baseline-case/ and its
historical specification is baseline/spec.json (baseline/result/spec.json for
a durable job). Never overwrite a prior run.

Historical target and observation paths are evidence, not runnable authority.
An operator must explicitly select an authorized nonproduction target and
credentials, establish the required setup/reset and observation boundary, and
rebind only those paths and the copied case path in a new specification. Preserve
its message selection and assertions. Use readmit test --help for execution
options. From the separate workspace, after rebinding and operator review:

    readmit test spec.json
    readmit test spec.json --send --output NEW_RESULT

The first command validates locally without sending. The second explicitly
authorizes execution. Exit 0 is pass, 1 assertion failure, and 2 execution error.
New specifications have new identities; retain both.

No packet operation performs setup, reset, recovery or a network send. A missing
baseline cannot be recreated by a built-in defective fixture. If the original
target or required observation is unavailable, retain the single-run report and
state that before/after proof is unavailable. After interruption use a new packet
destination; an incomplete packet is never verified as complete. Uncertain sends
must be reconciled at the target before any authorized rerun.
`

// connectedInstructionsBlock seals the connected packet's rerun instructions.
var connectedInstructionsBlock = instructionBlock{current: "", texts: map[string][]byte{"": []byte(connectedInstructionsV1)}}

func connectedInstructions() []byte { return connectedInstructionsBlock.write() }

// connectedInstructionsV1 is the connected packet's original rerun text, kept
// byte-identical so packets sealed before versioning keep verifying.
const connectedInstructionsV1 = `# Rerun retained connected evidence

Verification is offline: readmit report connected verify PACKET
It reads only the retained lifecycles; no server, file export, database or
credential is contacted, and no historical path is resolved.

Keep this packet immutable. Each lifecycle's plan is under SECTION/plan and
its original inputs under SECTION/plan/phases. A new execution needs the
original authored test, a newly prepared plan and a runtime configuration
naming an authorized nonproduction environment, its grants and credentials.
None of those are reconstructed from this packet: historical targets, grants
and bound server identities are evidence, never runnable authority.

    readmit connected prepare INPUT_DIRECTORY PLAN --seed SEED --base-time TIME
    readmit test PLAN --connected-config CONFIG --instance NEW_INSTANCE
    readmit test PLAN --connected-config CONFIG --instance NEW_INSTANCE --send --output NEW_RESULT

The first test command prepares without effects. The second explicitly
authorizes execution. Exit 0 is pass, 1 assertion failure, 2 unresolved.
Retain the new result beside this one; never overwrite either.

A reproduction claim needs two retained actual executions of the same plan
against the same declared target (readmit report connected assemble --replay).
A missing baseline is never recreated, a lost response is never resent, and
an uncertain effect must be reconciled at the target before any new run.
`
