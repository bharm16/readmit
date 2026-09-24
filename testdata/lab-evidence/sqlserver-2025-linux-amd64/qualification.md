# Synthetic database qualification

- Engine: sqlserver
- Selected major: 2025
- Image: `mcr.microsoft.com/mssql/server:2025-latest`
- Image ID: `sha256:78eb6a93ef20501bd754282cc216188a0acee1cfa6119c33eef0b21599c1f726`
- Repo digests: `mcr.microsoft.com/mssql/server@sha256:2b5b581621126574f3d1f75e78d3eebe8d05aedb59ad0cfdf9aa42cb0634d726`
- Image platform: linux/amd64
- Server version: `Microsoft SQL Server 2025 (RTM-CU9) (KB5122048) - 17.0.5005.3 (X64) Aug 27 2026 09:30:16 Copyright (C) 2025 Microsoft Corporation Enterprise Developer Edition (64-bit) on Linux (Ubuntu 24.04.4 LTS) <X64>`

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
