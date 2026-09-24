# Synthetic database lab evidence

The three Linux/arm64 PostgreSQL folders are retained local qualification runs
for issue #75. The six Linux/amd64 folders are retained **branch discovery**
results from the all-cell [manual run 36025500364](https://github.com/bharm16/readmit/actions/runs/36025500364).
That run used mutable major tags to discover exact image digests. A final
default-branch run of the digest-pinned workflow remains necessary before
claiming SQL Server qualification. Oracle was not run. Every source value is
synthetic. Each folder holds the selected image ID and repository digest, the
server's actual patch string, a 94-file SHA-256 manifest, and the original
`readmit-observation-completion/v1` and typed `readmit-database-read/v1`
snapshots for the scenarios named in `qualification.md`. The public command
line's own collection is under `cli-public-read/`.

The password, CA signing key and server private key were generated for each
run, screened from the output, and destroyed with the isolated container and
temporary directory. Each result applies only to its recorded image, platform,
server patch, and test revision; it does not justify claims for another cell.
