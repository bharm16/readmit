# Fresh independent lab recreation, September 27, 2026

This is the complete credential-filtered export of a second isolated lab,
created from a clean checkout of commit
`89295a092e8245c91301ae6a85017ddaea7e4c9e` using the documented
`init`, `up`, `qualify`, `down`, `export` sequence. No target, controller,
oracle, fixture or lock was changed to obtain this result. The new state
directory, credentials, private CA, Compose project and volumes did not reuse
the first lab's state or acquisitions. Docker's content-addressed image/build
cache was available; this is fresh deployment evidence, not an empty-cache
download test or a claim that a separate human operator performed the steps.

The host used Docker Engine 29.8.0, Compose v5.5.1 and an 8 GiB Linux/arm64
Docker VM. Stimulus acknowledgement times in the acquisitions span
2026-09-27 16:21:38 through 16:26:23 UTC. The pinned releases actually running
were OIE 4.6.0 on Temurin Java 17.0.17+10, HAPI 8.6.0 for FHIR R4 4.0.1,
and PostgreSQL 16.11. [Runtime](recreation-20260927/runtime.json) records
the running image IDs, configuration hashes and internal network check;
[the capability statement](recreation-20260927/capability-statement.json)
comes from the running HAPI server.

[The qualification receipt](recreation-20260927/qualification.json)
records all 24 actual revisions: duplicate, field mutation, linkage, drop,
reorder and late duplicate, each in positive, defective, corrected and
reintroduced modes. Each revision has an actual OIE channel export for each
engine route and retained receiver/FHIR observations. Across all three
boundaries, positive and corrected modes pass, while defective and
reintroduced modes fail with the named defect's required signature. All
transport responses are positive. The late-duplicate checks retain the
initial and post-horizon observations, so an immediate duplicate cannot
stand in for delayed behavior.

The same receipt contains eleven successful auth/TLS checks, each case's
owned reset, and the final empty-state verification.
[Controller completion](recreation-20260927/controller-complete.json)
binds the actual acquisitions and confirms 26 retained PostgreSQL resource
rows after the scoped deletion checks. HAPI retains resource history; this
row count does not claim active resources remained after reset.
[Teardown](recreation-20260927/teardown.json) confirms this project's
containers, volumes and network were removed. No other project's resources
were selected for teardown.

The export contains 79 acquired files, 8,294,607 bytes, plus its
[manifest](recreation-20260927/manifest.json). The manifest's SHA-256 is
`3355777952888e5e126932ae8bef5eb09f6edf05625ad5d6a779fda1de5215a3`.
It was emitted by the existing exporter after checking the generated lab
credentials, private-key markers and JWT-shaped values. No environment
files, private credentials, state directory or server logs are included.
The full export is retained byte-for-byte, including the controller's last
incremental receipt; no acquisition or completion record was edited.

Reopen it without Docker, network access or credentials from the repository
root:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p test_integration_lab.py -v
```

The default integrity test reads these acquisitions, reproduces every route
outcome and defect signature, checks the export manifest and verifies that
removed acquisitions or changed channel bytes cannot remain qualified.
To check another fresh export, set `READMIT_LAB_EVIDENCE` to its `export/`
directory. The live recreation itself still uses the commands in
[the lab guide](../../../docs/independent-integration-lab.md).

This proves the documented independent reference lab can be recreated with
fresh state on Linux/arm64. It does not prove hosted Linux/amd64 operation,
native Desktop/runner integration (IG22), customer authorization, EHR
workflow correctness or Epic/Oracle Health certification.
