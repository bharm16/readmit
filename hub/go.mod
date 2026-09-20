module github.com/bharm16/readmit/hub

go 1.27.0

toolchain go1.27.1

require (
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/microsoft/go-mssqldb v1.11.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/sijms/go-ora/v2 v2.9.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

require (
	github.com/bharm16/readmit v0.0.0
	github.com/jackc/pgx/v5 v5.11.0
)

replace github.com/bharm16/readmit => ..
