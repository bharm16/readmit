#!/usr/bin/env python3
"""Repeatable product-label coverage check for issue #512.

The reviewed label inventory lives in docs/labels/inventory.json: one decision
(rename / icon / remove / keep) per reviewed label occurrence, each recording
the exact current string, the exact final string, the owning source path and
the review's conditions. This tool keeps that inventory and the presentation
sources honest in both directions:

* regression gate (always enforced): an implemented rename/remove/icon no
  longer has its old string at its recorded path; an implemented rename/icon
  still has its final string (for icons, the accessible name) there; a keep
  still has its current string there;
* coverage gate: every label candidate extracted from a first-party
  presentation source is either covered by a decision for that file, covered
  by an explicit recorded exclusion with a reason, or counted against that
  file's pending review. `--strict` fails while any file is pending or any
  candidate is uncovered, which is the issue's closure gate; the default run
  reports progress and fails only on regressions or on decisions whose status
  claims implementation the source does not show.

Candidate extraction is deliberately source-aware but conservative: it reads
text children of label-bearing elements (buttons, links, headings, labels,
legends, options, disclosure summaries), the naming attributes (aria-label,
title, placeholder), the shell's declared region/command/indicator strings in
Go, and the static site's headings, navigation and controls. Dynamic labels
composed from several elements are recorded as template exclusions rather than
pretended to be enumerable; the exclusion carries the template and its reason.

The check supports contextual review; it does not replace it. It does not
impose a length limit or vocabulary rule of its own.
"""

import argparse
import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
INVENTORY = REPO / "docs" / "labels" / "inventory.json"
WORKLIST = REPO / "docs" / "labels" / "worklist.json"

# First-party presentation sources this check reads. The desktop frontend's
# tests and journeys, the generated bindings and the testkit are not shipped
# presentation; the bindings' call policy and the icon glyphs carry no product
# labels. Everything else under src is in scope.
FRONTEND_SRC = REPO / "desktop" / "frontend" / "src"
FRONTEND_SKIP = {
    "bindings.gen.ts",
    "bindings.ts",
    "IconButton.tsx",
}
GO_LABEL_SOURCES = [
    REPO / "internal" / "desktop" / "shell.go",
]
SITE = REPO / "site"

TSX_NAMED = re.compile(
    r"<(button|a|h[1-6]|summary|legend|label|option)\b[^>]*>([^<>{}]*[^<>\s{}][^<>{}]*)</\1>",
    re.S,
)
TSX_NAMING_ATTRS = re.compile(
    r'\b(?:aria-label|title|placeholder)="([^"\n]+)"'
)
GO_LABELLED = re.compile(r'\b(?:Label|Title):\s*"([^"\n]+)"')
HTML_NAMED = re.compile(
    r"<(h[1-6]|a|button|summary|label|option)\b[^>]*>([^<>]*[^<>\s][^<>]*)</\1>",
    re.S,
)
HTML_NAMING_ATTRS = re.compile(r'\b(?:aria-label|title)="([^"\n]+)"')

STATIC_TEXT = re.compile(r"^[A-Za-z0-9…].*$")


def clean(text: str) -> str:
    """Collapse the whitespace a JSX child or HTML text node can carry so a
    label broken across lines still compares equal to itself."""
    return re.sub(r"\s+", " ", text).strip()


def normalized(text: str) -> str:
    """A source file with the same whitespace collapsed, so a label the markup
    wraps across lines is still present in it."""
    return re.sub(r"\s+", " ", text)


def alternatives(label: str) -> list[str]:
    """The branches one recorded label names, separated by ` / `."""
    return [part.strip() for part in label.split(" / ") if part.strip()]


def template_patterns(label: str) -> list[re.Pattern]:
    """The match patterns one recorded label yields. A conditional final names
    its alternatives separated by ` / `; each must be present. `{placeholder}`
    stands for a value bound at render time and matches a bound run."""
    patterns = []
    for alternative in alternatives(label):
        escaped = re.escape(clean(alternative))
        # re.escape writes the braces of a placeholder out escaped; either
        # form becomes one bounded wildcard.
        body = re.sub(r"\\\{[^}]*\\\}|\{[^}]*\}", r'[^"<]{1,120}', escaped)
        patterns.append(re.compile(body))
    return patterns


def frontend_files():
    if not FRONTEND_SRC.is_dir():
        return []
    files = []
    for path in sorted(FRONTEND_SRC.rglob("*.tsx")):
        if path.name.endswith((".test.tsx", ".journey.tsx")):
            continue
        if "testkit" in path.parts:
            continue
        if path.name in FRONTEND_SKIP:
            continue
        files.append(path)
    return files


def presentation_files():
    files = frontend_files()
    files += [p for p in GO_LABEL_SOURCES if p.exists()]
    if SITE.is_dir():
        files += sorted(SITE.glob("*.html"))
    return files


def extract(path: Path):
    """Label candidates of one presentation source, as (string, kind) pairs."""
    text = path.read_text(encoding="utf-8")
    found = []
    if path.suffix == ".tsx":
        for match in TSX_NAMED.finditer(text):
            label = clean(match.group(2))
            if label:
                found.append((label, f"{match.group(1)} text"))
        for match in TSX_NAMING_ATTRS.finditer(text):
            found.append((clean(match.group(1)), "naming attribute"))
    elif path.suffix == ".go":
        for match in GO_LABELLED.finditer(text):
            found.append((clean(match.group(1)), "declared label"))
    else:
        for match in HTML_NAMED.finditer(text):
            label = clean(match.group(2))
            if label:
                found.append((label, f"{match.group(1)} text"))
        for match in HTML_NAMING_ATTRS.finditer(text):
            found.append((clean(match.group(1)), "naming attribute"))
    return [(s, kind) for s, kind in found if STATIC_TEXT.match(s)]


