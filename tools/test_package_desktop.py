"""Exercise desktop packaging through the tool that builds and verifies it."""

from contextlib import redirect_stderr
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import stat
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))

from distribution import source
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
                             source(name).read_bytes())

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

    def test_a_packages_folder_holding_a_file_its_manifest_does_not_record_is_refused(self):
        """`readmit upgrade` refuses such a folder as a staged candidate, so
        the packaging check refuses it before it is published as one."""
        output = self.build()
        (output / "readmit-desktop_1.2.3-alpha.4_amd64.wixpdb").write_bytes(b"a build tool's debug database")
        with self.assertRaises(packaging.Refused) as refused:
            packaging.verify(self.declaration, output)
        self.assertIn("readmit-desktop_1.2.3-alpha.4_amd64.wixpdb, which its manifest does not record", str(refused.exception))

    def test_the_msi_build_keeps_wix_s_debug_database_out_of_the_packages_folder(self):
        """WiX writes a .wixpdb beside the installer unless told where. Beside
        the installer it was published with the packages, and the staged
        candidate `readmit upgrade` read was refused for holding it."""
        commands = []

        def wix(args, **kwargs):
            commands.append(list(args))
            Path(args[args.index("-o") + 1]).write_bytes(b"an installer database")
            if "-pdb" in args:
                Path(args[args.index("-pdb") + 1]).write_bytes(b"a debug database")
            else:
                Path(args[args.index("-o") + 1]).with_suffix(".wixpdb").write_bytes(b"a debug database")
            return subprocess.CompletedProcess(args, 0, "", "")

        output = self.work / "windows-packages"
        with patch.object(packaging.shutil, "which", return_value="wix"), patch.object(packaging.subprocess, "run", side_effect=wix):
            manifest = packaging.build(self.declaration, self.binary, "1.2.3", self.target(("windows", "amd64")), output)
        self.assertEqual(len(commands), 1)
        self.assertEqual((output / manifest["packages"][0]["name"]).read_bytes(), b"an installer database")
        self.assertEqual(manifest["packages"][0]["format"], "msi")
        self.assertEqual(manifest["packages"][0]["sha256"],
                         hashlib.sha256(b"an installer database").hexdigest())
        self.assertEqual(sorted(entry.name for entry in output.iterdir()),
                         sorted([packaging.MANIFEST_NAME] + [package["name"] for package in manifest["packages"]]))

    def test_a_wix_failure_is_refused_in_its_own_words_without_the_staging_path(self):
        for reason in ("exit", "timeout"):
            with self.subTest(reason=reason):
                commands = []

                def wix(args, **kwargs):
                    commands.append((list(args), kwargs))
                    staged = next(value.removeprefix("BuildDir=") for value in args
                                  if value.startswith("BuildDir="))
                    stdout = f"wix: compiling {staged}/legal.wxs\n"
                    stderr = f"wix: error: cannot read {Path(staged).resolve()}/readmit-desktop.exe\n"
                    if reason == "timeout":
                        raise subprocess.TimeoutExpired(args, 600, stdout.encode(), stderr.encode())
                    return subprocess.CompletedProcess(args, 1, stdout, stderr)

                output = self.work / f"windows-{reason}"
                with patch.object(packaging.shutil, "which", return_value="wix"), \
                        patch.object(packaging.subprocess, "run", side_effect=wix):
                    with self.assertRaises(packaging.Refused) as refused:
                        packaging.build(self.declaration, self.binary, "1.2.3",
                                        self.target(("windows", "amd64")), output)

                self.assertEqual(len(commands), 1, "no retry")
                command, options = commands[0]
                self.assertEqual(command[:5], ["wix", "build", "-nologo", "-arch", "x64"])
                staged = next(value.removeprefix("BuildDir=") for value in command
                              if value.startswith("BuildDir="))
                self.assertEqual(command[-4:], ["-o", str(output / "readmit-desktop_1.2.3_x64.msi"),
                                                str(packaging.WIX_SOURCE),
                                                str(Path(staged) / "legal.wxs")])
                self.assertEqual(command[command.index("-pdb") + 1],
                                 str(Path(staged) / "readmit-desktop_1.2.3_x64.msi.wixpdb"))
                self.assertEqual(options, {"capture_output": True, "text": True, "timeout": 600})
                message = str(refused.exception)
                outcome = "did not finish in 600 seconds" if reason == "timeout" else "wix exited 1"
                for expected in ("readmit-desktop_1.2.3_x64.msi was not built", outcome,
                                 "wix: compiling <payload>/legal.wxs",
                                 "wix: error: cannot read <payload>/readmit-desktop.exe"):
                    self.assertIn(expected, message)
                for private in (staged, str(Path(staged).resolve()), "readmit-desktop-package-"):
                    self.assertNotIn(private, message)
                self.assertFalse((output / packaging.MANIFEST_NAME).exists())
                if reason == "timeout":
                    self.assertTrue(refused.exception.__suppress_context__)

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

    def test_product_identity_and_installer_labels_are_compatibility_keeps(self):
        # #525 IP01-IP07: the display name and summary feed identifiers, the
        # installation folder and the product's visible name on every platform,
        # so a label cleanup keeps them and their single source exactly.
        declaration = self.declaration
        self.assertEqual(declaration["display_name"], "readmit")
        self.assertEqual(declaration["summary"], "Local HL7 v2 inspection and case evidence")
        self.assertEqual(declaration["product"], "readmit-desktop")
        self.assertEqual(declaration["bundle_identifier"], "com.readmit.desktop")
        self.assertEqual(declaration["upgrade_code"], "1B6F9D3E-4C5A-4F2B-9E7D-3A8C10D45E62")
        self.assertEqual(declaration["manufacturer"], "readmit")
        self.assertEqual(declaration["maintainer"], "readmit maintainers <maintainers@readmit.invalid>")
        self.assertEqual(declaration["webview2"]["message"],
                         "readmit needs the Microsoft Edge WebView2 Runtime. Install the Evergreen Standalone "
                         "Installer on this machine first; readmit downloads nothing itself.")
        self.assertEqual(packaging.package_name(declaration, "1.2.3", self.target(LINUX), "deb"),
                         "readmit-desktop_1.2.3_amd64.deb")
        entry = packaging.desktop_entry(declaration)
        self.assertIn("Name=readmit\n", entry)
        self.assertIn("Comment=Local HL7 v2 inspection and case evidence\n", entry)
        self.assertIn("Exec=/usr/bin/readmit-desktop\n", entry)
        self.assertIn("THIRD_PARTY_NOTICES.md", packaging.desktop_legal())
        # The Windows template takes every linked value from the declaration.
        wix = packaging.WIX_SOURCE.read_text(encoding="utf-8")
        for linked in ('<Package Name="$(var.DisplayName)" Manufacturer="$(var.Manufacturer)"',
                       '<SummaryInformation Description="$(var.Summary)" />',
                       'DowngradeErrorMessage="A newer version of $(var.DisplayName) is already installed."',
                       '<Launch Condition="Installed OR READMITWEBVIEW2" Message="$(var.WebView2Message)" />',
                       '<Directory Id="INSTALLFOLDER" Name="$(var.DisplayName)" />',
                       '<Feature Id="Main" Title="$(var.DisplayName)" Level="1">',
                       '<Shortcut Id="ReadmitStartMenu" Name="$(var.DisplayName)"',
                       'Source="$(var.BuildDir)\\readmit-desktop.exe"'):
            self.assertIn(linked, wix)
        # The native window keeps the product name, and the installed build
        # answers with the identity the packaging checks read.
        shell = (packaging.ROOT / "desktop" / "main.go").read_text(encoding="utf-8")
        self.assertRegex(shell, r'\n\t\tTitle:\s+"Readmit",\n')
        self.assertIn('fmt.Printf("readmit-desktop version %s\\n", engine.Version())', shell)

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

    def test_macos_icon_is_declared_and_verified_from_the_bundle(self):
        bundle = packaging.application_bundle(self.declaration, self.binary, "1.2.3", self.work)
        information = plistlib.loads((bundle / "Contents/Info.plist").read_bytes())
        icon = bundle / "Contents/Resources" / information["CFBundleIconFile"]
        self.assertEqual(icon.read_bytes()[:4], b"icns")
        self.assertEqual(icon.read_bytes(), packaging.APP_ICON.read_bytes())
        for change in (lambda: icon.write_bytes(b"wrong icon"), lambda: icon.unlink()):
            change()
            with self.assertRaisesRegex(packaging.Refused, "application icon"):
                packaging.verify_bundle(self.declaration, "1.2.3", bundle)

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

    def build_macos_with(self, create, name="retry-packages", images=None, pkgbuild=None, detach_status=0,
                         output=None):
        """Build the macOS packages with `create` standing in for `hdiutil create`
        and `pkgbuild` for pkgbuild, over `images`: the attached images `hdiutil
        info` reports, which `create` may add to and which a detach of a whole
        disk removes every image of that disk from, unless it exits `detach_status`.

        It returns what was tried: every attempt's command line, every pkgbuild
        command line, every pause, every detach with the number of creates run
        before it, what the build said on stderr, and the refusal it ended with,
        if any."""
        images = [] if images is None else images
        attempts, pkgbuilds, pauses, detached, said, refused = [], [], [], [], io.StringIO(), None

        def command(args, **kwargs):
            if args[0] == "pkgbuild":
                self.assertEqual(kwargs["timeout"], 600, "a pkgbuild that never finishes is still bounded")
                pkgbuilds.append(list(args))
                return (pkgbuild or self.installer_built)(args)
            if args[:2] == ["hdiutil", "info"]:
                return subprocess.CompletedProcess(args, 0, plistlib.dumps({"images": images}).decode(), "")
            if args[:2] == ["hdiutil", "detach"]:
                self.assertEqual(args[2], "-force")
                detached.append((args[-1], len(attempts)))
                if detach_status:
                    return subprocess.CompletedProcess(args, detach_status, "", "hdiutil: detach failed - Resource busy\n")
                images[:] = [image for image in images if not any(
                    entity["dev-entry"] == args[-1] or entity["dev-entry"].startswith(args[-1] + "s")
                    for entity in image["system-entities"])]
                return subprocess.CompletedProcess(args, 0, f'"{Path(args[-1]).name}" ejected.\n', "")
            self.assertEqual(args[:2], ["hdiutil", "create"])
            self.assertEqual(kwargs["timeout"], 600, "a create that never finishes is still bounded")
            attempts.append(list(args))
            return create(args, len(attempts))

        output = self.work / name if output is None else output
        with patch.object(packaging.subprocess, "run", side_effect=command), \
                patch.object(packaging.time, "sleep", side_effect=pauses.append), redirect_stderr(said):
            try:
                packaging.build(self.declaration, self.binary, "1.2.3", self.target(MACOS), output)
            except packaging.Refused as error:
                refused = error
        return SimpleNamespace(output=output, attempts=attempts, pkgbuilds=pkgbuilds, pauses=pauses,
                               detached=detached, said=said.getvalue(), refused=refused)

    @staticmethod
    def created(args):
        Path(args[-1]).write_bytes(b"a disk image")
        return subprocess.CompletedProcess(args, 0, f"created: {args[-1]}\n", "")

    @staticmethod
    def busy(args):
        return subprocess.CompletedProcess(args, 1, "", "hdiutil: create failed - Resource busy\n")

    @staticmethod
    def installer_built(args):
        Path(args[-1]).write_bytes(b"an installer package")
        return subprocess.CompletedProcess(args, 0, "", "")

    @staticmethod
    def left_attached(args):
        """What `hdiutil info` lists for the image a create wrote and left
        attached: its absolute path, spelled as the create was given it, and,
        unmounted, the backing disk and the APFS disk synthesized from it."""
        return {"image-path": os.path.abspath(args[-1]), "system-entities": [
            {"dev-entry": device} for device in ("/dev/disk8", "/dev/disk8s1", "/dev/disk9", "/dev/disk9s1")]}

    # Images the build did not attach: someone else's, mounted where they opened
    # it, and one of the same name attached from another folder.
    OTHERS = (
        {"image-path": "/Users/someone/Downloads/Other.dmg", "system-entities": [
            {"dev-entry": "/dev/disk4", "mount-point": "/Volumes/Other"}]},
        {"image-path": "/Volumes/Share/readmit-desktop_1.2.3_arm64.dmg", "system-entities": [
            {"dev-entry": "/dev/disk5"}, {"dev-entry": "/dev/disk5s1"}]},
    )

    def others(self):
        return copy.deepcopy(list(self.OTHERS))

    def test_a_disk_image_whose_staging_volume_was_busy_is_created_again(self):
        """`create -srcfolder` mounts the volume it fills where the system can
        open it, and a file still open there when hdiutil unmounts it fails the
        whole create with EBUSY. That is the one failure tried again."""
        run = self.build_macos_with(lambda args, attempt: self.busy(args) if attempt == 1 else self.created(args))
        self.assertIsNone(run.refused)
        self.assertEqual(len(run.attempts), 2)
        self.assertEqual(run.attempts[0], run.attempts[1], "a retry creates the same image the same way")
        self.assertEqual(run.pauses, [packaging.CREATE_RETRY_SECONDS])
        for expected in ("readmit-desktop_1.2.3_arm64.dmg was not created", "hdiutil exited 1",
                         f"attempt 1 of {packaging.CREATE_ATTEMPTS}", "hdiutil: create failed - Resource busy"):
            self.assertIn(expected, run.said)
        manifest = json.loads((run.output / packaging.MANIFEST_NAME).read_bytes())
        self.assertEqual(sorted(package["name"] for package in manifest["packages"]),
                         ["readmit-desktop_1.2.3_arm64.dmg", "readmit-desktop_1.2.3_arm64.pkg"])

    def test_a_disk_image_that_stays_busy_is_refused_after_a_bounded_number_of_attempts(self):
        run = self.build_macos_with(lambda args, attempt: self.busy(args))
        self.assertEqual(len(run.attempts), packaging.CREATE_ATTEMPTS)
        self.assertEqual(run.pauses, [packaging.CREATE_RETRY_SECONDS] * (packaging.CREATE_ATTEMPTS - 1))
        message = str(run.refused)
        for expected in ("readmit-desktop_1.2.3_arm64.dmg", f"{packaging.CREATE_ATTEMPTS} attempts",
                         "hdiutil exited 1", "hdiutil: create failed - Resource busy"):
            self.assertIn(expected, message)

    def test_any_other_create_failure_is_refused_at_once_in_hdiutil_s_own_words(self):
        """Only the documented unmount failure is transient. A full disk, an
        image another attach is holding, a refused permission or a hang is the
        result, the first time, with what hdiutil said and without the
        machine's staging path."""
        for index, reason in enumerate(("No space left on device", "Resource temporarily unavailable",
                                        "Operation not permitted", "timeout")):
            with self.subTest(reason=reason):
                def fail(args, attempt):
                    staged = args[args.index("-srcfolder") + 1]
                    stderr = (f"hdiutil: could not access {staged}/readmit-desktop.app"
                              f" ({Path(staged).resolve()})\nhdiutil: create failed - {reason}\n")
                    if reason == "timeout":
                        # run() hands back what was captured so far as bytes.
                        raise subprocess.TimeoutExpired(args, 600, b"Initializing\xe2\x80\xa6\n", stderr.encode())
                    return subprocess.CompletedProcess(args, 1, "Initializing…\n", stderr)

                run = self.build_macos_with(fail, name=f"refused-{index}")
                self.assertEqual(len(run.attempts), 1, "no retry")
                self.assertEqual(run.pauses, [])
                self.assertIsNotNone(run.refused)
                message = str(run.refused)
                staged = run.attempts[0][run.attempts[0].index("-srcfolder") + 1]
                outcome = "did not finish in 600 seconds" if reason == "timeout" else "hdiutil exited 1"
                for expected in ("readmit-desktop_1.2.3_arm64.dmg was not created", outcome,
                                 f"hdiutil: create failed - {reason}", "Initializing…",
                                 "could not access <payload>/readmit-desktop.app (<payload>)"):
                    self.assertIn(expected, message)
                for private in (staged, str(Path(staged).resolve()), "readmit-desktop-package-"):
                    self.assertNotIn(private, message)
                if reason == "timeout":
                    # The timeout's own text is the command line, staging path included.
                    self.assertTrue(run.refused.__suppress_context__)

    def test_an_image_its_create_left_attached_is_detached_and_no_other_image_is(self):
        """hdiutil can return with the image it wrote still attached. Only the
        devices of the path that create wrote are released: not an image that
        was attached before, not one of the same name in another folder, and
        not one someone else attached while the create ran. The output folder
        is also named relative to the working directory, as CI names it."""
        self.addCleanup(os.chdir, os.getcwd())
        os.chdir(self.work)
        for output in (self.work / "left-attached", Path("left-attached-relative")):
            with self.subTest(output=str(output)):
                images, latecomer = self.others(), {"image-path": "/Users/someone/Later.dmg",
                                                    "system-entities": [{"dev-entry": "/dev/disk10"}]}

                def create(args, attempt):
                    images.extend([self.left_attached(args), copy.deepcopy(latecomer)])
                    return self.created(args)

                run = self.build_macos_with(create, images=images, output=output)
                self.assertIsNone(run.refused)
                self.assertEqual(run.detached, [("/dev/disk8", 1)], "the backing disk's detach releases its APFS disk")
                self.assertEqual(images, self.others() + [latecomer], "every other image is attached as it was")
                self.assertEqual(run.said, "")
                # The package commands are the ones they were, so the packages,
                # their names and the manifest recording them are too.
                staged = run.attempts[0][run.attempts[0].index("-srcfolder") + 1]
                self.assertEqual(run.attempts, [[
                    "hdiutil", "create", "-volname", self.declaration["display_name"], "-srcfolder", staged,
                    "-ov", "-format", "UDZO", str(output / "readmit-desktop_1.2.3_arm64.dmg")]])
                self.assertEqual(run.pkgbuilds, [[
                    "pkgbuild", "--root", staged, "--identifier", self.declaration["bundle_identifier"],
                    "--version", "1.2.3", "--install-location", "/Applications",
                    str(output / "readmit-desktop_1.2.3_arm64.pkg")]])
                manifest = json.loads((output / packaging.MANIFEST_NAME).read_bytes())
                self.assertEqual([(package["name"], package["format"]) for package in manifest["packages"]], [
                    ("readmit-desktop_1.2.3_arm64.dmg", "dmg"), ("readmit-desktop_1.2.3_arm64.pkg", "pkg")])

    def test_an_earlier_build_s_attachment_of_the_same_path_is_not_this_create_s(self):
        """A build before this one, into a folder since deleted and made again,
        can have left the same path attached. That attachment is not this
        create's: it stays as it is, unreported, and only what this create
        left is released."""
        for leaves in (False, True):
            with self.subTest(leaves=leaves):
                name = f"again-{leaves}"
                earlier = {"image-path": str(self.work / name / "readmit-desktop_1.2.3_arm64.dmg"),
                           "system-entities": [{"dev-entry": "/dev/disk4"}, {"dev-entry": "/dev/disk4s1"}]}
                images = [copy.deepcopy(earlier)]

                def create(args, attempt):
                    if leaves:
                        images.append(self.left_attached(args))
                    return self.created(args)

                run = self.build_macos_with(create, name=name, images=images)
                self.assertIsNone(run.refused)
                self.assertEqual(run.said, "")
                self.assertEqual(images, [earlier])
                self.assertEqual(run.detached, [("/dev/disk8", 1)] if leaves else [])

    def test_an_image_a_failed_create_left_attached_is_detached_before_it_is_tried_again_or_refused(self):
        def timeout(args, attempt):
            raise subprocess.TimeoutExpired(args, 600, b"", b"")

        for index, (reason, outcome, refusal) in enumerate((
            ("busy", lambda args, attempt: self.busy(args) if attempt == 1 else self.created(args), None),
            ("full", lambda args, attempt: subprocess.CompletedProcess(
                args, 1, "", "hdiutil: create failed - No space left on device\n"), "No space left on device"),
            ("timeout", timeout, "did not finish in 600 seconds"),
        )):
            with self.subTest(reason=reason):
                images = self.others()

                def create(args, attempt):
                    if attempt == 1:
                        images.append(self.left_attached(args))
                    return outcome(args, attempt)

                run = self.build_macos_with(create, name=f"failed-left-attached-{index}", images=images)
                self.assertEqual(run.detached, [("/dev/disk8", 1)], "released before anything else is tried")
                self.assertEqual(images, self.others())
                if refusal is None:
                    self.assertIsNone(run.refused)
                    self.assertEqual(len(run.attempts), 2)
                else:
                    self.assertEqual(len(run.attempts), 1)
                    self.assertIn(refusal, str(run.refused))
                self.assertNotIn("cleanup", run.said)

    def test_a_build_that_cannot_read_what_is_attached_creates_nothing(self):
        """Without what was attached before a create, nothing it leaves could be
        told apart from anyone else's, so the build is refused before creating
        anything, as verification is refused before an attach."""
        run = self.build_macos_with(lambda args, attempt: self.created(args), name="uninspected",
                                    images=["not an image"])
        self.assertEqual(run.attempts, [])
        self.assertEqual(run.detached, [])
        self.assertIn("cannot inspect disk images", str(run.refused))
        self.assertFalse((run.output / packaging.MANIFEST_NAME).exists())

    def test_what_a_create_left_that_cannot_be_proven_its_own_or_detached_is_reported(self):
        """Cleanup detaches nothing it cannot attribute, and a cleanup that
        could not finish is reported, naming the image, as after a failed
        attach: it never changes what the create did. A created image is still
        packaged and a failed create is still refused in hdiutil's words."""
        for mode, expected in (("reused-device", "create cleanup refused: ambiguous device or mount association"),
                               ("shared-disk", "create cleanup refused: device association changed"),
                               ("unreadable-inventory", "cannot inspect disk images"),
                               ("detach-error", "create device did not detach")):
            for created in (True, False):
                with self.subTest(mode=mode, created=created):
                    # A device this machine had before the create, since given to another image.
                    reused = {"image-path": "/Users/someone/Other.dmg", "system-entities": [{"dev-entry": "/dev/disk8"}]}
                    shared = {"image-path": "/Users/someone/Other.dmg", "system-entities": [{"dev-entry": "/dev/disk8s3"}]}
                    images = [reused] if mode == "reused-device" else []

                    def create(args, attempt):
                        images[:] = [self.left_attached(args)]
                        if mode == "shared-disk":
                            images.append(shared)
                        if mode == "unreadable-inventory":
                            images.append("not an image")
                        if created:
                            return self.created(args)
                        return subprocess.CompletedProcess(args, 1, "", "hdiutil: create failed - No space left on device\n")

                    run = self.build_macos_with(create, name=f"unproven-{mode}-{created}", images=images,
                                                detach_status=int(mode == "detach-error"))
                    self.assertEqual(run.detached, [("/dev/disk8", 1)] if mode == "detach-error" else [])
                    if mode == "shared-disk":
                        self.assertIn(shared, images, "another image's device is never detached")
                    self.assertIn("readmit-desktop_1.2.3_arm64.dmg: " + expected, run.said)
                    if mode == "detach-error":
                        self.assertIn("hdiutil: detach failed - Resource busy", run.said)
                    if created:
                        self.assertIsNone(run.refused)
                        self.assertTrue((run.output / packaging.MANIFEST_NAME).is_file())
                    else:
                        self.assertIn("No space left on device", str(run.refused))
                        self.assertNotIn(expected, str(run.refused))

    def test_a_pkgbuild_failure_is_refused_in_its_own_words_without_the_staging_path(self):
        """A failed pkgbuild reached the log as a CalledProcessError: a status
        and a command line naming the staging folder, and no reason."""
        for index, reason in enumerate(("exit", "timeout")):
            with self.subTest(reason=reason):
                def pkgbuild(args):
                    staged = args[args.index("--root") + 1]
                    stdout = f"pkgbuild: Inferring bundle components from contents of {staged}\n"
                    stderr = f'pkgbuild: error: Specified root path "{Path(staged).resolve()}" does not exist.\n'
                    if reason == "timeout":
                        # run() hands back what was captured so far as bytes.
                        raise subprocess.TimeoutExpired(args, 600, stdout.encode(), stderr.encode())
                    return subprocess.CompletedProcess(args, 1, stdout, stderr)

                run = self.build_macos_with(lambda args, attempt: self.created(args), name=f"pkgbuild-{index}",
                                            pkgbuild=pkgbuild)
                self.assertEqual(len(run.pkgbuilds), 1, "no retry")
                message = str(run.refused)
                staged = run.pkgbuilds[0][run.pkgbuilds[0].index("--root") + 1]
                outcome = "did not finish in 600 seconds" if reason == "timeout" else "pkgbuild exited 1"
                for expected in ("readmit-desktop_1.2.3_arm64.pkg was not built", outcome,
                                 "Inferring bundle components from contents of <payload>",
                                 'Specified root path "<payload>" does not exist'):
                    self.assertIn(expected, message)
                for private in (staged, str(Path(staged).resolve()), "readmit-desktop-package-"):
                    self.assertNotIn(private, message)
                self.assertFalse((run.output / packaging.MANIFEST_NAME).exists())
                if reason == "timeout":
                    # The timeout's own text is the command line, staging path included.
                    self.assertTrue(run.refused.__suppress_context__)

    @unittest.skipIf(platform.system() != "Darwin", "pkgbuild is a macOS tool")
    def test_a_real_pkgbuild_failure_is_refused_in_its_own_words_without_the_staging_path(self):
        staged = self.work / "payload-never-staged"
        package = self.work / "readmit-desktop_1.2.3_arm64.pkg"
        with self.assertRaises(packaging.Refused) as refused:
            packaging.build_installer_package(self.declaration, staged, package, "1.2.3")
        message = str(refused.exception)
        for expected in (f"{package.name} was not built", "pkgbuild exited 1",
                         'Specified root path "<payload>" does not exist'):
            self.assertIn(expected, message)
        for private in (str(staged), str(staged.resolve()), staged.name):
            self.assertNotIn(private, message)
        self.assertFalse(package.exists())

    def release_images_under_work(self):
        """Detach whatever is still attached of an image in this test's own
        folder, so a failing test leaves nothing attached. Nothing else on the
        machine is this test's, so nothing else is touched."""
        work = self.work.resolve()
        for entry in packaging.attached_images():
            if Path(entry["image-path"]).resolve().is_relative_to(work):
                for entity in entry["system-entities"]:
                    if re.fullmatch(r"/dev/disk[0-9]+", entity.get("dev-entry", "")):
                        subprocess.run(["hdiutil", "detach", "-force", entity["dev-entry"]],
                                       capture_output=True, timeout=600)

    @staticmethod
    def attached_devices(image):
        return sorted(entity.get("dev-entry", "") for entry in packaging.attached_images()
                      if Path(entry["image-path"]).resolve() == image.resolve()
                      for entity in entry["system-entities"])

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_a_real_image_its_create_left_attached_is_detached_and_a_bystander_is_not(self):
        """The helper that leaves a created image attached cannot be made to on
        demand, so the image each create wrote is attached again at the
        subprocess boundary: the same inventory entry, its path and unmounted
        devices. A bystander this test attached itself, which the build did not
        create, stays attached with the same devices."""
        self.addCleanup(self.release_images_under_work)
        source = self.work / "bystander-source"
        source.mkdir()
        (source / "fixture.txt").write_text("synthetic fixture")
        bystander = self.work / "bystander.dmg"
        packaging.create_disk_image(self.declaration, source, bystander)
        packaging.run_tool(["hdiutil", "attach", "-nomount", "-readonly", str(bystander)], "attach the bystander")
        attached = self.attached_devices(bystander)
        self.assertTrue(attached)
        original_run, left = packaging.subprocess.run, []

        def leave_attached(args, **kwargs):
            result = original_run(args, **kwargs)
            if args[:2] == ["hdiutil", "create"] and result.returncode == 0:
                original_run(["hdiutil", "attach", "-nomount", "-readonly", args[-1]],
                             capture_output=True, timeout=600, check=True)
                left.append(Path(args[-1]))
            return result

        output = self.work / "left-attached-packages"
        with patch.object(packaging.subprocess, "run", side_effect=leave_attached):
            packaging.build(self.declaration, self.binary, "1.2.3", self.target(MACOS), output)
        self.assertEqual(left, [output / "readmit-desktop_1.2.3_arm64.dmg"])
        self.assertEqual(self.attached_devices(left[0]), [], "the image the build created is no longer attached")
        self.assertEqual(self.attached_devices(bystander), attached, "the bystander is attached as it was")
        packaging.verify(self.declaration, output)

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_a_disk_image_created_again_holds_the_application_it_claims(self):
        """The retried image is the image: the same name, format and content.

        The first real create completes and is then reported busy at the
        subprocess boundary, so the second overwrites a real image rather
        than racing a real indexer for the staging volume."""
        original_run, attempts = packaging.subprocess.run, []

        def busy_once(args, **kwargs):
            result = original_run(args, **kwargs)
            if args[:2] == ["hdiutil", "create"]:
                attempts.append(result.returncode)
                if len(attempts) == 1 and result.returncode == 0:
                    return self.busy(args)
            return result

        output = self.work / "retried-packages"
        with patch.object(packaging.subprocess, "run", side_effect=busy_once), \
                patch.object(packaging.time, "sleep"), redirect_stderr(io.StringIO()):
            packaging.build(self.declaration, self.binary, "1.2.3", self.target(MACOS), output)
        self.assertEqual(attempts, [0, 0])
        document = packaging.verify(self.declaration, output)
        self.assertEqual(sorted(entry.name for entry in output.iterdir()), [
            packaging.MANIFEST_NAME, "readmit-desktop_1.2.3_arm64.dmg", "readmit-desktop_1.2.3_arm64.pkg"])
        image = output / "readmit-desktop_1.2.3_arm64.dmg"
        information = plistlib.loads(packaging.run_tool(
            ["hdiutil", "imageinfo", "-plist", str(image)], "read the image format").stdout.encode())
        self.assertEqual(information["Format"], "UDZO")
        self.assertEqual({package["format"] for package in document["packages"]}, {"dmg", "pkg"})

    def test_failed_attach_cleans_its_new_device_and_preserves_the_refusal(self):
        image = self.work / "failure.dmg"
        image.write_bytes(b"image bytes")
        images = [{"image-path": "/someone/else.dmg", "system-entities": [
            {"dev-entry": "/dev/disk7", "mount-point": "/Volumes/Other"}]}]
        detached, attempts = [], []

        def command(args, **kwargs):
            if args[1] == "info":
                output = plistlib.dumps({"images": images}).decode()
                return subprocess.CompletedProcess(args, 0, output, "")
            if args[1] == "attach":
                attempts.append(args)
                images.append({"image-path": str(Path(args[-1]).resolve()),
                               "system-entities": [{"dev-entry": "/dev/disk8"}]})
                return subprocess.CompletedProcess(args, 1, "partial device", "Resource temporarily unavailable")
            self.assertEqual(args[1:3], ["detach", "-force"])
            detached.append(args[-1])
            return subprocess.CompletedProcess(args, 0, "", "")

        with patch.object(packaging.subprocess, "run", side_effect=command):
            with self.assertRaises(packaging.Refused) as refused:
                with packaging.mounted(image):
                    self.fail("failed attach must never expose a volume")
        self.assertEqual(detached, ["/dev/disk8"])
        self.assertEqual(len(attempts), 1, "no attach retry")
        self.assertNotEqual(Path(attempts[0][-1]), image)
        self.assertEqual(image.read_bytes(), b"image bytes")
        self.assertIn("Resource temporarily unavailable", str(refused.exception))
        self.assertIn("partial device", str(refused.exception))

    def test_failed_attach_cleanup_refuses_uncertain_ownership_and_keeps_original_error(self):
        for mode in ("existing", "foreign-mount", "reassigned", "changed-mount", "shared-partition",
                     "invalid-inventory", "inventory-error", "detach-error", "timeout"):
            with self.subTest(mode=mode):
                image = self.work / "failure.dmg"
                image.write_bytes(b"fixture")
                owned = {"image-path": "", "system-entities": [{"dev-entry": "/dev/disk8"}]}
                foreign = {"image-path": "/other.dmg", "system-entities": [{"dev-entry": "/dev/disk8s1"}]}
                info_count, detach_count = 0, 0
                failure = subprocess.TimeoutExpired("hdiutil attach", 600, stderr="attach timed out")

                def command(args, **kwargs):
                    nonlocal info_count, detach_count
                    if args[1] == "attach":
                        owned["image-path"] = str(Path(args[-1]).resolve())
                        if mode == "foreign-mount":
                            owned["system-entities"][0]["mount-point"] = "/Volumes/Other"
                        if mode == "timeout":
                            raise failure
                        return subprocess.CompletedProcess(args, 1, "", "original attach failure")
                    if args[1] == "info":
                        info_count += 1
                        images = [] if info_count == 1 else [owned]
                        if mode == "existing" and info_count == 1:
                            images = [foreign]
                        if mode == "reassigned" and info_count >= 3:
                            images = [foreign]
                        if mode == "changed-mount" and info_count >= 3:
                            images = [{"image-path": owned["image-path"], "system-entities": [
                                {"dev-entry": "/dev/disk8", "mount-point": "/Volumes/Changed"}]}]
                        if mode == "shared-partition" and info_count >= 2:
                            images = [owned, foreign]
                        if mode == "inventory-error" and info_count >= 2:
                            return subprocess.CompletedProcess(args, 1, "", "inventory unavailable")
                        output = "invalid plist" if mode == "invalid-inventory" and info_count >= 2 else plistlib.dumps({"images": images}).decode()
                        return subprocess.CompletedProcess(args, 0, output, "")
                    self.assertEqual(args[1], "detach")
                    detach_count += 1
                    return subprocess.CompletedProcess(args, int(mode == "detach-error"), "", "cleanup failure")

                reported = io.StringIO()
                with patch.object(packaging.subprocess, "run", side_effect=command), redirect_stderr(reported):
                    with self.assertRaises((packaging.Refused, subprocess.TimeoutExpired)) as refused:
                        with packaging.mounted(image):
                            self.fail("failed attach exposed volume")
                if mode == "timeout":
                    self.assertIs(refused.exception, failure)
                else:
                    self.assertIn("original attach failure", str(refused.exception))
                    self.assertTrue(reported.getvalue(), "cleanup uncertainty must be visible")
                self.assertEqual(detach_count, int(mode in ("detach-error", "timeout")))

    def test_failed_attach_rechecks_apfs_group_after_detaching_backing_disk(self):
        for reassigned in (False, True):
            with self.subTest(reassigned=reassigned):
                image = self.work / "apfs.dmg"
                image.write_bytes(b"fixture")
                images, detached = [], []

                def command(args, **kwargs):
                    if args[1] == "info":
                        return subprocess.CompletedProcess(args, 0, plistlib.dumps({"images": images}).decode(), "")
                    if args[1] == "attach":
                        images.append({"image-path": str(Path(args[-1]).resolve()), "system-entities": [
                            {"dev-entry": "/dev/disk8"}, {"dev-entry": "/dev/disk8s1"},
                            {"dev-entry": "/dev/disk9"}, {"dev-entry": "/dev/disk9s1"}]})
                        return subprocess.CompletedProcess(args, 1, "", "original failure")
                    self.assertEqual(args[1], "detach")
                    detached.append(args[-1])
                    images.clear()
                    if reassigned:
                        images.append({"image-path": "/someone/else.dmg", "system-entities": [
                            {"dev-entry": "/dev/disk9"}]})
                    return subprocess.CompletedProcess(args, 0, "", "")

                reported = io.StringIO()
                with patch.object(packaging.subprocess, "run", side_effect=command), redirect_stderr(reported):
                    with self.assertRaisesRegex(packaging.Refused, "original failure"):
                        with packaging.mounted(image):
                            self.fail("failed attach exposed volume")
                self.assertEqual(detached, ["/dev/disk8"])
                self.assertEqual("association changed" in reported.getvalue(), reassigned)

    @unittest.skipIf(platform.system() != "Darwin", "hdiutil is a macOS tool")
    def test_failed_attach_releases_a_real_temporary_disk_image(self):
        source = self.work / "image-source"
        source.mkdir()
        (source / "fixture.txt").write_text("synthetic fixture")
        image = self.work / "fixture.dmg"
        # A bare `hdiutil create` can leave the fixture attached (#353); the
        # tool's own create releases what it leaves.
        packaging.create_disk_image(self.declaration, source, image)
        original_run = packaging.subprocess.run
        private_image = None
        detached = []

        def fail_after_attach(args, **kwargs):
            nonlocal private_image
            result = original_run(args, **kwargs)
            if args[:2] == ["hdiutil", "attach"] and result.returncode == 0:
                private_image = Path(args[-1])
                # The real tool created a real device; inject its failure at
                # the subprocess boundary rather than racing Disk Arbitration.
                return subprocess.CompletedProcess(args, 1, result.stdout, "injected attach failure")
            if args[:2] == ["hdiutil", "detach"]:
                detached.append(args[-1])
            return result

        with patch.object(packaging.subprocess, "run", side_effect=fail_after_attach):
            with self.assertRaisesRegex(packaging.Refused, "injected attach failure"):
                with packaging.mounted(image):
                    self.fail("failed attach exposed volume")
        self.assertIsNotNone(private_image)
        self.assertEqual(len(detached), 1)
        inventory = plistlib.loads(original_run(["hdiutil", "info", "-plist"], capture_output=True).stdout)
        self.assertFalse(any(Path(entry["image-path"]).resolve() == private_image
                             for entry in inventory["images"]))
        self.assertTrue(image.is_file())

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
