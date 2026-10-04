# Exploratory receive and send

Messages' Receive and send selected reviews one saved listener revision, selected
original occurrences from a case or explicit variant and one target through the
existing reviewed-action owner. It retains a post-send horizon, message and byte
limits, actual frame/connection/session bounds and the source's receive settings.
The source's responder, remote-bind approval, TLS references and current operation
authority remain in force. Other writes and independent sends cannot run beside it.

## Runtime matching

`readmit-exchange-matching/v1` has two explicit modes. `unscoped` retains every
received byte for inspection and never credits an output association to an
execution or assertion. Stable sending application IDs and ordinary business keys
do not establish which runtime owns traffic.

`runtime-marker` uses the existing selector-to-runtime equality rule with a locally
reserved runtime marker. IssueExchangeRuntimeMarker requires author admission and
writes a `readmit-exchange-runtime-marker/v1` receipt for a cryptographically random
128-bit marker into this project's private store. Issuing a marker reaches no
network and edits no message. Copy the marker, create an explicit variant through
the existing field edit/preview/save owner where supported, and choose that derived
input. New pre-scoped input may instead come from the customer's own source.
Original evidence is never edited or given an implicit marker.

Review requires that every selected input already carries that exact issued marker
at the declared selector, plus one distinct readable input key. Immediately before
arming, a durable `readmit-exchange-runtime-reservation/v1` consumes the marker once
for the exact exchange, input and send identities. Even failed arming consumes it;
another exchange must use a fresh marker. This establishes local runtime ownership
and fresh local attribution, not external authentication or proof of unique causation.

Every credited output must carry that owned marker unchanged, one selected key and
a receive timestamp inside the stimulus/horizon boundary. Wrong/foreign markers,
unrelated keys, unreadable scope, before-stimulus and post-horizon occurrences remain
in the capture with exclusions. Duplicate output keys exclude every candidate as
ambiguous. Readers verify the issuance/reservation and the captured/sent evidence.
No business-key fallback silently weakens the runtime selector.

## Retained evidence and stopping

One admitted operation owns receiver readiness and sender execution. It records
readiness before stimulus intent; failed arming sends nothing. Capture uses the
shared receiver journal/case writer and send uses the shared replay sender and its
policy decision. Review tokens are single-use and expire; a retained exchange
output is also a durable once fence. A new explicit review selects fresh output
rather than reusing an abandoned operation.

`readmit-exploratory-exchange/v1` retains exact reviewed source revision and receive
configuration/identity, matching mode, input/send identities, target, actual readiness
and stimulus times, declared horizon end, received count, marker-bound occurrence
pairs, exclusions, actual replay outcomes and delivery uncertainty. It lives in
`.readmit/exchanges/<id>` beside the unmodified capture case and journal. A completed
record is hashed after evidence writers stop. Readers verify that hash, the capture
identity, runtime reservation and actual sender evidence. The shared retained-evidence
reader opens received messages without changing the selected project or route.

Complete zero means healthy capture over the entire declared post-send horizon. It
does not mean the target processed nothing or will never produce output. Message,
byte, frame, connection, authority and execution boundaries are safety stops; early
termination, cancellation, unavailable capture and unhealthy journal coverage remain
incomplete. Stop preserves uncertain delivery. Process death leaves durable intent,
send evidence and capture journal for inspection. Reopening never resumes a send or
rearms a listener, and marker/consent state never restores execution authority.

History reads are bounded at 128 retained entries and 16 MiB of records per read;
exceeding either refuses the read rather than dropping retained work silently. This
slice creates no test expectations or general multi-target orchestration.

### Create a test draft from an exchange

Create test draft from exchange opens the existing connected test editor. The
new exchange contract retains the exact reviewed case identity, ordered input
occurrences and target revision. Promotion verifies those references and the
exact retained exchange identity before opening the draft; changed input,
receiver or target references are refused. A missing receiver or target remains
suggested unavailable configuration in an incomplete draft.

The editor retains its exchange provenance and ordered steps through Save draft
and restart. It copies no observed value into an assertion, selects no runnable
observation or execution target automatically, and restores no consent or
consumed runtime marker. Choose expected behavior, observations and setup
explicitly before publishing. Complete healthy zero and partial output remain
observed exchange facts rather than expected truth. Return to retained exchange
reopens that exact evidence without starting a receiver or resending input.

`readmit-exploratory-exchange/v2` adds exact promotion origins.
`readmit-test-links/v2` retains their value-free provenance, and
`readmit-desktop-test-editor/v3` holds the resulting incomplete connected work.
The original exchange v1, test links v1 and editor v1/v2 remain readable under
their original membership; new promotion members are refused under those old
versions, including empty or null members. A legacy exchange that did not pin
exact object references remains inspectable and cannot be silently promoted by
finding a similar current source.
