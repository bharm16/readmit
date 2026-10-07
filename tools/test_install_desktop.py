"""Local refresh preserves existing apps and refuses unrelated destinations."""

from pathlib import Path
import hashlib
import json
import plistlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import install_desktop as installer
import package_desktop as packaging


class InstallTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.applications = self.root / "Applications"
        self.applications.mkdir()
        self.destination = self.applications / "Readmit.app"
        self.shortcut = self.root / "Desktop" / "Readmit.app"
        self.shortcut.parent.mkdir()
        self.bundle = self.root / "candidate.app"
        self.make_bundle(self.bundle, b"new")
        running = patch.object(installer.subprocess, "run", return_value=subprocess.CompletedProcess([], 1))
        self.process = running.start()
        self.addCleanup(running.stop)

    def make_bundle(self, path, content, owned=True):
        (path / "Contents/MacOS").mkdir(parents=True)
        (path / "Contents/MacOS/readmit-desktop").write_bytes(content)
        (path / "Contents/Info.plist").write_bytes(plistlib.dumps({
            "CFBundleIdentifier": "com.readmit.desktop", installer.LOCAL_MARKER: owned,
        }))

    def installed_bytes(self):
        return (self.destination / "Contents/MacOS/readmit-desktop").read_bytes()

    def test_first_install_and_refresh_keep_the_same_shortcut_and_other_data(self):
        state = self.root / "saved-session.json"
        state.write_bytes(b"retained data")
        installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"new")
        self.assertEqual(self.shortcut.resolve(), self.destination.resolve())
        (self.bundle / "Contents/MacOS/readmit-desktop").write_bytes(b"refreshed")
        installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"refreshed")
        self.assertEqual(self.shortcut.resolve(), self.destination.resolve())
        self.assertEqual(state.read_bytes(), b"retained data")
        self.assertEqual(list(self.applications.iterdir()), [self.destination])

    def test_unowned_app_and_symbolic_link_destination_are_refused(self):
        self.make_bundle(self.destination, b"unrelated", owned=False)
        with self.assertRaises(packaging.Refused):
            installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"unrelated")
        other = self.applications / "other.app"
        self.destination.rename(other)
        self.destination.symlink_to(other)
        with self.assertRaises(packaging.Refused):
            installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"unrelated")

    def test_desktop_collision_is_refused_before_the_app_changes(self):
        self.make_bundle(self.destination, b"old")
        self.shortcut.write_bytes(b"personal file")
        with self.assertRaises(packaging.Refused):
            installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"old")
        self.assertEqual(self.shortcut.read_bytes(), b"personal file")

    def test_running_app_or_failed_process_inventory_is_refused(self):
        self.make_bundle(self.destination, b"old")
        for status in (0, 2):
            with self.subTest(status=status):
                self.process.return_value.returncode = status
                with self.assertRaises(packaging.Refused):
                    installer.install_bundle(self.bundle, self.destination, self.shortcut)
                self.assertEqual(self.installed_bytes(), b"old")

    def test_failed_replacement_restores_previous_app(self):
        self.make_bundle(self.destination, b"old")
        rename = Path.rename

        def failing_install(path, target):
            if path.name == installer.APP_NAME and path.parent.name.startswith(".readmit-install-"):
                raise OSError("installation refused")
            return rename(path, target)

        with patch.object(Path, "rename", failing_install):
            with self.assertRaisesRegex(OSError, "installation refused"):
                installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"old")
        self.assertFalse(self.shortcut.exists())

    def test_failed_shortcut_restores_previous_app(self):
        self.make_bundle(self.destination, b"old")
        with patch.object(Path, "symlink_to", side_effect=OSError("Desktop not writable")):
            with self.assertRaisesRegex(OSError, "Desktop not writable"):
                installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"old")

    def test_running_app_is_rechecked_after_staging(self):
        self.make_bundle(self.destination, b"old")
        self.process.side_effect = [subprocess.CompletedProcess([], 1), subprocess.CompletedProcess([], 0)]
        with self.assertRaisesRegex(packaging.Refused, "Quit Readmit"):
            installer.install_bundle(self.bundle, self.destination, self.shortcut)
        self.assertEqual(self.installed_bytes(), b"old")

    def test_failed_recovery_retains_previous_app_for_manual_restore(self):
        self.make_bundle(self.destination, b"old")
        rename = Path.rename

        def failing_install_and_restore(path, target):
            if path.parent.name.startswith(".readmit-install-"):
                raise OSError("volume refuses rename")
            return rename(path, target)

        with patch.object(Path, "rename", failing_install_and_restore):
            with self.assertRaisesRegex(packaging.Refused, "retained files"):
                installer.install_bundle(self.bundle, self.destination, self.shortcut)
        retained = list(self.applications.glob(".readmit-install-*/previous.app/Contents/MacOS/readmit-desktop"))
        self.assertEqual(len(retained), 1)
        self.assertEqual(retained[0].read_bytes(), b"old")

    def test_interrupt_after_either_rename_restores_previous_app(self):
        rename = Path.rename
        for interrupted in ("previous.app", "Readmit.app"):
            with self.subTest(interrupted=interrupted):
                if not self.destination.exists():
                    self.make_bundle(self.destination, b"old")
                fired = False

                def interrupt_after_rename(path, target):
                    nonlocal fired
                    result = rename(path, target)
                    if not fired and target.name == interrupted:
                        fired = True
                        raise KeyboardInterrupt()
                    return result

                with patch.object(Path, "rename", interrupt_after_rename):
                    with self.assertRaises(KeyboardInterrupt):
                        installer.install_bundle(self.bundle, self.destination, self.shortcut)
                self.assertEqual(self.installed_bytes(), b"old")
                self.assertFalse(self.shortcut.exists())

    def test_second_interrupt_during_recovery_retains_old_app(self):
        self.make_bundle(self.destination, b"old")
        rename = Path.rename

        def interrupt_install_and_restore(path, target):
            if path.parent.name.startswith(".readmit-install-"):
                raise KeyboardInterrupt()
            return rename(path, target)

        with patch.object(Path, "rename", interrupt_install_and_restore):
            with self.assertRaises(KeyboardInterrupt):
                installer.install_bundle(self.bundle, self.destination, self.shortcut)
        retained = list(self.applications.glob(".readmit-install-*/previous.app/Contents/MacOS/readmit-desktop"))
        self.assertEqual(len(retained), 1)
        self.assertEqual(retained[0].read_bytes(), b"old")


