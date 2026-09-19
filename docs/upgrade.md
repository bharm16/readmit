# Upgrading without losing evidence

A workstation that holds an interface investigation holds the only copy of some
of it. Installing a newer readmit on that machine has to be a decision somebody
makes deliberately, with the answer to one question in front of them first:

> **Does the build about to be installed read what this machine already holds?**
> If it does not, the answer is to keep the build that does. Nothing is
> converted, migrated in place, or rewritten to make an upgrade succeed.

`readmit upgrade` answers that question and takes the archive the upgrade is
rolled back to. It installs nothing.

```sh
readmit upgrade check --candidate /srv/staged/readmit-1.4.0 \
  --project incident-4821 --run incident-4821/rerun-2026-09
readmit upgrade prepare incident-4821 --candidate /srv/staged/readmit-1.4.0 \
  --output incident-4821.rollback --approve
```

## Commands

| Command | What it does |
| --- | --- |
| `upgrade check --candidate STAGED_DIRECTORY --project PROJECT [--run RUN]` | Reads the staged candidate, reviews the artifacts named, and prints one `readmit-upgrade-plan/v1`. Writes nothing |
| `upgrade prepare PROJECT --candidate STAGED_DIRECTORY --output NEW_ARCHIVE --approve` | Takes the verified recovery archive this upgrade would be rolled back to |

## The update check is explicit, and it is offline

