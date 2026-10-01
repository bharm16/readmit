import hashlib
import io
import json
import pathlib
import tarfile
import tempfile
import unittest

from tools.fhir_validator import build, capability, inventory


class ArchiveSafetyTests(unittest.TestCase):
    def archive(self, entries):
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w:gz") as archive:
            for name, data, link in entries:
                member = tarfile.TarInfo(name)
                member.mode = 0o755 if name.endswith("/java") else 0o644
                if link is not None:
                    member.type = tarfile.SYMTYPE
                    member.linkname = link
                    archive.addfile(member)
                else:
                    member.size = len(data)
                    archive.addfile(member, io.BytesIO(data))
        return raw.getvalue()

    def test_internal_jre_license_link_is_materialized_without_symlinks(self):
        raw = self.archive([("runtime/bin/java", b"binary", None), ("runtime/legal/base/LICENSE", b"notice", None), ("runtime/legal/other/LICENSE", b"", "../base/LICENSE")])
        with tempfile.TemporaryDirectory() as root:
            output = pathlib.Path(root) / "stage"
            capability.unpack(raw, output, "runtime")
            self.assertEqual((output / "legal/other/LICENSE").read_bytes(), b"notice")
            self.assertFalse((output / "legal/other/LICENSE").is_symlink())
            self.assertEqual((output / "bin/java").stat().st_mode & 0o777, 0o755)

    def test_paths_duplicates_cycles_special_files_and_bombs_refuse_before_writes(self):
        cases = [
            [("../escape", b"bad", None)],
            [("/absolute", b"bad", None)],
            [("C:/escape", b"bad", None)],
            [("runtime/NUL", b"bad", None)],
            [("runtime/trailing.", b"bad", None)],
            [("runtime/a", b"one", None), ("runtime/a", b"two", None)],
            [("runtime/a", b"", "../../outside")],
            [("runtime/a", b"", "b"), ("runtime/b", b"", "a")],
            [("runtime/a", b"file", None), ("runtime/a/child", b"bad", None)],
            [("runtime/evil\\name", b"bad", None)],
        ]
        for entries in cases:
            with self.subTest(entries=entries), tempfile.TemporaryDirectory() as root:
                output = pathlib.Path(root) / "stage"
                with self.assertRaises(capability.Refused):
                    capability.unpack(self.archive(entries), output, "runtime")
                self.assertFalse(output.exists())
        raw = self.archive([("runtime/large", b"x" * 20, None)])
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(capability.Refused):
                capability.unpack(raw, pathlib.Path(root) / "stage", "runtime", max_bytes=10)
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w:gz") as archive:
            entry = tarfile.TarInfo("runtime/fifo")
            entry.type = tarfile.FIFOTYPE
            archive.addfile(entry)
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(capability.Refused):
                capability.unpack(raw.getvalue(), pathlib.Path(root) / "stage", "runtime")

    def test_wrong_hash_and_changed_package_dependency_are_not_accepted(self):
        with tempfile.TemporaryDirectory() as root:
            path = pathlib.Path(root) / "asset"
            path.write_bytes(b"untrusted")
            with self.assertRaises(capability.Refused):
                capability.verified_bytes(path, {"size": 9, "sha256": hashlib.sha256(b"trusted").hexdigest()})
        packages = [{"name": "readmit.test", "version": "1.0.0", "dependencies": {"hl7.fhir.r4.core": "4.0.1"}}, {"name": "hl7.fhir.r4.core", "version": "4.0.1"}]
        self.assertEqual(capability.package_order(packages), ["hl7.fhir.r4.core#4.0.1", "readmit.test#1.0.0"])
        packages[0]["dependencies"]["hl7.fhir.r4.core"] = "current"
        with self.assertRaises(capability.Refused):
            capability.package_order(packages)


class SuppliedPackageTests(unittest.TestCase):
    def package(self, declaration):
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w:gz") as archive:
            data = json.dumps(declaration).encode()
            member = tarfile.TarInfo("package/package.json")
            member.size = len(data)
            archive.addfile(member, io.BytesIO(data))
        return raw.getvalue()

    def test_a_supplied_package_is_named_only_by_an_exact_identity(self):
        self.assertEqual(build.package_key(self.package({"name": "example.ig", "version": "1.2.0"})), "example.ig#1.2.0")
        for declaration in ({"name": "../escape", "version": "1.0.0"}, {"name": "example.ig", "version": "current"}, {"name": "Example", "version": "1.0.0"}, {"version": "1.0.0"}):
            with self.subTest(declaration=declaration), self.assertRaises(capability.Refused):
                build.package_key(self.package(declaration))
        with self.assertRaises(capability.Refused):
            build.package_key(b"not an archive")

    def test_the_qualification_package_folder_packs_reproducibly(self):
        folder = capability.ROOT / "testdata/fhir-validation/package"
        self.assertEqual(inventory.owned_package(folder), inventory.owned_package(folder))
        self.assertEqual(build.package_key(inventory.owned_package(folder)), "readmit.synthetic.validation#1.0.0")


class PinTests(unittest.TestCase):
    def test_every_acquired_input_is_an_exact_release_never_a_mutable_latest(self):
        pins = capability.load_pins()
        self.assertIn("@sha256:", pins["base_image"])
        for asset in pins["assets"]:
            with self.subTest(asset=asset["id"]):
                url = asset["url"].lower()
                self.assertTrue(url.startswith("https://"))
                for mutable in ("latest", "current", "snapshot", "nightly"):
                    self.assertNotIn(mutable, url)
                self.assertRegex(asset["version"], r"^[0-9]+(\.[0-9]+)+")
                self.assertIn(asset["version"].replace("+", "%2B"), asset["url"])
                self.assertRegex(asset["sha256"], r"^[0-9a-f]{64}$")
                self.assertGreater(asset["size"], 0)


if __name__ == "__main__":
    unittest.main()
