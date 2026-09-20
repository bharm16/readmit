# Hub runtime dependency notices

The separate hub archive includes these dependency license texts. Exact module
versions also appear in `../go.mod` and `../go.sum`.

| Module | Version | License |
| --- | --- | --- |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| github.com/jackc/puddle/v2 | v2.2.2 | MIT |
| golang.org/x/sync | v0.22.0 | BSD-3-Clause, Go patent grant |
| golang.org/x/text | v0.41.0 | BSD-3-Clause, Go patent grant |

Go's standard library is BSD-3-Clause; its toolchain license is included as
`go-LICENSE`. PostgreSQL is installed and administered separately by the customer.

The imported engine observation collector also brings Microsoft go-mssqldb
v1.11.0 (BSD-3-Clause), go-ora/v2 v2.9.0 (MIT), golang-sql civil/sqlexp
(Apache-2.0), Google UUID (BSD-3-Clause), shopspring decimal (MIT), and
golang.org/x/crypto v0.56.0 (BSD-3-Clause with Go patent grant). Complete license
and vendored-code notices are retained here for the deployment archive.