There is no update service, no automatic check, and no network connection of
any kind — readmit has none of those anywhere
([support matrix](support-matrix.md#not-available-in-this-preview)). A
**candidate** is a directory an administrator already put on the machine: the
native packages and the `readmit-desktop-package/v1` manifest
[the packaging tool](desktop.md#native-packages) wrote beside them.

The check reads that manifest strictly — unknown members and unknown versions
are errors, and a later contract version is refused as an unsupported version
rather than as an invalid document, so "this release does not read that" and
"that is not a manifest" never read as the same answer — and then re-reads
every package file the manifest records and compares its SHA-256 with the
recorded one.

| Reported for a staged package | What it means |
| --- | --- |
| `intact` | The file is the file the manifest recorded |
| `altered` | The file is there and its bytes are not those. A download that stopped partway reads as this |
| `absent` | The manifest records it and the directory does not hold it |

The staged directory must hold the manifest and the packages it names and
nothing else. An unrecorded file beside them refuses the whole candidate: it is
exactly how the wrong installer gets run.

This is an integrity check over what was staged, not the package verification
[`tools/package_desktop.py verify`](desktop.md#native-packages) performs on a
build machine. That one opens each package with the tools that wrote it; this
one re-reads bytes, which is what an administrator can do on a workstation that
holds no build toolchain at all.

The identity the candidate is compared against is the build **running the
check**, which is the same engine stamp the desktop application of that commit
carries ([one build behind both entry points](desktop.md#one-build-behind-both-entry-points)).
Two identities are reported and neither is ordered against the other: this
release does not decide that one version string is newer than another. It
refuses a candidate that names the identity already running, because that is
not an upgrade.

## Administrator approval

readmit installs nothing and cannot. The installation is the platform's own
installer, run with elevation by an administrator —
`apt-get install`, `installer -pkg`, `msiexec /i` — so the approval is that
person's, at the point the operating system asks for it. Nothing here can
perform an installation, so nothing here can perform one silently.

The one thing `upgrade` does write is the recovery archive, and that needs
`--approve` on the command line. Without it nothing is written at all, not even
a partial archive:

```
readmit: upgrade prepare requires --approve; an administrator approves an
upgrade before anything is written
```

## The compatibility review

At least one `--project` or `--run` is required. A check that reviewed nothing
is not a compatibility review, and reporting one as `ready` would be exactly
the auto-pass the command exists to prevent. What this machine holds is named
by the operator; readmit discovers no artifact of its own and opens nothing it
was not given.

Each `--project` and `--run` is opened with the build running the check and
reported as one of three states. Only `readable` is a pass.

| Reported | What it means |
| --- | --- |
| `readable` | This build reads every document of it |
| `unsupported` | It names a contract version this build does not read |
| `unreadable` | This build refused it for some other reason: damaged, absent, or holding a document it could not open |

A **project** is reviewed with the same migration preview
[`project migration-preview`](project-lifecycle.md) runs: the project document,
the revisions document, an optional quota declaration and every top-level file
declaring an index contract. There is no converter for an unknown contract, and
there never is one; a project that is not readable is reported, not migrated.

A **run** is reviewed through the `readmit-engine/v1` pin it retained beside its
plan ([durable runs](durable-runs.md)). That pin names the engine build, the
spec contract and the semantic profile the run was evaluated under, so a later
build states plainly whether it reads that run rather than guessing from
evidence it may not understand. A different engine build is not a reason to
refuse anything — builds change, and the contracts are what decide readability —
but a spec contract or profile version this release does not read is
`unsupported` by name.

`unsupported` is reported only for a run. A run retains an explicit pin and a
project does not, so a project document this build cannot open is `unreadable`
without readmit claiming to know whether it is newer or damaged. That
distinction matters here more than anywhere: reporting evidence as damaged when
it is only newer is how somebody decides to throw it away.

## Nothing is rewritten

Neither command writes one byte into the evidence it reads. `check` writes
nothing at all. `prepare` writes only the new recovery archive, at the path it
was given, which must be outside the project and must not already exist.

No document gains a member, and no contract changes, because an upgrade can be
checked. `readmit-engine/v1`, `readmit-job/v1`, `readmit-backup/v1`,
`readmit-project/v1`, `readmit-desktop-package/v1` and
`readmit-desktop-packaging/v1` are all read exactly as they are;
`readmit-upgrade-plan/v1` is a new contract beside them
([ADR-0003](adr/0003-specs-are-strict-json-with-typed-operators.md)).

## The migration backup and the rollback boundary

`upgrade prepare` takes an ordinary verified `readmit-backup/v1` recovery
archive of the project — the same artifact `project archive` writes, with the
same guarantees ([backing up a workspace](backup.md)): every registered case
and revision is opened and recorded as `verified`, `changed`, `unreadable` or
`missing`, a derived index is recorded as the declarations it was built under
rather than copied, and a backup that is honest but not whole exits non-zero.

It refuses to take one when the staged candidate is not intact, because an
archive presented as *this upgrade's* rollback point stands for an upgrade that
was actually staged. It refuses when the build running it does not read the
project, because a rollback point taken by a build that cannot verify the
evidence vouches for nothing.

It deliberately does **not** require the candidate to be signed for
distribution. A rollback point must never wait on a signing decision this
repository does not hold, so `prepare` takes the archive and then restates the
refusal on its last line:

```
Rollback point taken under engine build: 1.3.2
Installing this candidate is still refused: the staged candidate records that
it is not signed for distribution, so it is a development preview and not an
upgrade this release installs
```

`prepare` exiting zero means a complete verified archive was written. It never
means the candidate may be installed; `check` says that.

### What rolling back does and does not do

The archive restores the project **as it was when the archive was taken**, into
a new directory, with every identity the project recorded intact. Four things
are outside that boundary and are stated rather than implied:

- **Work done after the archive was taken is not in it.** A backup is taken
  when somebody takes one and it is whole; there is nothing incremental.
- **Restoring an older project does not make a newer contract readable.** If a
  newer build wrote a document under a version an older build does not read,
  rolling the application back leaves that document exactly as `unsupported`
  as it was. Contract versions change through a new name and a reader for both,
  never through in-place migration, so the remedy is the build that reads it.
- **readmit does not roll the application back.** Reinstalling the previous
  package is the administrator's own step through the platform's installer,
  from a package they still hold. readmit keeps no copy of it.
- **Nothing outside the project is restored.** References from other projects,
  exported reports and old backups are not rewritten; see
  [retention and references](project-lifecycle.md#retention-and-references).

## Installer failure recovery

An installation that fails is the platform installer's failure, and the
recovery is bounded by one fact: **no installer of ours touches evidence.**
Projects, case bundles, runs and the desktop shell's local state are files in
folders an operator chose; a package installs an application and removes an
application ([installing and removing](desktop.md#installing-and-removing)).
So a failed, partial or rolled-back installation cannot have changed a byte of
what the machine holds.

What is left uncertain is only *which application is installed*, and that is
read back directly:

```sh
readmit --version
readmit upgrade check --candidate /srv/staged/readmit-1.4.0 --project incident-4821
```

If the identity is still the old one the installation did not take effect;
stage the candidate again — `altered` and `absent` are what a download that
stopped partway reports — and run the installer again. If it is the new one,
the review in the same plan says whether this build reads the machine's
evidence. Either way the recovery archive taken by `prepare` is still there and
is still the rollback point it was.

## The document: `readmit-upgrade-plan/v1`

```json
{
  "schema": "readmit-upgrade-plan/v1",
  "installed": "1.3.2",
  "candidate": "1.4.0",
  "os": "darwin",
  "arch": "arm64",
  "signed_for_distribution": true,
  "staged": [
    {"name": "readmit-desktop_1.4.0_arm64.pkg", "format": "pkg", "state": "intact"}
  ],
  "retained": [
    {"name": "incident-4821", "kind": "project", "state": "readable"},
    {"name": "rerun-2026-09", "kind": "run", "state": "readable"}
  ],
  "state": "ready"
}
```

`state` is `ready` or `refused`, and every reason for a refusal is visible in
the members above it, so a reader holding only the document reaches the verdict
the command did. The command prints that document and nothing else, then exits
2 with the first reason on stderr — the plan comes first, so an operator reads
why before they read that.

Bounded at 64 KiB for the manifest, 16 packages, and 1 GiB for one package
file. Past a bound the candidate is refused rather than read as far as it fit.

## What is refused

| Situation | What readmit does |
| --- | --- |
| The staged directory holds no readable `manifest.json` | Refuses |
| The manifest declares a `readmit-desktop-package` version this release does not read | Refuses as an unsupported version, distinctly from an invalid document |
| The manifest holds an unknown member, or omits a declared one | Refuses |
| The staged directory holds a file the manifest does not record | Refuses the whole candidate |
| A staged package is altered or absent | Reports it and refuses the upgrade |
| The candidate was built for another operating system or architecture | Refuses |
| The candidate names the build already running the check | Refuses: that is not an upgrade |
| The candidate records `"signed_for_distribution": false` | Refuses: a development preview is not an upgrade this release installs |
| A reviewed project or run is not `readable` | Reports it and refuses the upgrade |
| `check` with no `--project` and no `--run` | Refuses: a check that reviewed nothing is not a compatibility review |
| `prepare` without `--approve` | Refuses, and writes nothing |
| `prepare` where the archive destination exists, or is inside the project | Refuses: creation is exclusive and `artifactpath` reserves both |

## Privacy

- The plan names the staged packages, and names each reviewed artifact by its
  own directory entry. It repeats no path an operator typed, no message
  content, no retained value and no identity.
- Diagnostics on stderr name what went wrong and carry the position of the
  entry at fault, never its name.
- Nothing is sent anywhere. There is no update service, no telemetry, no crash
  reporting and no analytics; readmit has none of those.

## Not supported in this release

- **Installing anything.** `upgrade` reads and writes one archive. The
  installation is the platform's installer, run by an administrator.
- **A signed or published candidate.** Every package this repository builds
  records `"signed_for_distribution": false`, so `upgrade check` refuses every
  candidate staged from one. Signing identities, notarization and publication
  are release inputs this repository does not hold; see
  [D5](product-decisions.md#d5--desktop-distribution-and-signing) and
  [release acceptance](release-acceptance.md).
- **An automatic update check, a release feed, or a download.** The candidate
  is staged by a person.
- **Ordering two version strings.** Both identities are reported; which one is
  newer is the administrator's own knowledge of what they staged.
- **Converting or migrating evidence between contract versions.** There is no
  converter for an unknown contract, in either direction.
- **Rolling the application back.** Reinstalling the previous package is the
  administrator's step, from a package they still hold.
- **Reviewing anything but a project directory and a durable run directory.**
- **Verifying a package's contents, signature or provenance.** The check
  re-reads bytes against the manifest's digests.
