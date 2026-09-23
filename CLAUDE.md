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
- Review the change against the repository docs and the issue's spec, and land every fix, before opening the pull
  request, so CI runs once. If something must change after it opens, batch it into a single push.
- Squash to one commit whose subject is the issue title. The pull request title is the issue title; its body starts
  with `Closes #N` only when every item of the issue's completion checklist is met, otherwise `Refs #N` with each
  unmet item explained.
- Tick an issue's checklist items only when the change actually demonstrates them. Gates that need external labs,
  credentials, signing, spend or legal review stay with the owner.
- Rebase onto `origin/main` once before the first push and resolve conflicts locally, keeping both sides of shared
  lists (workflow steps, release notes, README tables, third-party notices, the three shipped-doc lists). A green,
  mergeable pull request merges even if `main` has moved since.
