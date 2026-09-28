---
status: accepted
date: 2026-09-26
---

# Keep relationship reviews as catalog revisions with timed, withdrawable decisions

The desktop timeline (#550) names its link rules, coverage and relationship
review as objects of the project's catalog (`link-rules`, `coverage`,
`link-review`) rather than as workspace entries a person types. A timeline and a
review name the exact revision they applied, so an answer for a replaced
selection is recognizably stale and a review is never applied to another case or
another link rule version.

`readmit-correlation-review/v1` is amended in place rather than versioned: the
contract is unreleased (no tag carries it, and only the desktop reads it), so no
document a customer holds is orphaned. Every retained decision now requires `at`,
the time the facade recorded it (a decision being submitted carries none), because a review history a person reads shows who
decided what, when and why. A new `withdraw` action restores the status a link
had before its latest decision still in effect; Undo is one more revision, and no
history is rewritten. Review directories written before this amendment lack `at`
and are refused, which is acceptable only because the format was never released.

Recorded links are reviewed too. With no link rules chosen, the review of a case
is of the acknowledgements the case itself matched: its machine finding is
`correlate.Recorded`, a `readmit-correlation/v1` finding of those matches as
observed links named `recorded-N`, with an empty `rules_sha256` because no rule
produced it. That review is its own `link-review` object, keyed by the case and
"recorded" through the finding's digest, and is never applied under link rules,
nor a rules' review under recorded links. An acknowledgement or identifier that
matched more than one occurrence is not a link and is never accepted, rejected
or undone; Add link records the pair a person means.

A `link-review` revision holds the history alone. The machine finding is
reproduced from the case and the immutable link rule revision, and its digest
must equal the history's `machine` member; storing it would exceed the catalog's
member bound for large cases and add nothing a reader can trust more.

A decision is recorded under a reviewer: the one the window names for it, or
else the Reviewer configured in the local preferences. With neither, Review, Add
link and Undo are refused with a problem at `decision.actor`, and the window asks
for a Reviewer only then. The account's own name is never used in its place: a
reviewer is a local declaration a person makes, not an identity anything
authenticates, and an operating-system user name silently standing in for one
would read as exactly that.

A coverage declaration binds its case's identity and the link rules' canonical
digest (`correlate.RulesDigest`), both taken from the objects a person chose; no
digest is ever typed. Coverage and link rules keep the identities and authority
mappings they were imported with. A source window may declare the IANA time zone
it was written in (`time_zone`), amending `readmit-sequence-analysis/v1` in place
for the same reason as the review contract: it is unreleased. Its instants then
carry that zone's offset, so the window reads back as the wall times it was
declared at; the zone places nothing and adjusts no observed time.

Only sources one Readmit recorder session captured share a time axis: that
session's single clock observed all of them. Every other source is its own
clock, and its events are grouped within it; the timeline reports the clocks
and never aligns one against another. A coverage declaration's clock tolerance only
bounds how far one occurrence's observed and declared instants may differ before
a clock mismatch is reported; it aligns no clocks.