class ReferenceBuildInputTests(unittest.TestCase):
    def test_local_refresh_uses_pinned_retained_content_without_a_customer_step(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(installer.os.environ, {}, clear=True):
            root = Path(directory)
            registry = root / "Library/Application Support/readmit/reference-library.json"
            registry.parent.mkdir(parents=True)
            entries = []
            for index in range(14):
                path = root / f"source-{index}.json"
                raw = f"owned-{index}".encode()
                path.write_bytes(raw)
                entries.append({"edition": str(index), "path": str(path), "identity": "sha256:" + hashlib.sha256(raw).hexdigest()})
            registry.write_text(json.dumps({"schema": "readmit-hl7-reference-library/v1", "editions": entries}))
            with patch.object(installer.Path, "home", return_value=root):
                with installer.retained_reference_library() as folder:
                    manifest = json.loads((folder / "library.json").read_bytes())
                    self.assertEqual(len(manifest["catalogs"]), 14)
                    self.assertEqual((folder / "catalog-7.json").read_bytes(), b"owned-7")
                self.assertFalse(folder.exists())
                Path(entries[0]["path"]).write_bytes(b"changed")
                with self.assertRaises(packaging.Refused):
                    with installer.retained_reference_library():
                        self.fail("changed source accepted")

    def test_staging_cleanup_preserves_preexisting_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "desktop/reference-assets/library.zip"
            archive.parent.mkdir(parents=True)
            archive.write_bytes(b"previous staging input")
            with patch.object(installer, "ROOT", root), self.assertRaises(packaging.Refused):
                with installer.embedded_reference_library(root):
                    self.fail("existing staging archive overwritten")
            self.assertEqual(archive.read_bytes(), b"previous staging input")

    def test_competing_staging_archive_is_never_removed_and_helper_uses_host_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "desktop/reference-assets/library.zip"
            archive.parent.mkdir(parents=True)
            environment = {"CGO_ENABLED": "1"}

            def competing_writer(command, **kwargs):
                self.assertIs(kwargs["env"], environment)
                Path(command[-1]).write_bytes(b"this build")
                archive.write_bytes(b"other build")

            with patch.object(installer, "ROOT", root), patch.object(installer, "run", side_effect=competing_writer), self.assertRaises(packaging.Refused):
                with installer.embedded_reference_library(root, environment):
                    self.fail("competing archive overwritten")
            self.assertEqual(archive.read_bytes(), b"other build")

    def test_only_this_invocations_archive_is_cleaned_after_compilation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / "desktop/reference-assets/library.zip"
            archive.parent.mkdir(parents=True)

            def writer(command, **kwargs):
                self.assertNotEqual(Path(command[-1]).parent.parent, archive.parent)
                Path(command[-1]).write_bytes(b"owned archive")

            with patch.object(installer, "ROOT", root), patch.object(installer, "run", side_effect=writer):
                with installer.embedded_reference_library(root, {}):
                    self.assertEqual(archive.read_bytes(), b"owned archive")
                self.assertFalse(archive.exists())
                with installer.embedded_reference_library(root, {}):
                    archive.unlink()
                    archive.write_bytes(b"replacement")
                self.assertEqual(archive.read_bytes(), b"replacement")


if __name__ == "__main__":
    unittest.main()
