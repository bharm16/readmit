# Released behavioral expectations

`readmit expectation` releases an authored test as an immutable local revision
with explicit profile pins. It reuses [baseline review](baseline.md): the same
complete specification comparison, operator/selector/occurrence changes, local
approver and rationale. A passing run never creates an approval. The desktop
Regression baseline panel offers **Release a test version with profile pins**
for the same workflow, including historical inspection and cancel-before-write.

```sh
readmit expectation review candidate.json --id booking \
  --profile local-profile.json --show-values
readmit expectation release candidate.json --id booking \
  --profile local-profile.json --review REVIEW_SHA256 \
  --approver 'Local reviewer' --rationale 'Reviewed synthetic rejection' \
  --output booking-1.json
readmit expectation review changed.json --id booking \
  --profile local-profile-v2.json --previous booking-1.json --show-values
readmit expectation release changed.json --id booking \
  --profile local-profile-v2.json --previous booking-1.json --review NEW_REVIEW_SHA256 \
  --approver 'Local reviewer' --rationale 'Revised expectation and profile' \
  --output booking-2.json
readmit expectation show booking-1.json --show-values
```

Repeat `--profile` for each local profile. Omitting it explicitly pins none; it
never inherits pins from the parent. Review names every added, removed or changed
pin and specification part. Approval re-reads all inputs. A changed expectation,
profile, test name or predecessor invalidates the review. Reformatting alone
changes no canonical identity. A profile whose content changes under its old
version is refused; create a new profile version deliberately.

The `readmit-test-release/v1` document contains `schema`, stable `id`, `parent`
(the complete predecessor release's canonical SHA-256), `profiles` (the existing
`readmit-profile-version/v1` seals), `baseline` (the existing approved
`readmit-baseline/v1` record), and `review` (the commitment to all those review
inputs). Revision numbers follow the baseline chain. Full release identity is
its canonical SHA-256, including approval metadata. Forks from one predecessor
are distinct identities; there is no global latest pointer. Save exclusively
creates a new private file, never replaces a release and never edits evidence.
Historical inspection needs neither the original candidate nor profile files.
Retain predecessors to inspect history; reading a child alone does not verify
an unavailable parent. The impact operation verifies the direct predecessor
relationship in both the release and baseline chains.

## Applying a released template to a suite

Use an explicit `readmit-suite-releases/v1` sidecar. Every suite test must have
exactly one entry. A reference names a suite test, a local relative release path
resolved beside the sidecar, and the full release identity printed on release.
Different suite test IDs can reuse one released template.

```json
{
  "schema": "readmit-suite-releases/v1",
  "tests": [{
    "test": "booking",
    "release": "booking-1.json",
    "identity": "REPLACE_WITH_RELEASE_SHA256"
  }]
}
```

```sh
readmit suite prepare suite.json --environment east \
  --releases releases.json --output east-reviewed --json
readmit suite run suite.json --environment west \
  --releases releases.json --output west-reviewed --send --json
readmit expectation impact booking-1.json booking-2.json suite.json \
  --releases releases.json
```

Preparation verifies each complete release against the sidecar identity and each
suite template against the complete approved specification. A data row may repeat
an approved expected value, but a different expected value is refused. Release a
separate reviewed template for a row requiring different expectations. Environment
bindings and row case references still have the existing [suite](suites.md)
meaning: they vary the deployment and input, not the approved expectations.
This approval is of a reusable **template**, not approval of every case, target,
environment or execution. The normal case verification and durable run input pins
remain in force. A profile pin records the rules the author used; it does not add
profile evaluation or make unsupported rules pass.

The prepared directory retains `release-references.json` and a verified
`release-TEST.json` per template beside its generated specs, so the exact local
approval and profile pins remain inspectable after the source files move. The
copied sidecar is a historical source declaration; its paths are not relocated.
Omitting `--releases` preserves ordinary suite behavior and makes **no approval
claim**. This is explicit local admission, not an organizational enforcement gate.

Impact lists each test and its number of expanded rows as `affected` (pins the
old release and declarations changed), `unaffected` (same declarations),
`current` (already pins the successor), or `unrelated`. It reads only the supplied
suite and sidecar, verifies their release files, and moves no pin. It includes
the exact baseline and profile changes. It does not search a global library or
predict pass/fail; cases, environments and executions remain unassessed. Change
a sidecar explicitly when accepting an upgrade, then re-run preparation.

## Privacy and refusals

Values are hidden by default. `--show-values` reveals escaped full JSON; releases
always retain the full private specification on disk. Profile IDs, change
locations, test IDs, local reviewer names and rationale are user-authored data,
not disclosure-approved metadata. Keep them free of patient values. Files use
0600 and directories 0700 where supported; Windows inherits directory controls.
Local labels and hashes do not authenticate a reviewer or resist an actor who
can rewrite all files and pins. Team authorization remains the hub's boundary.

Readers refuse unknown/duplicate members, missing/null required nested members,
unsupported schemas, altered commitments, mismatched pins, truncated documents,
symlinks and oversized files. Releases are bounded to 3 MiB, specs retain their
1 MiB bound, and at most 32 distinct profile IDs may be pinned. Suite sidecars
are at most 1 MiB and 64 entries. Tests retain the baseline's revision bound.
There is no automatic migration, overwrite, retry or network call. A cancelled
review writes nothing; admitted local writes finish once started. An interrupted
new file stays visible and is refused if incomplete. Inspect it and use a new
destination; never treat it as a release or resume a send from it. Suite execution
cancellation and recovery retain the existing durable-run semantics.

Existing baseline, suite, test, result and evidence documents gain no members.
This adds no team identity service, global version registry, automatic pin
upgrade, external-system certification or evidence-disclosure approval.
