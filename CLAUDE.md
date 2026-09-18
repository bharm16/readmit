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
