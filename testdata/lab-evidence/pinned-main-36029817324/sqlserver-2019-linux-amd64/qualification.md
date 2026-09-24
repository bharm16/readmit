# Synthetic database qualification

- Engine: sqlserver
- Selected major: 2019
- Image: `mcr.microsoft.com/mssql/server@sha256:ef0b8db33970ecd01bed49c3a84a1d083c435a9891718df619298b67b352e74a`
- Image ID: `sha256:aa00fc4a3e14cc8baf6ce7447e738023aab84b90f3bf913304729201a8d80556`
- Repo digests: `mcr.microsoft.com/mssql/server@sha256:ef0b8db33970ecd01bed49c3a84a1d083c435a9891718df619298b67b352e74a`
- Image platform: linux/amd64
- Server version: `Microsoft SQL Server 2019 (RTM-CU32-GDR) (KB5122772) - 15.0.4490.9 (X64) Aug 21 2026 12:54:17 Copyright (C) 2019 Microsoft Corporation Developer Edition (64-bit) on Linux (Ubuntu 20.04.6 LTS) <X64>`

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
