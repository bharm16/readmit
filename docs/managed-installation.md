# Managed and offline installation

This runbook covers administrator-staged installation of the existing native
packages, offline entitlement import and the customer-controlled runner.
Packages currently built by this repository are **unsigned development previews**.
These procedures do not approve a production rollout. D5's Windows 11 24H2/25H2,
macOS 15/26 and Ubuntu 24.04 matrix still needs the exact signed candidate and
managed-machine acceptance recorded in [release acceptance](release-acceptance.md).
CI's Windows Server runner is not a Windows 11 policy lab.

## Stage once, transfer deliberately

On an approved connected staging machine, collect the exact desktop package for
the destination OS/architecture, its manifest, build provenance and independently
trusted release checksums, plus the matching standalone CLI archive. Retain the
previous approved installers and CLI. Compare digests after transfer to the
isolated machine; a manifest copied beside an attacker-modified package is not
an authenticity authority. Verify the publisher signature and provenance under
your organization's policy before installation. Current preview manifests say
`signed_for_distribution: false`; changing that field cannot create a signature.

Keep the package directory (manifest and only its named packages) separate from
prerequisites, licenses and evidence. `readmit upgrade check` rejects extra files
in its candidate directory. The static CLI needs no webview, Python, Go, Node or
container runtime. Desktop packages need these separately staged prerequisites:

| Destination | Offline prerequisite |
| --- | --- |
| Windows x64 | Microsoft's **Evergreen Standalone Installer X64**, not the downloading bootstrapper. Stage its trusted publisher verification and version. The MSI requires the machine-wide WebView2 runtime registry entry. A fixed-version runtime directory is not supported by this installer. |
| Ubuntu 24.04 amd64/arm64 | The architecture-matching `libgtk-3-0 (>= 3.24)` and `libwebkit2gtk-4.1-0 (>= 2.44)` dependencies, including Ubuntu's applicable package aliases and entire transitive dependency closure. Use an authenticated snapshot of the approved Ubuntu repositories or a complete local package cache prepared against the destination baseline. |
| macOS Intel/Apple Silicon | The OS-provided WebKit webview. Select the matching architecture; no separate downloaded webview is bundled. |

A directory containing just two Linux `.deb` files is not a dependency closure.
Prepare and test it on a clean matching machine; an already provisioned build
runner can hide a missing dependency. Do not run `apt-get update` or dependency
repair against internet repositories on the air-gapped destination. Preserve
repository signature verification when building the offline repository. WebView2
and OS maintenance are administrator responsibilities; Readmit does not download
or update those components. Manage their network/update policies separately.

## Silent native deployment

Run these only with already approved, exact artifact paths. Replace `VERSION`
and architecture placeholders before running. Administrative elevation belongs
to package installation/removal; run the application afterward as a normal user.
Keep installer logs in an administrator-controlled directory outside evidence.

### Windows

In an elevated PowerShell session, stage and install the standalone runtime
first; wait for it, check its outcome and verify the machine-wide runtime before
installing Readmit. Microsoft's [offline runtime instructions](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)
document its `/silent /install` options. The MSI never fetches it.

