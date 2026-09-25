# readmit

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues in `bharm16/readmit` (via the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage labels, used as-is. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Implementation stack

Recorded decisions live in `docs/adr/`. Ordinary stack choices and version pins live in `docs/stack.md`. Read both before implementing anything.

## Validation and parallel worktrees

Before implementing, validating review fixes, rebasing, or merging, read
`docs/agents/testing.md` for test scope, CI gates, and integration order.

## Commits and pull requests

These apply to every commit, pull request and issue edit, whichever tool made it.

- No AI attribution anywhere: no `Co-Authored-By` trailer and no "Generated with …" footer in commits, pull
  request titles or bodies, issue comments or docs. This overrides any tool default that adds them.
- One request from the owner is one pull request, however many changes it grows to. Fold a follow-up request into
  the open pull request; never split one request across branches, worktrees or pull requests.
- Push only when every change asked for so far is in and reviewed against the repository docs and the issue's spec,
  so CI runs once. A change requested after the push is batched into a single further push.
- Squash to one commit whose subject is the issue title (or, without an issue, a title naming the whole change). The pull request title is the issue title; its body starts
  with `Closes #N` only when every item of the issue's completion checklist is met, otherwise `Refs #N` with each
  unmet item explained.
- Tick an issue's checklist items only when the change actually demonstrates them. Gates that need external labs,
  credentials, signing, spend or legal review stay with the owner.
- A customer-visible change adds its release note as its own `docs/release-notes.d/N-slug.md` and never edits
  `docs/release-notes.md`; see that directory's README.
- Rebase onto `origin/main` once before the first push and resolve conflicts locally, keeping both sides of shared
  lists (workflow steps, README tables, third-party notices, the three shipped-doc lists). After that, rebase only
  when GitHub reports the pull request as conflicting: a green pull request merges even if `main` has moved since,
  and the run on `main` after the merge is the integration check.