def rel(path: Path) -> str:
    try:
        return str(path.resolve().relative_to(REPO))
    except ValueError:
        return str(path)


def read_source(path: str) -> str | None:
    """The bytes of one recorded source path, resolved from the repository
    root first so the check runs from any working directory."""
    for candidate in (Path(REPO, path), Path(path)):
        if candidate.exists():
            return candidate.read_text(encoding="utf-8")
    return None


def exclusion_matches(exclusion, path: str, candidate: str) -> bool:
    if "path" in exclusion and exclusion["path"] != path:
        return False
    if "exact" in exclusion:
        return candidate == exclusion["exact"]
    if "pattern" in exclusion:
        return re.search(exclusion["pattern"], candidate) is not None
    return False


class Coverage:
    def __init__(self, inventory: dict):
        self.inventory = inventory
        self.by_path: dict[str, list[dict]] = {}
        for decision in inventory.get("decisions", []):
            self.by_path.setdefault(decision["path"], []).append(decision)
        self.exclusions = inventory.get("exclusions", [])
        self.pending = set(inventory.get("pending", []))

    def covered(self, path: str, candidate: str) -> bool:
        for decision in self.by_path.get(path, []):
            if candidate in [decision.get("current")] + alternatives(decision.get("final", "")):
                return True
        return any(exclusion_matches(e, path, candidate) for e in self.exclusions)

    def regressions(self, files=None) -> list[str]:
        problems = []
        for path, decisions in sorted(self.by_path.items()):
            text = read_source(path)
            if text is None:
                problems.append(f"{path}: decision records a path that does not exist")
                continue
            flat = normalized(text)
            for decision in decisions:
                status = decision.get("status", "implemented")
                if status != "implemented":
                    continue
                current, final = decision.get("current", ""), decision.get("final", "")
                kind = decision["decision"]
                # A conditional final names each alternative; every branch the
                # interface can render must be in place, never a slash list.
                # A `{placeholder}` stands for a bound value.
                finals = template_patterns(final) if final else []
                # The entry's condition may keep the old wording alive beside
                # the new label — as helper text, a hint or an accessible
                # group name. `survives` records that, so the old string's
                # presence is expected rather than a regression.
                survives = decision.get("survives")
                # The audit's Current column abbreviates an either/or pair as
                # "A / B"; the longest wording is the string that was rendered.
                # An icon's accessible name may also be its old visible text,
                # in which case the current string is the final one.
                final_alternatives = alternatives(final) if final else []
                current_alternatives = alternatives(current) if current else []
                rendered_current = max(current_alternatives, key=len) if current_alternatives else ""
                if kind in ("rename", "icon", "remove") and current and not survives \
                        and rendered_current not in final_alternatives:
                    if any(p.search(flat) for p in template_patterns(rendered_current)):
                        problems.append(f"{path}: {decision['id']} is {kind}d but its current string is still present: {current!r}")
                if kind in ("rename", "icon") and finals:
                    if not all(p.search(flat) for p in finals):
                        where = "accessible name" if kind == "icon" else "final string"
                        problems.append(f"{path}: {decision['id']} is implemented but its {where} is missing: {final!r}")
                if kind == "keep" and current_alternatives and not all(p.search(flat) for p in template_patterns(current)):
                    problems.append(f"{path}: {decision['id']} is a keep but its string is gone: {current!r}")
        return problems

    def uncovered(self, files) -> dict[str, list[tuple[str, str]]]:
        report: dict[str, list[tuple[str, str]]] = {}
        for path in files:
            name = rel(path)
            missing = []
            for candidate, kind in extract(path):
                if not self.covered(name, candidate):
                    missing.append((candidate, kind))
            if missing:
                report[name] = sorted(set(missing))
        return report


def run(strict: bool = False, update_worklist: bool = False) -> int:
    inventory = json.loads(INVENTORY.read_text(encoding="utf-8"))
    coverage = Coverage(inventory)
    files = presentation_files()
    problems = coverage.regressions()
    uncovered = coverage.uncovered(files)
    pending = sorted(coverage.pending)

    reviewed_files = {rel(p) for p in files} - set(pending)
    print(f"label decisions: {len(inventory.get('decisions', []))}")
    print(f"presentation sources: {len(files)} "
          f"({len(reviewed_files)} reviewed, {len(pending)} pending review)")
    total_missing = sum(len(v) for v in uncovered.values())
    print(f"uncovered label candidates: {total_missing}")
    for name in sorted(uncovered):
        marker = "pending" if name in coverage.pending else "REVIEWED"
        print(f"  {name} [{marker}]: {len(uncovered[name])} uncovered")
    if problems:
        print("regressions:")
        for problem in problems:
            print(f"  {problem}")
    if update_worklist:
        WORKLIST.write_text(json.dumps(
            {"uncovered": {k: [c for c, _ in v] for k, v in sorted(uncovered.items())},
             "pending": pending},
            indent=1, ensure_ascii=False) + "\n", encoding="utf-8")
        print(f"wrote {rel(WORKLIST)}")

    failed = bool(problems)
    if strict and (pending or uncovered or problems):
        failed = True
        if pending or uncovered:
            print("strict closure gate: unreviewed labels remain "
                  f"({len(pending)} pending files, {total_missing} uncovered candidates)")
    return 1 if failed else 0


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--strict", action="store_true",
                        help="fail while any file is pending review or any candidate is uncovered")
    parser.add_argument("--update-worklist", action="store_true",
                        help="rewrite docs/labels/worklist.json with the current uncovered candidates")
    args = parser.parse_args(argv)
    return run(strict=args.strict, update_worklist=args.update_worklist)


if __name__ == "__main__":
    sys.exit(main())
