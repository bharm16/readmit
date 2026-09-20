# The guided sample, without a terminal

The complete frozen desktop walkthrough requires no license or activation.

The desktop shell walks one complete investigation over synthetic evidence:
create the sample, author a regression test over it, run that test against a
practice receiver behaving as the defect makes it behave and watch it fail, then
run the same test against the corrected receiver and watch it pass.

Nothing in it is typed into a shell. That is the point of it, and it is what
separates this page from [the synthetic walkthrough](../samples/synthetic-walkthrough/README.md),
which is the same investigation as a script somebody runs from a terminal.

What it leaves behind is the real work. The case is the generated case, the spec
is an ordinary `readmit-test/v1` document that `readmit test` prepares unchanged,
and each run is an ordinary `readmit-result/v1` directory whose verdict the
result reader re-derives from the run and observation evidence it retained. There
is no tutorial mode, no simulated state and no progress document: see
[how progress is known](#how-progress-is-known).

The saved spec is a document the command line reads and validates; it is not one
the command line can execute as it stands. It names the practice endpoint, whose
declared port nothing is listening on, and an observation document a practice run
writes for itself. Sending it from a terminal means pointing it at a receiver of
your own — that is [`readmit test --send`](test-runner.md), and it is a separate
decision about a separate endpoint.

## The four steps

| Step | What it does | What completes it |
| --- | --- | --- |
| `sample` | Creates the sample workspace: the frozen `readmit-synth-v1` cases, an index of the one this path uses, and a practice endpoint | The `regression` case verifies as complete generated evidence, the practice endpoint reads as a target configuration, and the index still describes that case |
| `test` | Answers [the authoring stages](test-authoring.md) over that case and saves the spec into the workspace | A `readmit-test/v1` document in the workspace sending the `regression` case at the practice endpoint |
| `baseline` | Runs the saved spec against the practice receiver in its **defective** mode | A retained result over that spec, that case and that mode whose status is `assertion_failure` |
| `post-fix` | Runs the same spec against the practice receiver in its **fixed** mode | A retained result over that spec, that case and that mode whose status is `pass` |

The order is the order the steps are listed, and the next step is the first one
that is not complete. The first step is offered as the sample creation action;
the second opens the sample case so the authoring panel can be answered beside
it; the last two are run from the guided panel itself.

A step is completed by the outcome it names and by nothing else. A test that
expects the defect rather than the corrected behaviour is a real test, and
running it produces two real results, but neither of them completes a step here:
the whole point of the sample is the defect being found and then fixed.

## The sample workspace is a workspace, not a family

`readmit synth` writes a **family**: three case bundles and a `family.json`
completion record. A directory holding that record is retained evidence, and
nothing may be written inside retained evidence — including the spec the
authoring flow saves and the results a run retains. A guided sample could not be
walked in one at all.

So the sample workspace keeps the generated case bundles exactly as the command
line writes them, byte for byte, and does not keep the completion record. In its
place it writes two entries that are not evidence.

The first is one practice endpoint, `practice-target.json`, because a test names
a target configuration and a generated family holds none; without it the only way
to finish the authoring flow would be to write a configuration file by hand in a
text editor.

```json
{
  "schema": "readmit-target/v1",
  "test_endpoint": true,
  "address": "127.0.0.1:2575",
  "transport": "plain",
  "approved_transport": false,
  "connect_timeout": "2s",
  "message_timeout": "5s",
  "max_ack_bytes": 65536
}
```

It is a test endpoint on loopback with no approved transport and no credential,
so nothing about it can point a run at a real interface. The address is the port
[`readmit listen`](listen.md) is documented with, so the saved spec means the
same thing to somebody who later runs it from the command line against their own
receiver.

The second is one index of the `regression` case, `regression.index.json`. The
[message grid](desktop.md#the-message-grid) the authoring flow selects its
occurrences from reads an index, and the window builds none anywhere else, so
without one the guided path would stop at a terminal running
[`readmit index build`](index.md). It is the only index this product builds
without being asked each time, and its three retention declarations are fixed and
stated here rather than defaulted:

| Declaration | Value |
| --- | --- |
| Fields | `MSH[1]-9[1]`, `MSH[1]-10[1]`, `SCH[1]-2[1]` |
| Retained in the form | `states` — the decoded state and byte span of each position, and **no value at all** |
| Retained until | no end; deleting it is a person's decision |

An index is derived and disposable, so deleting it is safe. Its records are a
pure function of the case and the policy above, and only the instant it records
differs between two builds.

## What a practice run is

A practice run binds the built-in fixture receiver on a loopback port **inside
this application**, sends the saved spec's selected occurrences at it, and writes
the whole run into one new entry of the workspace:

```
baseline-run/
  spec.json          the spec that was executed
  target.json        the endpoint, with the loopback port the receiver bound
  observation.json   the observation the practice receiver wrote
  receiver/          the case the practice receiver recorded
  result/            readmit-result/v1
```

The two run steps send the same bytes of the same spec at the same receiver. The
only thing that differs is the receiver's mode: in `defective` mode the
rescheduling leaves the original appointment behind and the ledger holds two
records, and in `fixed` mode it updates the original and the ledger holds one.
A test expecting one appointment therefore fails the first and passes the second,
which is the demonstration the whole sample exists for.

Three members of the saved spec are rebound onto the run's own directory, and
the window states all three rather than leaving them to be discovered:

- `target`, because the practice receiver chooses its own loopback port and no
  configuration written in advance can name it;
- `observation.path`, because the practice receiver writes its own observation;
- `input.case`, because the executed copy of the spec sits one directory below
  the saved one.

Everything a verdict depends on — the selected occurrences, the observation
boundary and every assertion — is the saved spec's own and is never touched. The
saved spec in the workspace is not rewritten by a run.

A practice run is the one operation in the window that opens a socket. It reaches
no host but the loopback port it bound itself. Cancelling it stops future sends;
whatever it had already written is retained where it was written and is reported
as cancelled rather than as a verdict.

## How progress is known

Every step is read back out of the workspace folder, by the reader that owns the
evidence that step produces: the case bundle reader verifies the sample, the spec
reader decodes the saved test, and the result reader re-derives each verdict.

Nothing is remembered. There is no completion flag, no tutorial document and no
member added to the shell's own local state, so:

- closing the window and reopening the folder reports what that folder really
  holds, not what a previous session believed;
- a step performed from the command line counts exactly like one performed in
  the window;
- deleting the saved spec takes every step that depends on it back to
  incomplete, because those steps were never true of anything else.

The workspace may hold more than one test. The guided sample follows the first
entry, in the order the folder lists them, that declares a test sending the
sample case at the practice endpoint; a run of any other test is not a step of
it.

One consequence is worth stating plainly. The practice endpoint and the index are
not evidence and are safe to delete, but the first step needs both, so deleting
either takes the whole path back to `sample` — and the sample creation refuses a
folder that already exists. The remedy is to create a new sample workspace in
another folder. Nothing in the old one is lost or changed: the cases, the saved
spec and both runs stay exactly where they are and stay readable.

## Privacy

The evidence is synthetic and generated on this machine. The practice receiver
is in this process and listens on loopback; no other host is contacted, and
nothing is sent anywhere. The guided sample writes no document of its own, keeps
nothing in the shell's local state, and adds no member to any of the three
documents [the shell keeps](desktop.md#privacy).

A practice run's evidence is ordinary customer-local evidence in the sense every
result directory is: it contains the values that were sent and observed. Here
those values are synthetic.

The window reports a failing expectation by naming the expectation and its
operator, never the value that was read. Reading a value out of evidence is the
[inspector](desktop.md#inspecting-original-values), deliberately.

## What it does not establish

- Nothing about a production receiver, a vendor engine or a clinical workflow.
  The observation boundary is the built-in fixture's appointment ledger.
- Nothing about HL7 conformance. `readmit-siu-v1` is a narrow fixture profile.
- Nothing about an **installed** application. Every desktop package this
  repository builds is an unsigned development preview published nowhere, so
  this path is walked in a shell built from a checkout, not in an application an
  engineer installs. See [the support matrix](support-matrix.md) and
  [D5](product-decisions.md#d5--desktop-distribution-and-signing).
- Nothing about readiness to hold customer evidence.

## Not in this release

- Running any test but the guided sample's, and sending to any endpoint but the
  practice receiver this application binds. Executing a spec against a real
  target is [`readmit test`](test-runner.md) and
  [durable runs](durable-runs.md).
- Choosing the receiver's behaviour freely. The two run steps fix it, because a
  step means one outcome.
- A third trial that reintroduces the defect. The command line's
  [fail, pass, fail demonstration](test-runner.md) has one, and
  [`readmit report`](report.md) seals a packet from the first two.
- Explaining a failure through linked assertion evidence in the window. The
  window names the expectation and its outcome; [`readmit explain`](explain.md)
  links a run to the evidence each value was read from.
- Reopening a saved spec into an authoring draft, and resuming an unsaved draft
  across an interruption. Both are out of scope in
  [test authoring](test-authoring.md) and stay out of scope here.
- Listing a run folder, a target configuration or an index as something the
  workspace understands. All three are listed as entries this release does not
  open as evidence; the guided panel and the grid name them.
- Building an index of any other case, rebuilding this one, or changing any of
  its three declarations. That is [`readmit index build`](index.md).
- Deleting or replacing anything the guided sample wrote. Every step writes a
  new entry and refuses one that already exists.
