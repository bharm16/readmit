# Synthetic database qualification

- Engine: sqlserver
- Selected major: 2022
- Image: `mcr.microsoft.com/mssql/server:2022-latest`
- Image ID: `sha256:5b0916c7af8ca97ce9390be7e1182862d6fa90bff9f44f035dbe1626df140637`
- Repo digests: `mcr.microsoft.com/mssql/server@sha256:4402d880dd4c34bfa7d8705e56a86cd6c88da80a1f6bbbe741f999e76264a090`
- Image platform: linux/amd64
- Server version: `Microsoft SQL Server 2022 (RTM-CU27) (KB5104824) - 16.0.4295.3 (X64) Aug 26 2026 11:02:22 Copyright (C) 2022 Microsoft Corporation Developer Edition (64-bit) on Linux (Ubuntu 22.04.5 LTS) <X64>`

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
