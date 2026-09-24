# Synthetic database qualification

- Engine: postgresql
- Selected major: 18
- Image: `postgres:18`
- Image ID: `sha256:d8a40176c29aa0c7a20a19f85ddddc47f72d8d6789a7a86a21f3713e71fb4ad6`
- Repo digests: `postgres@sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722`, `postgres@sha256:86c951e05bf56c93d95d397747fb8820ac76cc3bedb78f43abd83eedbe3666ae`
- Image platform: linux/arm64
- Server version: `18.6 (Debian 18.6-1.pgdg13+2)`

The opt-in lab test passed over a generated CA, a verified server name, password authentication,
a separate setup principal and a SELECT-only observation principal. Each scenario retains its
own completion and typed snapshot in this artifact. No credential value or private key is retained.

## Retained scenarios

- bound-filter
- byte-limit
- cancelled
- cli-public-read
- connection-recovered
- decimal
- empty
- fresh-recovery
- invalid-mapping
- long-text
- lost-connection
- null-key
- permission-recovered
- permission-refused
- populated
- query-deadline
- row-limit
- timezone
- wrong-principal
