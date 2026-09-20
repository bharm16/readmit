"""Exercise desktop packaging through the tool that builds and verifies it."""

from contextlib import redirect_stderr
import io
import json
import os
from pathlib import Path
import platform
import plistlib
import stat
import sys
import tempfile
import time
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

    def test_installed_legal_material_is_present_in_the_linux_payload(self):
        output = self.build()
        package = next(output.glob("*.deb"))
        data = packaging.read_tar_gz(packaging.read_ar(package)["data.tar.gz"])
        for name in ("licenses/react-MIT.txt", "licenses/wails-MIT.txt",
                     "dictionary/fields-v251.json", "docs/dictionary-provenance.md"):
            self.assertEqual(data["usr/share/doc/readmit-desktop/" + name][1],
                             (packaging.ROOT / ("internal/" + name if name.startswith("dictionary/") else name)).read_bytes())

    def test_missing_or_altered_legal_material_is_refused_even_with_a_new_checksum(self):
        for index, name in enumerate(("licenses/react-MIT.txt", "dictionary/fields-v251.json")):
            with self.subTest(name=name):
                output = self.build(name=f"legal-{index}")
                package = next(output.glob("*.deb"))
                members = packaging.read_ar(package)
                data = packaging.read_tar_gz(members["data.tar.gz"])
                data["usr/share/doc/readmit-desktop/" + name] = (0o644, b"altered")
                packaging.write_ar(package, [
                    ("debian-binary", members["debian-binary"]),
                    ("control.tar.gz", members["control.tar.gz"]),
                    ("data.tar.gz", packaging.tar_gz([(key, mode, value) for key, (mode, value) in data.items()])),
                ])
                self.rewrite(output, package.name, package.read_bytes())
                with self.assertRaisesRegex(packaging.Refused, "legal material"):
                    packaging.verify(self.declaration, output)

    def test_bundle_without_license_text_is_refused(self):
        bundle = packaging.application_bundle(self.declaration, self.binary, "1.2.3", self.work)
        (bundle / "Contents/Resources/licenses/wails-MIT.txt").unlink()
        with self.assertRaisesRegex(packaging.Refused, "legal material"):
            packaging.verify_bundle(self.declaration, "1.2.3", bundle)

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
        bundle = packaging.application_bundle(self.declaration, self.binary, "0.1.0-alpha.2", output)
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

    def test_a_packaging_tool_that_fails_is_refused_in_its_own_words(self):
        """What the tool wrote is the diagnosis; an exit status on its own is not.

        `check=True` put that text in an exception nobody read, so four failures
        of the disk-image step reported a command line and a status and no reason.
        """
        with self.assertRaises(packaging.Refused) as refused:
            packaging.run_tool([
                sys.executable, "-c",
                "import sys; sys.stderr.write('why it refused\\n');"
                " sys.stdout.write('what it had done\\n'); sys.exit(3)",
            ], "the stand-in tool did not run")
        message = str(refused.exception)
        for expected in ("the stand-in tool did not run", "exited 3",
                         "why it refused", "what it had done"):
            self.assertIn(expected, message)

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_an_image_that_does_not_attach_is_refused_in_hdiutil_s_own_words(self):
        image = self.work / "readmit-desktop_1.2.3_arm64.dmg"
        image.write_bytes(b"a file with a disk image's name and none of its bytes")
        with self.assertRaises(packaging.Refused) as refused:
            with packaging.mounted(image):
                self.fail("nothing is read out of an image that never attached")
        message = str(refused.exception)
        for expected in (image.name, "did not attach", "hdiutil exited 1", "attach failed"):
            self.assertIn(expected, message)

    def detach_before_mounted_does(self, volume):
        """Detach the volume the way no caller does, so the detach `mounted()`
        runs next has nothing left to detach and has to say so.

        `hdiutil detach` can return before the unmount has landed, so waiting
        for the system to agree is what makes the next detach fail every time
        rather than most times."""
        packaging.run_tool(["hdiutil", "detach", "-force", str(volume)], "the test detached it")
        for _ in range(600):
            if not volume.is_mount():
                return
            time.sleep(0.1)
        self.fail("the volume this test detached is still mounted a minute later")

    @unittest.skipIf(platform.system() != "Darwin", "pkgutil is a macOS tool")
    def test_an_installer_package_that_does_not_expand_is_refused_in_pkgutil_s_own_words(self):
        package = self.work / "readmit-desktop_1.2.3_arm64.pkg"
        package.write_bytes(b"a file with an installer package's name and none of its bytes")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify_installer_package(self.declaration, "1.2.3", package)
        message = str(refused.exception)
        for expected in (package.name, "did not expand", "Could not open product archive"):
            self.assertIn(expected, message)

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_an_image_this_tool_cannot_detach_is_refused_and_not_leaked_in_silence(self):
        """A detach that fails leaves the image attached after this process ends.
        Ignoring its result left two such attachments on a machine (#104), and
        the tool that put them there could not say that it had."""
        output = self.build(selected=MACOS, name="detach-packages")
        image = next(output.glob("*.dmg"))
        with self.assertRaises(packaging.Refused) as refused:
            with packaging.mounted(image) as volume:
                self.detach_before_mounted_does(volume)
        message = str(refused.exception)
        for expected in (image.name, "did not detach", "detach failed"):
            self.assertIn(expected, message)

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_a_refusal_about_the_image_survives_a_detach_that_also_fails(self):
        """The refusal a caller gets is about the package, never about cleanup."""
        output = self.build(selected=MACOS, name="masked-packages")
        image = next(output.glob("*.dmg"))
        reported = io.StringIO()
        with self.assertRaises(packaging.Refused) as refused, redirect_stderr(reported):
            with packaging.mounted(image) as volume:
                self.detach_before_mounted_does(volume)
                raise packaging.Refused("what the image holds is not what it claims")
        self.assertEqual(str(refused.exception), "what the image holds is not what it claims")
        self.assertIn("did not detach", reported.getvalue())

    @unittest.skipIf(platform.system() != "Darwin", "codesign is a macOS tool")
    def test_an_ad_hoc_signature_is_not_a_distribution_signature(self):
        """Apple silicon links every executable with an ad-hoc signature, so
        `codesign` succeeding is not evidence that anything was signed for
        distribution. Only a named signing authority is."""
        output = self.work / "bundle"
        output.mkdir()
        bundle = packaging.application_bundle(self.declaration, self.binary, "1.2.3", output)
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

    def test_native_startup_requires_webview_readiness_not_just_version(self):
        import subprocess
        shell = self.executable("native", "readmit-desktop native webview ready")
        def check():
            return subprocess.run([sys.executable, str(packaging.ROOT / "tools/package_desktop.py"),
                                   "startup", "--desktop", str(shell)], capture_output=True, text=True)
        self.assertEqual(check().returncode, 0)
        shell.write_text("#!/bin/sh\nprintf 'readmit-desktop version 1.2.3\\n'\n")
        self.assertNotEqual(check().returncode, 0)
        shell.write_text("#!/bin/sh\nprintf 'readmit-desktop native webview ready\\n'\nexit 2\n")
        self.assertNotEqual(check().returncode, 0)

    def test_one_build_is_reported_by_the_package_and_the_archive_alike(self):
        shell = self.executable("readmit-desktop", "readmit-desktop version 0.1.0-alpha.2")
        released = self.executable("readmit", "readmit version 0.1.0-alpha.2")
        self.assertEqual(packaging.identity(shell, released), "0.1.0-alpha.2")

    def check_installed(self, shell, released, version="1.2.3"):
        import subprocess
        resources = self.work / "resources"
        if not resources.exists():
            packaging.stage_legal_material(resources)
        return subprocess.run([
            sys.executable, str(packaging.ROOT / "tools/package_desktop.py"), "installed",
            "--desktop", str(shell), "--command-line", str(released),
            "--resources", str(resources), "--version", version,
        ], capture_output=True, text=True)

    def test_public_installed_check_requires_expected_identity_and_complete_notices(self):
        shell = self.executable("readmit-desktop", "readmit-desktop version 1.2.3")
        released = self.executable("readmit", "readmit version 1.2.3")
        self.assertEqual(self.check_installed(shell, released).returncode, 0)
        # Agreement alone is insufficient: both executables could be stale.
        self.assertNotEqual(self.check_installed(shell, released, "1.2.4").returncode, 0)
        (self.work / "resources/licenses/react-MIT.txt").unlink()
        result = self.check_installed(shell, released)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("legal material", result.stderr)

    def test_installed_check_does_not_find_hidden_runtime_on_path(self):
        shell = self.executable("readmit-desktop", "readmit-desktop version 1.2.3")
        released = self.executable("readmit", "readmit version 1.2.3")
        shell.write_text("#!/bin/sh\nif command -v python3 >/dev/null 2>&1; then printf 'readmit-desktop version 1.2.3\\n'; else exit 7; fi\n")
        result = self.check_installed(shell, released)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did not report a version", result.stderr)

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
