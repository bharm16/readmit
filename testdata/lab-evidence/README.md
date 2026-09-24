# Synthetic database lab evidence

These three folders are the retained local Linux/arm64 PostgreSQL 16, 17 and
18 qualification runs for issue #75. Every source value is synthetic. Each
folder holds the selected image ID and repository digest, the server's actual
patch string, a 94-file SHA-256 manifest, and the original
`readmit-observation-completion/v1` and typed `readmit-database-read/v1`
snapshots for the scenarios named in `qualification.md`. The public command
line's own collection is under `cli-public-read/`.

The password, CA signing key and server private key were generated for each
run, screened from the output, and destroyed with the isolated container and
temporary directory. The evidence records no SQL Server or Oracle result and
does not justify claims for those engines or for another image/platform.
