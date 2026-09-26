"""The release contract: which files ship, and where each comes from.

tools/distribution.json declares every release-archive member except the
executable, the checkout folder a member is taken from when its archive path
differs, and the desktop packages' legal material as a named subset of the
members. Everything that packages or checks shipped files reads them here.
"""

import json
from pathlib import Path
import re


ROOT = Path(__file__).resolve().parent.parent
SCHEMA = "readmit-distribution/v1"


def _manifest():
    manifest = json.loads(Path(__file__).with_name("distribution.json").read_text())
    members, legal, sources = manifest.get("members"), manifest.get("desktop_legal"), manifest.get("sources")
    if (manifest.get("schema") != SCHEMA or not isinstance(members, list) or not members
            or len(set(members)) != len(members) or not isinstance(sources, dict)):
        raise RuntimeError("distribution manifest is invalid")
    if not isinstance(legal, list) or not legal or not set(legal) <= set(members):
        raise RuntimeError("the desktop legal material must be a subset of the distribution members")
    return manifest


_MANIFEST = _manifest()


def members():
    """Every archive member other than the executable, by archive path."""
    return tuple(_MANIFEST["members"])


def desktop_legal():
    """The members every desktop package installs as its legal material."""
    return tuple(_MANIFEST["desktop_legal"])


def source(member):
    """The checkout file an archive member is taken from."""
    for prefix, folder in _MANIFEST["sources"].items():
        if member.startswith(prefix):
            return ROOT / (folder + member.removeprefix(prefix))
    return ROOT / member


def goreleaser_files(text=None):
    """The checkout files goreleaser's archive takes, by archive path.

    goreleaser's `files:` is a copy of the manifest kept in its own syntax; a
    test requires the two to agree, so a file only goreleaser ships fails.
    """
    text = (ROOT / ".goreleaser.yml").read_text() if text is None else text
    block = re.search(r"^    files:\n((?:      .*\n|[ \t]*\n)*)", text, re.M).group(1)
    shipped = {}
    for entry in re.split(r"^      - ", block, flags=re.M)[1:]:
        fields = dict(re.findall(r"^\s*(\w+): (.+)$", entry, re.M))
        if not fields:
            for path in sorted(ROOT.glob(entry.strip())):
                if path.is_file():
                    shipped[path.relative_to(ROOT).as_posix()] = path
        elif fields.get("strip_parent") == "true" and set(fields) == {"src", "dst", "strip_parent"}:
            shipped[fields["dst"] + "/" + Path(fields["src"]).name] = ROOT / fields["src"]
        else:
            raise RuntimeError(f"unsupported goreleaser files entry: {entry.strip()}")
    return shipped
