# Synthetic database lab evidence

The three Linux/arm64 PostgreSQL folders are retained local qualification runs
for issue #75. The six Linux/amd64 folders are retained **branch discovery**
results from the all-cell [manual run 36025500364](https://github.com/bharm16/readmit/actions/runs/36025500364).
That run used mutable major tags to discover exact image digests. The separate
[`pinned-main-36029817324`](pinned-main-36029817324/PROVENANCE.md) folder retains
all six successful, digest-pinned default-branch artifacts from commit
`cea49f96669a18759ba323c38a915886d62430b6`. Those exact PostgreSQL and SQL
Server image/platform/server-patch combinations are qualified by the retained
synthetic lab evidence. Oracle was not run and remains unqualified. Every source
value is synthetic. Each cell folder holds the selected image ID and repository
digest, the server's actual patch string, a 94-file SHA-256 manifest, and the original
`readmit-observation-completion/v1` and typed `readmit-database-read/v1`
snapshots for the scenarios named in `qualification.md`. The public command
line's own collection is under `cli-public-read/`.

The password, CA signing key and server private key were generated for each
run, screened from the output, and destroyed with the isolated container and
temporary directory. Each result applies only to its recorded image, platform,
server patch, and test revision; it does not justify claims for another cell.
