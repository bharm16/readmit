"""Local refresh preserves existing apps and refuses unrelated destinations."""

from pathlib import Path
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


if __name__ == "__main__":
    unittest.main()