```powershell
$runtime = Start-Process 'C:\ReadmitStage\MicrosoftEdgeWebView2RuntimeInstallerX64.exe' -ArgumentList '/silent /install' -Wait -PassThru
if ($runtime.ExitCode -ne 0) { throw "Runtime installation requires investigation: $($runtime.ExitCode)" }
$package = 'C:\ReadmitStage\readmit-desktop_VERSION_x64.msi'
$install = Start-Process msiexec.exe -ArgumentList "/i `"$package`" /qn /norestart /l*v C:\ReadmitStage\install.log" -Wait -PassThru
if ($install.ExitCode -ne 0) { throw "Installation requires investigation: $($install.ExitCode)" }
```

Use the package's actual filename; the example assumes `x64` as in the package
manifest. [Windows Installer options](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/msiexec)
provide silent removal with `/x` in place of `/i`. Wait for the process and
inspect its exit status; no UI does not mean success. A restart-required outcome
must be handled by the management system before launch/acceptance, not discarded.
Do not delete the shared WebView2 runtime when removing Readmit.
The installed executable is `C:\Program Files\readmit\readmit-desktop.exe`.
Its GUI subsystem requires redirected output to capture `--version`; the native
CI uses the packaging identity tool rather than assuming console output.

### macOS

The managed path uses the PKG, not a scripted drag from the DMG:

```sh
sudo installer -pkg /private/tmp/readmit-stage/readmit-desktop_VERSION_arm64.pkg -target /
/Applications/readmit-desktop.app/Contents/MacOS/readmit-desktop --version
```

Use `x86_64` for the Intel package filename. Read the installed identity and
compare it with the selected CLI's `readmit --version`. Remove only the installed
`/Applications/readmit-desktop.app` when uninstalling, through the organization's
approved removal procedure. Package receipts are inventory, not evidence; a
receipt alone does not prove the app remains installed. Never remove a project,
user state directory or shared webview as part of this operation.

### Ubuntu

With the complete approved dependency closure already in the local APT cache,
run the native installer with downloads disabled:

```sh
sudo apt-get --no-download -y install /srv/readmit-stage/readmit-desktop_VERSION_amd64.deb
dpkg-query -W -f='${Status}\n' readmit-desktop
/usr/bin/readmit-desktop --version
sudo apt-get --no-download -y remove readmit-desktop
```

Use `arm64` on arm64 machines. `--no-download` makes a missing cached package a
failure rather than an attempted network repair. Do not continue after a failed
installation or use forced dependency options. Resolve the missing package on
the staging machine and repeat the native install. The removal line is for the
uninstall drill, not the normal deployment sequence. Removing Readmit does not
remove projects or deliberately purge shared GTK/WebKit dependencies.

## Offline activation and ordinary-user permissions

Receive the signed entitlement and the vendor's public trust document through
an authenticated administrative channel. No production trust store is embedded.
The trust document holds public verification keys, never the issuer's private
key. Choose organizational author/device names, not hardware fingerprints.
For a v2 named-author entitlement, run as the eventual user:

```sh
readmit license verify /private/stage/entitlement.json --trust /private/config/vendor-keys.json --author analyst --device workstation-1
readmit license import /private/stage/entitlement.json --trust /private/config/vendor-keys.json --author analyst --device workstation-1 --output /private/state/entitlement
readmit license show /private/state/entitlement --trust /private/config/vendor-keys.json
```

The paths and signed assignments are examples. Create their parent directories
with access restricted to that user and administrators; the output directory
must be new and outside evidence. Under v1 omit `--author`; v1 counts bound
devices, not named humans. Verification reports the term state; use
`--require CAPABILITY` on `verify` or `show` when admission requires a specific
currently granted capability. Import alone is not proof of an active term.

An existing output or another device assignment refuses instead of overwriting.
Do not erase a store to renew: use `license renew STORE ENTITLEMENT --trust TRUST`.
V1-to-v2 adoption requires a fresh store, not renewal into a different contract.
An interrupted update retains an `.incomplete` file; stop concurrent operations,
retain it for investigation and follow [license recovery](license.md) before
retrying. `license release STORE` records local release, not vendor revocation.
Offline revocation requires an updated trust/entitlement file; clock and restored
snapshot limitations remain as documented in [v2 licensing](license-v2.md).
Reading, verifying and exporting existing evidence stays ungated.

Keep binaries and trust configuration administrator-controlled and unwritable
by application users. Give each user write access only to their approved project
and state directories. On Windows enforce this with ACLs; Unix mode bits do not
prove Windows privacy. Preserve user state, activation records and evidence on
uninstall. A package installer does not provision storage encryption, backups,
service credentials or retention policy.

## Proxies, certificates and runners

There is no global Readmit proxy or insecure-TLS switch. Local inspection,
license import and staged upgrade checks require no network access. Package
acquisition uses the staging machine's approved package-manager/proxy settings;
those settings do not configure Readmit's transports.

MLLP is a direct TCP transport, not HTTP proxy traffic. Use explicit endpoint
TLS configuration from [targets](target.md): `server_name` verifies the peer
name and `ca_file` selects PEM authorities in place of platform roots.
Client private keys remain credential references. Never turn verification off
to accommodate interception or copy a private key into an installer command.

The [customer runner](customer-runner.md) needs direct HTTPS reachability to the
customer hub with verified TLS 1.3 and mTLS. Its required `ca`, `certificate`,
`key` and `token` configuration fields are not proxy settings; proxy environment
variables and redirects are unsupported. The explicit CA file supplies the
runner's trust roots. A fully disconnected workstation can inspect and activate;
a runner disconnected from its hub cannot enroll or renew execution authority.
A proxy-only network is not an accepted runner deployment.

Use the five-target CLI archive for the runner. The supplied Linux systemd unit
runs as `readmit-runner`, has `Restart=no`, and writes only under its private
`/var/lib/readmit-runner` root. Provision the service identity, protected config,
private inbox/evidence and credential-provider access before enabling it. The
container example needs a staged static executable and explicit private mounts;
there is no Windows service installer or macOS launch daemon supplied. Follow
the runner's signed-update verification and exact engine/profile negotiation;
never substitute a newer binary for an already pinned run. Stop before update,
retain uncertain job claims, and use read-only recovery after interruption.

## Policy friction and acceptance record

Gatekeeper, SmartScreen, application control, endpoint protection, execution
allowlists and restricted filesystem policies can block previews, webview
processes or credential providers. Preserve the rejection and exact artifact
identity; obtain administrator approval for the actual publisher/artifact.
Do not disable protection, strip quarantine or bypass a failed prerequisite.
A headless `--version` check does not prove interactive webview launch, file
picker access, keyboard accessibility or endpoint-policy compatibility.

Before rollout, retain results from a clean managed machine for each claimed
OS/version/architecture: silent install; missing prerequisite refusal; ordinary
user launch; disconnected activation; wrong assignment/altered signature
refusal; uninstall preserving a synthetic project; installer interruption and
identity readback; approved upgrade and rollback with evidence identities intact.
Use [upgrade preparation](upgrade.md) for the verified recovery archive, and
retain the previous application installer separately. Its `ready` report is
not cryptographic signature verification or a native installation test. Windows
MSI downgrade refusal is not an automatic rollback facility; an administrator
must remove the newer package before reinstalling the previously approved one.

Existing automated coverage is explicit: `tests/license_test.go` and
`tests/license_v2_test.go` exercise public CLI activation/refusals;
`tools/test_package_desktop.py` checks package declarations and refusals; the
`desktop-package` CI matrix installs, checks identity and removes real unsigned
packages on its five runners. These tests do not certify offline dependency
closure, actual signature/notarization/stapling, enterprise policy, interrupted
native installation or signed upgrade/rollback. Those exact-candidate managed
lab results and protected signing identities remain owner release gates.
