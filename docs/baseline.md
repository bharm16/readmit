# Reviewing and approving regression baselines

Commands that create or run work use the [explicit license setup](license-v2.md#running-command-line-recipes-with-an-activated-license). Read-only commands and frozen practice need no activation.


A baseline is an explicit local approval of an authored regression specification.
It is never a passing run promoted automatically. `readmit-baseline/v1` retains
the complete validated `readmit-test/v1` specification, revision number, previous
revision's canonical SHA-256, review commitment, local approver and rationale.
Each approval exclusively creates a new private file; no command replaces a
historical revision, moves a saved test's pin or modifies original evidence.

```sh
readmit baseline review candidate.json --show-values
# Copy the exact identity from the review after inspecting its changes.
readmit baseline approve candidate.json --review REVIEW_SHA256 \
  --approver 'Local reviewer' --rationale 'Expected rejection verified in fixture' \
  --output baseline-1.json
readmit baseline review revised.json --previous baseline-1.json --show-values
readmit baseline approve revised.json --previous baseline-1.json \
  --review NEW_REVIEW_SHA256 --approver 'Local reviewer' \
  --rationale 'The rejection contract changed' --output baseline-2.json
readmit baseline show baseline-1.json --show-values
```

Review compares assertions by their unique IDs and shows additions, removals,
and changes to their **entire** definition: operator, occurrence, selector and
expected value. It separately lists schema, name, input, target, setup,
observation and assertion-order changes. No normalization or ignore policy can
suppress these differences. Even a change to non-assertion configuration
invalidates the review commitment. The previous revision's full document,
including its approval record, is bound into that commitment. Reformatting JSON
does not change canonical identities.

Without `--show-values`, review and show retain change locations and kinds but
omit before/after contents. With it, exact before/after JSON appears as escaped
strings, preserving empty, omitted, null and present expectations. Diagnostics
never echo file paths or specification values. Approver and rationale are
user-authored local records and are displayed by `show`; avoid putting patient
data into them. A baseline **retains all specification values on disk**, including
paths and expected patient values. It is sensitive local data, not a safe export.
Files are created with mode 0600; Windows inherits directory access controls.

## Desktop

The inspector's Regression baseline panel uses the same Go engine. Name the
candidate specification and optional previous baseline as entries in the open
workspace, then select **Review baseline changes**. Reveal values explicitly to
see exact before/after expectations. Enter a local approver, rationale and new
filename, then select **Approve this exact baseline revision**. Changing the
candidate, previous selection or privacy option clears the review. The engine
also re-reads both files on approval and refuses a stale commitment.

**Inspect retained baseline** reads the historical specification and approval
without needing the original candidate file. **Cancel review** discards the
pending decision and writes nothing. State is held only in the mounted panel;
it is never stored in browser storage or the restored working session. Backend
operations are bounded, hold the shared operation slot, and finish once admitted;
an admitted exclusive write is not interruptible. Errors leave prior revisions
unchanged and can be retried with a new destination after investigation.

## Contract and limits

`baseline.Review`, `Approve`, `Inspect`, `Read` and `Save` are the shared consumer
boundary in `internal/baseline`. Stored JSON is strict, including nested specs;
missing/null required members, unknown members, unsupported schemas, invalid
assertions and changed spec commitments are refused. Inputs must be regular
files, not symlinks. Specifications retain the test reader's 1 MiB limit;
baselines are bounded to 2 MiB, 256 assertions and one million revision numbers.
Approvers are at most 256 UTF-8 bytes and rationales at most 8192, both nonblank
and free of control characters. A partial/interrupted new file stays visible
and is refused if incomplete; it is never used as a parent or overwritten.

A revision hash detects a changed document, **not an authenticated identity**.
An actor who can replace local files can recompute commitments. Team identity,
authorization and revocation belong to #97/#98. Revision numbers follow the
explicitly supplied parent, not a mutable global latest pointer; branches from
one parent are allowed and distinguished by content identity. Keep every parent
for historical review. Reading a child checks its own commitment and does not
claim that an unavailable parent was verified.

This is approval of declared expectations, not evidence that those expectations
are correct or that a receiver passed. No network operation runs here. Case and
target paths are retained as declarations, **not snapshots of their contents**;
changing a referenced file does not change this baseline. Use the existing
verified case/run identities and [drift comparison](drift.md) to assess actual
input, environment, target and engine changes. Baselines neither change test
execution nor enforce team approval admission. Other specification contracts,
profile pins, normalization-policy pins and automatic known-good artifact
snapshots are unsupported here. All existing spec, result and evidence formats
remain unchanged.

For released test versions with profile pins, suite admission and impact, use
[released expectations](expectations.md). The desktop panel has a separate release
mode; plain baseline documents and their approval meaning remain unchanged.
