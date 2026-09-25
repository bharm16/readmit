# Per-change release notes

A change with a customer-visible effect adds its release note here as its own
file, `N-slug.md`, where `N` is its issue or pull request number and `slug` is
a few lowercase words joined by hyphens: `512-license-page.md`. The file holds
one or more Markdown list items written as `docs/release-notes.md`'s entries
are. Never edit `docs/release-notes.md` in a change: two open pull requests
that both add an entry at its top conflict, and each conflict costs a rebase
and a full CI run.

`python3 tools/release_notes.py` prints the assembled notes: the summary line,
these notes with the highest number first, then the entries already in
`docs/release-notes.md`. A release tag's `publish` job releases exactly that
output, so a note left here is never lost. Before tagging, the owner may run
`python3 tools/release_notes.py --fold` to write the assembled notes into
`docs/release-notes.md` and remove the notes it folded.
