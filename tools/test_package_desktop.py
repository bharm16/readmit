"""Exercise desktop packaging through the tool that builds and verifies it."""

import json
import os
from pathlib import Path
import platform
import plistlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).parent))

import package_desktop as packaging


LINUX = ("linux", "amd64")
MACOS = ("darwin", "arm64")


class PackagingTests(unittest.TestCase):
    def setUp(self):
        self.declaration = packaging.read_declaration()
        directory = tempfile.TemporaryDirectory(prefix="readmit-desktop-packaging-tests-")
        self.addCleanup(directory.cleanup)
        self.work = Path(directory.name)
        self.binary = self.work / "readmit-desktop"
        self.binary.write_bytes(b"not a real shell, but the bytes a package carries\n")

    def target(self, selected):
        return packaging.select_target(self.declaration, *selected)

    def build(self, selected=LINUX, version="1.2.3-alpha.4", name="packages"):
        output = self.work / name
        packaging.build(self.declaration, self.binary, version, self.target(selected), output)
        return output

    def rewrite(self, output, name, payload):
        """Replace a built package and record its new checksum, so a later refusal
        is about the package itself and never about a stale checksum."""
        (output / name).write_bytes(payload)
        manifest = json.loads((output / packaging.MANIFEST_NAME).read_bytes())
        for package in manifest["packages"]:
            if package["name"] == name:
                package["sha256"] = packaging.digest(output / name)
        (output / packaging.MANIFEST_NAME).write_text(json.dumps(manifest, indent=2) + "\n")

    def edit_manifest(self, output, change):
        manifest = json.loads((output / packaging.MANIFEST_NAME).read_bytes())
        change(manifest)
        (output / packaging.MANIFEST_NAME).write_text(json.dumps(manifest, indent=2) + "\n")

    def test_the_repository_declares_every_package_this_release_builds(self):
        targets = {(target["os"], target["arch"]) for target in self.declaration["targets"]}
        self.assertEqual(targets, {
            ("linux", "amd64"), ("linux", "arm64"),
            ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64"),
        })
        self.assertEqual(self.target(("windows", "amd64"))["formats"], ["msi"])

    def test_a_linux_package_declares_the_libraries_the_window_needs(self):
        output = self.build()
        document = packaging.verify(self.declaration, output)
        self.assertFalse(document["signed_for_distribution"])
        self.assertEqual([package["format"] for package in document["packages"]], ["deb"])
        members = packaging.read_ar(output / document["packages"][0]["name"])
        control = packaging.read_tar_gz(members["control.tar.gz"])["control"][1].decode()
        for dependency in self.declaration["debian_dependencies"]:
            self.assertIn(dependency, control)
        self.assertIn("Version: 1.2.3-alpha.4", control)
        data = packaging.read_tar_gz(members["data.tar.gz"])
        self.assertTrue(data["usr/bin/readmit-desktop"][0] & stat.S_IXUSR)
        self.assertEqual(data["usr/bin/readmit-desktop"][1], self.binary.read_bytes())
        self.assertIn("usr/share/applications/readmit-desktop.desktop", data)

    def test_the_same_inputs_build_the_same_package_bytes(self):
        first, second = self.build(name="first"), self.build(name="second")
        name = json.loads((first / packaging.MANIFEST_NAME).read_bytes())["packages"][0]["name"]
        self.assertEqual((first / name).read_bytes(), (second / name).read_bytes())

    def test_a_tampered_package_is_refused_against_its_own_checksum(self):
        output = self.build()
        name = json.loads((output / packaging.MANIFEST_NAME).read_bytes())["packages"][0]["name"]
        payload = bytearray((output / name).read_bytes())
        payload[-1] ^= 0xFF
        (output / name).write_bytes(bytes(payload))
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("does not match the checksum", str(refused.exception))

    def test_a_package_that_drops_a_declared_dependency_is_refused(self):
        output = self.build()
        name = json.loads((output / packaging.MANIFEST_NAME).read_bytes())["packages"][0]["name"]
        members = packaging.read_ar(output / name)
        control = packaging.read_tar_gz(members["control.tar.gz"])["control"][1].decode()
        dropped = control.replace(
            f"Depends: {', '.join(self.declaration['debian_dependencies'])}",
            f"Depends: {self.declaration['debian_dependencies'][0]}",
        )
        packaging.write_ar(output / name, [
            ("debian-binary", members["debian-binary"]),
            ("control.tar.gz", packaging.tar_gz([("./control", 0o644, dropped.encode())])),
            ("data.tar.gz", members["data.tar.gz"]),
        ])
        self.rewrite(output, name, (output / name).read_bytes())
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn(self.declaration["debian_dependencies"][1], str(refused.exception))

    def test_a_package_that_installs_no_executable_is_refused(self):
        output = self.build()
        name = json.loads((output / packaging.MANIFEST_NAME).read_bytes())["packages"][0]["name"]
        members = packaging.read_ar(output / name)
        packaging.write_ar(output / name, [
            ("debian-binary", members["debian-binary"]),
            ("control.tar.gz", members["control.tar.gz"]),
            ("data.tar.gz", packaging.tar_gz([("./usr/bin/readmit-desktop", 0o644, b"unrunnable")])),
        ])
        self.rewrite(output, name, (output / name).read_bytes())
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("/usr/bin/readmit-desktop", str(refused.exception))

    def test_a_manifest_written_by_another_release_is_refused(self):
        for index, (change, expected) in enumerate((
            (lambda manifest: manifest.update(schema="readmit-desktop-package/v2"), "readmit-desktop-package/v1"),
            (lambda manifest: manifest.update(notarized=True), "does not read"),
            (lambda manifest: manifest.pop("signed_for_distribution"), "missing"),
            (lambda manifest: manifest["packages"][0].update(signature="stapled"), "does not read"),
        )):
            with self.subTest(expected=expected):
                output = self.build(name=f"manifest-{index}")
                self.edit_manifest(output, change)
                with self.assertRaises(packaging.Refused) as refused:
                    packaging.verify(self.declaration, output)
                self.assertIn(expected, str(refused.exception))

    def test_a_manifest_that_claims_a_distribution_signature_is_refused(self):
        output = self.build()
        self.edit_manifest(output, lambda manifest: manifest.update(signed_for_distribution=True))
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("signs nothing for distribution", str(refused.exception))

    def test_a_manifest_that_omits_a_declared_format_is_refused(self):
        output = self.work / "macos"
        output.mkdir()
        name = "readmit-desktop_1.2.3_arm64.dmg"
        (output / name).write_bytes(b"a disk image stands in for the packages hdiutil writes")
        (output / packaging.MANIFEST_NAME).write_text(json.dumps({
            "schema": packaging.MANIFEST_SCHEMA, "version": "1.2.3", "os": "darwin",
            "arch": "arm64", "signed_for_distribution": False,
            "packages": [{"name": name, "format": "dmg", "sha256": packaging.digest(output / name)}],
        }, indent=2) + "\n")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("pkg", str(refused.exception))

    def test_a_manifest_pointing_outside_its_own_directory_is_refused(self):
        output = self.build()
        self.edit_manifest(output, lambda manifest: manifest["packages"][0].update(name="../escape.deb"))
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("not a package beside it", str(refused.exception))

    def test_a_declaration_written_by_another_release_is_refused(self):
        document = json.loads(packaging.DECLARATION.read_bytes())
        for change, expected in (
            (lambda value: value.update(schema="readmit-desktop-packaging/v2"), "readmit-desktop-packaging/v1"),
            (lambda value: value.update(signing_identity="Developer ID"), "does not read"),
            (lambda value: value.pop("debian_dependencies"), "missing"),
            (lambda value: value["targets"][0].update(formats=["snap"]), "unsupported package formats"),
            (lambda value: value["webview2"].pop("message"), "missing"),
            (lambda value: value.update(debian_dependencies=[]), "platform libraries"),
        ):
            with self.subTest(expected=expected):
                edited = dict(document)
                edited["targets"] = [dict(target) for target in document["targets"]]
                edited["webview2"] = dict(document["webview2"])
                change(edited)
                path = self.work / "declaration.json"
                path.write_text(json.dumps(edited))
                with self.assertRaises(packaging.Refused) as refused:
                    packaging.read_declaration(path)
                self.assertIn(expected, str(refused.exception))

    def test_a_target_this_release_does_not_package_is_refused(self):
        for selected in (("windows", "arm64"), ("freebsd", "amd64")):
            with self.subTest(target=selected), self.assertRaises(packaging.Refused) as refused:
                packaging.select_target(self.declaration, *selected)
            self.assertIn("no desktop package is declared", str(refused.exception))

    def test_a_version_no_package_format_can_carry_is_refused(self):
        for version in ("", "v1.2.3", "1.2.3 4", "1.2/3", "release"):
            with self.subTest(version=version), self.assertRaises(packaging.Refused):
                packaging.check_version(version)
        for version in ("1.2", "1.2.3.4", "1.2.x"):
            with self.subTest(version=version), self.assertRaises(packaging.Refused):
                packaging.numeric_version(version)
        self.assertEqual(packaging.numeric_version("0.1.0-alpha.2"), "0.1.0")
        self.assertEqual(packaging.numeric_version("0.0.0+dev.abc1234"), "0.0.0")

    def test_the_macos_bundle_carries_the_full_release_identity(self):
        output = self.work / "bundle"
        output.mkdir()
        bundle = packaging.application_bundle(self.declaration, self.binary, b"notices", "0.1.0-alpha.2", output)
        packaging.verify_bundle(self.declaration, "0.1.0-alpha.2", bundle)
        with (bundle / "Contents" / "Info.plist").open("rb") as plist:
            information = plistlib.load(plist)
        self.assertEqual(information["CFBundleShortVersionString"], "0.1.0")
        self.assertEqual(information["ReadmitBuildIdentity"], "0.1.0-alpha.2")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify_bundle(self.declaration, "0.1.0-alpha.3", bundle)
        self.assertIn("ReadmitBuildIdentity", str(refused.exception))

    def test_something_that_is_not_an_installer_database_is_refused(self):
        database = self.work / "readmit-desktop_1.2.3_x64.msi"
        database.write_bytes(b"MZ this is an executable, not an installer database")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify_installer_database(database)
        self.assertIn("not an installer database", str(refused.exception))

    @unittest.skipIf(platform.system() == "Darwin", "macOS reads its own packages")
    def test_a_macos_package_is_read_with_the_tools_that_wrote_it(self):
        image = self.work / "readmit-desktop_1.2.3_arm64.dmg"
        image.write_bytes(b"a disk image only macOS opens")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify_disk_image(self.declaration, "1.2.3", image)
        self.assertIn("on macOS", str(refused.exception))

    @unittest.skipIf(platform.system() != "Darwin", "codesign is a macOS tool")
    def test_an_ad_hoc_signature_is_not_a_distribution_signature(self):
        """Apple silicon links every executable with an ad-hoc signature, so
        `codesign` succeeding is not evidence that anything was signed for
        distribution. Only a named signing authority is."""
        output = self.work / "bundle"
        output.mkdir()
        bundle = packaging.application_bundle(self.declaration, self.binary, b"notices", "1.2.3", output)
        self.assertIsNone(packaging.distribution_authority(bundle))
        # A genuinely signed application names its authority, so "no authority"
        # is a reading of the package and not a constant this always returns.
        self.assertIsNotNone(packaging.distribution_authority(Path("/bin/ls")))

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil and pkgbuild are macOS tools")
    def test_the_macos_packages_hold_the_application_they_claim(self):
        output = self.build(selected=MACOS, name="macos-packages")
        document = packaging.verify(self.declaration, output)
        self.assertEqual({package["format"] for package in document["packages"]}, {"dmg", "pkg"})
        # Nothing but the packages and their manifest is left to be handed on.
        self.assertEqual(
            sorted(entry.name for entry in output.iterdir()),
            sorted([packaging.MANIFEST_NAME] + [package["name"] for package in document["packages"]]),
        )
        self.edit_manifest(output, lambda manifest: manifest.update(version="1.2.3-alpha.5"))
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("ReadmitBuildIdentity", str(refused.exception))

    def test_an_unbuilt_executable_is_refused_before_anything_is_written(self):
        with self.assertRaises(packaging.Refused) as refused:
            packaging.build(self.declaration, self.work / "absent", "1.2.3", self.target(LINUX), self.work / "none")
        self.assertIn("not a built desktop executable", str(refused.exception))
        self.assertFalse((self.work / "none").exists())

    def test_an_existing_output_directory_is_never_overwritten(self):
        output = self.build()
        with self.assertRaises(FileExistsError):
            packaging.build(self.declaration, self.binary, "1.2.3", self.target(LINUX), output)


