# Support without uncontrolled evidence uploads

Start with a locally reviewed diagnostic summary and a reproduction built from
synthetic inputs. Standard support receives reviewed metadata and synthetic
reproductions only. Nothing below uploads, emails, opens a vendor connection or
grants remote access. A support request is not permission to access patient data.
The [commercial terms](commercial-terms.md) are an owner/counsel draft: actual
support contacts, covered releases, staffing and lawful arrangements still need
publication and approval. No operational contact or incident SLA is implied here.

## Prepare the smallest useful request

Record the release version, OS family/architecture, command or screen, fixed
error category, expected behavior and observed outcome using a synthetic example.
Review even this manually supplied text. Do not paste full command lines, paths,
usernames, hostnames, endpoint addresses, credentials, hardware identifiers,
patient identifiers, screenshots, free-form logs or crash dumps. No environment,
process, filesystem or machine inventory is collected by the support command.
If the error has no safe fixed category, describe the operation generically;
keep its original diagnostic text customer-local.

For a verified retained packet, create a `sharing.json` file containing:

```json
{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}
```

Preview without writing a bundle, read every summary member, then explicitly
approve the displayed identity into a new directory:

```sh
readmit share RETAINED_PACKET --kind retained-packet --policy sharing.json
readmit share RETAINED_PACKET --kind retained-packet --policy sharing.json --approve EXACT_PREVIEW_ID --output NEW_SUPPORT
readmit share verify NEW_SUPPORT
```

The [desktop shell](desktop.md#privacy-review-protected-export-and-support-sharing)
prepares the same summaries from its privacy panel: the policy is authored
through structured controls, the preview shows every summary byte before
anything is written, publication requires the exact preview identity over the
current sources, and the bundle is written into a new local directory named in
the host's save dialog, or one new workspace entry; a directory that already
exists is refused. The window verifies a bundle offline through the reader
`share verify` runs and refuses the same bundles. It is the same share
operation, with the same refusals and the same no-upload boundary.

`portable-review` and `derived-review` inputs follow the same
[sharing boundary](redact.md#reviewed-support-diagnostics-and-sharing-policy);
a derived review additionally needs its private `--local-state` linkage, which
is never copied. A policy denying support, an unlisted source type, a missing
source or stale approval is a refusal, not a partial success. There is no fallback
to copying a workspace when a source cannot be verified.

The bundle has exactly `support.json`, `event.json`, and `identity.sha256`.
The summary allowlist is source kind, source/input/spec/policy commitments,
closed outcome, declined external equivalence and fixed scope text. Commitments
can link artifacts and need review too. No patient bytes, arbitrary diagnostic
text, paths, secrets, hardware IDs, original reports, notes or logs are included.
The event records local byte review, not an authenticated person's consent.
Existing evidence and support contracts are unchanged.

`share verify` verifies the complete directory offline, independently of the
original source. It displays the validated summary and identity without launching
a viewer or interpreting active content. It refuses missing/extra/changed files,
symlinks and malformed/noncanonical summaries, including active text substituted
into a closed field. Exit 0 means bundle integrity; exit 1 means refusal or I/O
failure. A hash does not authenticate the customer, prove lawful disclosure,
certify de-identification or establish an external receiver's correctness.

## Synthetic example, with no customer inputs

Run this in a new private working directory with the released binary:

```sh
readmit report --scenario siu-reschedule-v1 --output synthetic
readmit report verify synthetic
readmit report assemble --case synthetic/reproducer --spec synthetic/spec.json --current synthetic/post-fix --output synthetic-retained
readmit share synthetic-retained --kind retained-packet --policy sharing.json
readmit share synthetic-retained --kind retained-packet --policy sharing.json --approve EXACT_PREVIEW_ID --output synthetic-support
readmit share verify synthetic-support
```

Create the policy above first and replace `EXACT_PREVIEW_ID` with the displayed
value after reviewing it. `report` explicitly starts built-in loopback receivers
and executes the committed SIU scenario; it contacts no customer endpoint.
Assembly, preview, publication and verification make no connections. Prefer
sending the scenario name and instructions for independent regeneration. Its
fresh session IDs and timestamps need not match another execution. The synthetic
packet itself includes run configuration and historical paths, so it is **not**
an automatically approved attachment. Review all bytes separately if sharing it.
A built-in passing fixture is not a reproduction of a customer-specific failure.
For another failure, build a minimal synthetic case with the documented
[scenario tools](scenario.md); do not rename a patient-derived case synthetic.

## Customer approval and custody

Before any manual transfer, the customer's authorized reviewer approves the
exact files, identity, recipient, purpose, transfer channel and retention period.
Record that decision in the customer's controlled ticket/audit system. Local
`--approve` binds bytes only. Where team identity is required, use the customer
hub's authenticated support-review workflow; a local hash cannot substitute.
Send only the reviewed support directory through the approved channel, never its
parent, original packet or private linkage. This software performs no transfer.

The recipient runs `share verify`, compares the identity through the agreed
channel, and confirms the customer authorization independently. Changed bytes
need new review. Keep access limited to named responders; record receipt,
redisclosure approvals and retention/deletion actions in the controlled system.
Remove working copies under the approved retention procedure; account separately
for backups and legal retention obligations. Readmit does not track recipients,
erase remote copies or certify deletion. Unix output uses 0700/0600; Windows
uses inherited ACLs, which the customer must constrain before creation.

Ctrl-C cancels creation. Failed/interrupted publication may leave an incomplete
directory; verification refuses it. Preserve relevant local diagnostic evidence,
review again, and use a new output name. Never add a completion marker manually,
resume a partial transfer automatically or overwrite a previously approved bundle.

## Incident escalation and limits

For patient-care impact or a production interruption, activate the customer's
clinical/operations incident process immediately. Stop unsafe replay or disclosure,
preserve original evidence locally, and route technical diagnosis through the
approved support contact once available. This tool is not an emergency service.
For suspected exposure or a vulnerability, notify the customer's security/privacy
responders through their approved route; keep exploit details and sensitive
attachments out of public issues. Use the vendor's published private route only
once the owner has established it. If no route is available, retain details
locally and seek an approved contact rather than publishing them.

If support unexpectedly receives patient data, stop forwarding or processing it,
restrict access and escalate to the authorized privacy/security incident process.
Coordinate containment, preservation and deletion under that process; do not
promise erasure while investigations or retention requirements remain unresolved.
PHI access is outside standard support. Before offering it, owner/counsel and the
customer must settle actual roles, lawful basis and applicable agreements,
including whether a BAA is required, plus minimum necessary scope, authorized
people, storage/transfer controls, retention and incident duties. Neither this
runbook nor customer byte approval completes those arrangements. The metadata
and synthetic-only path remains usable without them.
