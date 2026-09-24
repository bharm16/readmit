# Digest-pinned main-branch database lab

- Source: [manual workflow run 36029817324](https://github.com/bharm16/readmit/actions/runs/36029817324), `workflow_dispatch`, `cell=all`.
- Workflow: `Synthetic database qualification` on the default branch.
- Tested commit: `cea49f96669a18759ba323c38a915886d62430b6` (merged PR #457).
- Run record created/updated: 2026-09-24 16:47:35–16:52:04 UTC; all six matrix jobs and artifact uploads completed successfully.
- Each folder is the byte-for-byte extracted artifact named `database-lab-<engine>-<version>` from that run. The earlier branch-discovery evidence remains in separate sibling folders.
- Each folder holds 94 SHA-256-inventoried files plus `sha256sums.txt`. The manifest and every member were checked after download and after copying into this repository. The production strict readers accepted the retained completion and database result documents. A scan found no private-key markers, password assignments, database DSNs, or runner paths; the lab also screened its generated secrets before upload.
- Claims apply only to the recorded image digest, Linux/amd64 platform, server patch, and tested Readmit commit. Oracle was not run.

| Cell folder | SHA-256 of `sha256sums.txt` |
| --- | --- |
| `postgresql-16-linux-amd64` | `cbae73d3a6db1058cb9120c9bd72158690287c0cffd239cbaa4491f2db2b1dac` |
| `postgresql-17-linux-amd64` | `a54d566f5d3e2d3e504f50f2b01a3b018cd8441b6df7f39f5ca860106f92e5d0` |
| `postgresql-18-linux-amd64` | `dd0451ec02053bbfafa9359a348a7e69e24fc33a29a1ad70f26785811c82a4cf` |
| `sqlserver-2019-linux-amd64` | `a132dc2211b5f8836fde415ed5ed8d803078359603c7202da902e73505f31a3f` |
| `sqlserver-2022-linux-amd64` | `53c64b0b1d8e7240f63278473a4862927771953c9e0a759bad688404c58839e4` |
| `sqlserver-2025-linux-amd64` | `096dad0b91f1c522f03a4b30aeabd04689fa2117b8639a4ce1cbdffbd8f802a1` |