@unittest.skipIf(os.name == "nt", "the stand-in executables are shell scripts")
class IdentityTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="readmit-desktop-identity-tests-")
        self.addCleanup(directory.cleanup)
        self.work = Path(directory.name)

    def executable(self, name, output):
        path = self.work / name
        path.write_text(f"#!/bin/sh\nprintf '%s\\n' '{output}'\n")
        path.chmod(0o755)
        return path

    def test_one_build_is_reported_by_the_package_and_the_archive_alike(self):
        shell = self.executable("readmit-desktop", "readmit-desktop version 0.1.0-alpha.2")
        released = self.executable("readmit", "readmit version 0.1.0-alpha.2")
        self.assertEqual(packaging.identity(shell, released), "0.1.0-alpha.2")

    def test_two_builds_reporting_different_engines_are_refused(self):
        shell = self.executable("readmit-desktop", "readmit-desktop version 0.1.0-alpha.2")
        released = self.executable("readmit", "readmit version 0.1.0-alpha.3")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.identity(shell, released)
        self.assertIn("0.1.0-alpha.3", str(refused.exception))

    def test_an_executable_that_reports_no_identity_is_refused(self):
        silent = self.work / "silent"
        silent.write_text("#!/bin/sh\nexit 3\n")
        silent.chmod(0o755)
        released = self.executable("readmit", "readmit version 0.1.0-alpha.2")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.identity(silent, released)
        self.assertIn("did not report a version", str(refused.exception))


if __name__ == "__main__":
    unittest.main()
