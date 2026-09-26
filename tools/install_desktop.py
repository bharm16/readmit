"""Build and install a local macOS Readmit.app; launching it never builds code."""

import argparse
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import tempfile

import package_desktop as packaging

ROOT = Path(__file__).resolve().parent.parent
APP_NAME = "Readmit.app"
LOCAL_MARKER = "ReadmitLocalInstall"


def run(command, **kwargs):
    subprocess.run(command, check=True, **kwargs)


def check_destination(destination, shortcut):
    """Replace only this tool's app, never another installation or shortcut."""
    if destination.is_symlink():
        raise packaging.Refused(f"refusing a symbolic-link application: {destination}")
    if destination.exists():
        try:
            information = plistlib.loads((destination / "Contents/Info.plist").read_bytes())
        except (OSError, ValueError) as error:
            raise packaging.Refused(f"not a local Readmit installation: {destination}") from error
        if (not isinstance(information, dict) or
                information.get("CFBundleIdentifier") != "com.readmit.desktop" or
                information.get(LOCAL_MARKER) is not True):
            raise packaging.Refused(f"not installed by this command: {destination}")
    if shortcut is not None and os.path.lexists(shortcut):
        if not shortcut.is_symlink() or Path(os.readlink(shortcut)) != destination:
            raise packaging.Refused(f"refusing to replace an unrelated Desktop item: {shortcut}")
    executable = destination / "Contents/MacOS/readmit-desktop"
    running = subprocess.run(["pgrep", "-f", "^" + re.escape(str(executable)) + "($| )"],
                             capture_output=True, text=True)
    if running.returncode == 0:
        raise packaging.Refused("Quit Readmit before refreshing the installed app.")
    if running.returncode != 1:
        raise packaging.Refused("could not check whether Readmit is running")


def install_bundle(bundle, destination, shortcut):
    """Stage beside the destination; restore the old app if replacement fails."""
    check_destination(destination, shortcut)
    directory = Path(tempfile.mkdtemp(prefix=".readmit-install-", dir=destination.parent))
    keep_backup = False
    try:
        staged = directory / APP_NAME
        previous = directory / "previous.app"
        shutil.copytree(bundle, staged, symlinks=True)
        # Recheck after the copy, immediately before touching an existing app.
        check_destination(destination, shortcut)
        had_previous = destination.exists()
        had_shortcut = shortcut is not None and os.path.lexists(shortcut)
        # Keep recovery files unless installation or restoration completed.
        # Even a second interrupt during rollback must not delete the old app.
        keep_backup = True
        try:
            if had_previous:
                destination.rename(previous)
            staged.rename(destination)
            if shortcut is not None and not os.path.lexists(shortcut):
                shortcut.symlink_to(destination, target_is_directory=True)
        except BaseException:
            try:
                # Inspect paths: an interrupt can arrive after rename succeeds
                # but before a Python flag recording its success is assigned.
                if not staged.exists():
                    destination.rename(staged)
                if previous.exists():
                    previous.rename(destination)
                if (shortcut is not None and not had_shortcut and shortcut.is_symlink()
                        and Path(os.readlink(shortcut)) == destination):
                    shortcut.unlink()
            except OSError as error:
                raise packaging.Refused(f"replacement recovery failed; retained files in {directory}") from error
            keep_backup = False
            raise
        keep_backup = False
    finally:
        if not keep_backup:
            shutil.rmtree(directory)


def build_bundle(directory):
    packaging.require_macos("local application installation")
    run(["python3", str(ROOT / "tools/toolchain.py"), "--check"], cwd=ROOT)
    revision = subprocess.check_output(["git", "rev-parse", "--short=12", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip()
    version = "0.0.0+local." + revision + (".dirty" if dirty else "")
    frontend = ROOT / "desktop/frontend"
    run(["npm", "ci"], cwd=frontend)
    run(["npm", "run", "build"], cwd=frontend)
    binary = directory / "readmit-desktop"
    environment = os.environ.copy()
    environment["CGO_ENABLED"] = "1"
    # A local install targets the host, even if the caller was cross-compiling.
    for name in ("GOOS", "GOARCH", "GOFLAGS"):
        environment.pop(name, None)
    run(["go", "build", "-tags", "production", "-trimpath", "-ldflags",
         "-X github.com/bharm16/readmit/internal/engine.version=" + version,
         "-o", str(binary), "."], cwd=ROOT / "desktop", env=environment)
    declaration = packaging.read_declaration()
    bundle = packaging.application_bundle(declaration, binary, version, directory)
    information_path = bundle / "Contents/Info.plist"
    information = plistlib.loads(information_path.read_bytes())
    information[LOCAL_MARKER] = True
    information["CFBundleName"] = "Readmit"
    information["CFBundleDisplayName"] = "Readmit"
    information_path.write_bytes(plistlib.dumps(information))
    # Local ad-hoc sealing needs no developer account or signing credentials.
    run(["codesign", "--force", "--sign", "-", str(bundle)])
    run(["codesign", "--verify", "--strict", str(bundle)])
    packaging.verify_bundle(declaration, version, bundle)
    if packaging.reported_version(binary, isolated=True) != version:
        raise packaging.Refused("the built executable does not report its source revision")
    packaging.startup(bundle / "Contents/MacOS/readmit-desktop")
    return bundle, version


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--applications-dir", type=Path, default=Path("/Applications"),
                        help="installation folder (default: /Applications; no sudo is used)")
    parser.add_argument("--no-desktop-shortcut", action="store_true")
    args = parser.parse_args()
    packaging.require_macos("local application installation")
    import fcntl  # macOS only; other hosts refuse above before importing it.

    applications = args.applications_dir.expanduser().resolve()
    applications.mkdir(parents=True, exist_ok=True)
    destination = applications / APP_NAME
    shortcut = None if args.no_desktop_shortcut else Path.home() / "Desktop" / APP_NAME
    if shortcut is not None and not shortcut.parent.is_dir():
        raise packaging.Refused("Desktop folder is missing; use --no-desktop-shortcut")
    # Concurrent refreshes cannot move or remove each other's installed bundle.
    with (applications / ".readmit-install.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise packaging.Refused("another Readmit installation is in progress") from error
        check_destination(destination, shortcut)
        with tempfile.TemporaryDirectory(prefix="readmit-local-build-") as directory:
            bundle, version = build_bundle(Path(directory))
            install_bundle(bundle, destination, shortcut)
    print(f"Installed {destination} ({version})")
    if shortcut is not None:
        print(f"Desktop shortcut: {shortcut}")
    print("Open Readmit from Finder or the Desktop. Run this command again to refresh it.")


if __name__ == "__main__":
    try:
        main()
    except (packaging.Refused, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error)) from error
