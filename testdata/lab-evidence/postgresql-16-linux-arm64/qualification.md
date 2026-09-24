# Synthetic database qualification

- Engine: postgresql
- Selected major: 16
- Image: `postgres:16`
- Image ID: `sha256:c319f2a8182bcdcbb3297d568e2a9cc9e7da3a624438e14b63921c719817e1b6`
- Repo digests: `postgres@sha256:a3b7f434b2dc57ce85a67e171163eb8ab1a1ebcb39d27484661f26b1dfbe30d6`
- Image platform: linux/arm64
- Server version: `16.15 (Debian 16.15-1.pgdg13+2)`

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
