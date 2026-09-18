"""Keep the mutation catalogue honest without building anything.

A mutation that no longer matches the source it names would silently stop
proving anything, so the catalogue is checked against the real tree here.
"""

from pathlib import Path
import tempfile
import unittest

import mutate
import verify


class Catalogue(unittest.TestCase):
    def test_names_are_unique(self):
        names = [mutation.name for mutation in mutate.MUTATIONS]
        self.assertEqual(len(names), len(set(names)))

    def test_every_mutation_names_a_real_check(self):
        for mutation in mutate.MUTATIONS:
            self.assertIn(mutation.check, verify.CHECKS, mutation.name)

    def test_every_mutation_still_describes_the_source_it_alters(self):
        for mutation in mutate.MUTATIONS:
            source = (mutate.ROOT / mutation.path).read_text(encoding="utf-8")
            self.assertEqual(source.count(mutation.old), 1, mutation.name)
            self.assertNotEqual(mutation.old, mutation.new, mutation.name)
            self.assertTrue(mutation.rationale, mutation.name)

    def test_every_check_is_either_mutation_covered_or_recorded_as_uncovered(self):
        covered = {mutation.check for mutation in mutate.MUTATIONS}
        uncovered = set(mutate.UNMUTATED_CHECKS)
        self.assertEqual(covered | uncovered, set(verify.CHECKS))
        self.assertFalse(covered & uncovered)


class Application(unittest.TestCase):
    def test_the_change_lands_in_the_copy_and_never_in_the_repository(self):
        mutation = mutate.MUTATIONS[0]
        original = (mutate.ROOT / mutation.path).read_bytes()
        with tempfile.TemporaryDirectory() as directory:
            copy = mutate.apply_mutation(mutation, Path(directory) / "tree")
            altered = (copy / mutation.path).read_text(encoding="utf-8")
            self.assertEqual(altered.count(mutation.old), 0)
            for name in mutate.COPIED_FILES:
                self.assertTrue((copy / name).is_file())
        self.assertEqual((mutate.ROOT / mutation.path).read_bytes(), original)

    def test_a_catalogue_entry_that_no_longer_matches_is_refused(self):
        stale = mutate.MUTATIONS[0]._replace(old="this text is not in the source", new="x")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(mutate.MutationError):
                mutate.apply_mutation(stale, Path(directory) / "tree")


if __name__ == "__main__":
    unittest.main()
