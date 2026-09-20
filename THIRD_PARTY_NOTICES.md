# Third-party notices

The executable contains Go's standard library (BSD-3-Clause), Cobra v1.10.2
(Apache-2.0), pflag (BSD-3-Clause), and, on Windows, mousetrap (Apache-2.0).
Exact application dependency versions are recorded in `go.mod` and `go.sum`.
Their license texts are included in `licenses/`.

The embedded v2.5.1 field labels are adapted from nHapi under MPL-2.0. Their
preferred source form is supplied as `dictionary/fields-v251.json` in release
archives, with attribution and the original source revision in
`docs/dictionary-provenance.md`. No nHapi runtime code is included.

The desktop shell is a separate module and is not part of the release archives.
Its native packages carry this file, the existing `licenses/` texts, and the
preferred dictionary source and provenance beside the application.
Its executable contains Wails 2 (MIT) and its dependencies, recorded in
`desktop/go.mod` and `desktop/go.sum`, and the bundled interface contains React
and React DOM (MIT), recorded with their exact versions in
`desktop/frontend/package-lock.json`. Those two license texts are included in
`licenses/`. Vite, TypeScript and the Vite React plugin build the interface and
are not shipped inside it.

GoReleaser, WiX and govulncheck are build/development tools, not application
runtime dependencies. WiX writes the Windows installer database and
contributes no code to the application that database installs. GitHub Actions
attests only executable and package build outputs, never customer evidence.

The customer-controlled hub is a separate module and is not in the CLI or
desktop packages. It uses pgx v5.11.0 (MIT) and its Go dependencies, pinned in
`hub/go.mod` and `hub/go.sum`. Complete dependency license texts are retained in
`hub/licenses/` and included in the hub's separate deployment archive. PostgreSQL
is customer-installed server software, not embedded in any Readmit executable.

The CLI database-observation path also contains pgx v5.11.0 (MIT), Microsoft
Go SQL Server driver v1.11.0 (BSD-3-Clause), and go-ora/v2 v2.9.0 (MIT), selected
by product decision D3. Their pure-Go runtime dependencies include pgpassfile,
pgservicefile and puddle (MIT), golang-sql civil and sqlexp (Apache-2.0), Google
UUID (BSD-3-Clause), shopspring decimal (MIT), and golang.org/x/crypto, /sync
and /text (BSD-3-Clause). Their complete license texts and the SQL Server
driver's vendored-code notices are included in `licenses/`. These clients do
not bundle database servers, Oracle Instant Client, ODBC, or Java. Driver
availability and static builds do not qualify any server/version combination.
