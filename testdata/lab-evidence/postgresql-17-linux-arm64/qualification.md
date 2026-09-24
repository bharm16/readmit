# Synthetic database qualification

- Engine: postgresql
- Selected major: 17
- Image: `postgres:17`
- Image ID: `sha256:97432f980da100ebd3e419711efee84e1e97a966d62c035286a07f239ddb4d9c`
- Repo digests: `postgres@sha256:f4c66b820c6f974249089d3d16d86a3698eae11e8746eb6644b2271031e91232`
- Image platform: linux/arm64
- Server version: `17.11 (Debian 17.11-1.pgdg13+2)`

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
