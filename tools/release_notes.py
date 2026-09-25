"""Assemble the release notes from docs/release-notes.md and the per-change notes beside it.

A change adds its note as its own file, docs/release-notes.d/N-slug.md, where N
is its issue or pull request number, instead of editing the top of
docs/release-notes.md, so two open pull requests never conflict over it. A note
is one or more Markdown list items. The assembled notes are the summary line of
docs/release-notes.md, then the per-change notes, highest number first, then
the entries docs/release-notes.md already holds. Without an option the report
prints them, and publication releases exactly that output. --fold writes them
into docs/release-notes.md and removes the per-change notes it folded.
"""

import argparse
from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parent.parent
NAME = re.compile(r"(\d+)-[a-z0-9]+(?:-[a-z0-9]+)*\.md")


def notes(directory):
    """Return each per-change note's path and text, highest number first."""
    found = []
    for path in sorted(directory.glob("*.md")):
        if path.name == "README.md":
            continue
        named = NAME.fullmatch(path.name)
        if not named:
            raise ValueError(f"{path.name}: name a note N-slug.md, N its issue or pull request number")
        text = path.read_text(encoding="utf-8").strip()
        if not text.startswith("- "):
            raise ValueError(f"{path.name}: a note is one or more Markdown list items starting with '- '")
        found.append((int(named.group(1)), path.name, path, text))
    found.sort(key=lambda note: (-note[0], note[1]))
    return [(path, text) for _, _, path, text in found]


def assemble(release_notes, directory):
    summary, _, entries = release_notes.read_text(encoding="utf-8").partition("\n\n")
    parts = [summary.strip()] + [text for _, text in notes(directory)]
    if entries.strip():
        parts.append(entries.strip())
    return "\n\n".join(parts) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--notes", type=Path, default=ROOT / "docs" / "release-notes.md")
    parser.add_argument("--changes", type=Path, default=ROOT / "docs" / "release-notes.d")
    parser.add_argument("--fold", action="store_true",
                        help="write the assembled notes into --notes and remove the folded per-change notes")
    arguments = parser.parse_args()
    try:
        folded = notes(arguments.changes)
        assembled = assemble(arguments.notes, arguments.changes)
    except (OSError, ValueError) as error:
        print(f"release notes: {error}", file=sys.stderr)
        return 2
    if not arguments.fold:
        sys.stdout.write(assembled)
        return 0
    arguments.notes.write_text(assembled, encoding="utf-8")
    for path, _ in folded:
        path.unlink()
    return 0


if __name__ == "__main__":
    sys.exit(main())
