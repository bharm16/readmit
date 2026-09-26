"""Hold every copy of the shipped-file list to tools/distribution.json."""

from pathlib import Path
import unittest

import distribution


class DistributionTests(unittest.TestCase):
    def test_goreleaser_ships_exactly_the_declared_members_from_their_sources(self):
        shipped = distribution.goreleaser_files()
        self.assertEqual(set(shipped), set(distribution.members()))
        for member, path in shipped.items():
            with self.subTest(member=member):
                self.assertEqual(path, distribution.source(member))
                self.assertTrue(path.is_file())

    def test_a_file_only_goreleaser_ships_is_detected(self):
        text = (distribution.ROOT / ".goreleaser.yml").read_text()
        extra = text.replace("      - README.md\n", "      - README.md\n      - CLAUDE.md\n", 1)
        self.assertIn("CLAUDE.md", set(distribution.goreleaser_files(extra)) - set(distribution.members()))

    def test_every_license_text_is_desktop_legal_material(self):
        licenses = {member for member in distribution.members() if member.startswith("licenses/")}
        self.assertTrue(licenses)
        self.assertLessEqual(licenses, set(distribution.desktop_legal()))

    def test_a_source_folder_is_declared_once(self):
        self.assertEqual(distribution.source("dictionary/fields-v251.json"),
                         distribution.ROOT / "internal/dictionary/fields-v251.json")
        self.assertEqual(distribution.source("docs/index.md"), distribution.ROOT / "docs/index.md")


if __name__ == "__main__":
    unittest.main()
