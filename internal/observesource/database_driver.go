package observesource

import (
	"context"
	"crypto/tls"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	oracle "github.com/sijms/go-ora/v2"
	"github.com/sijms/go-ora/v2/configurations"
)

// databaseConnector configures one driver to reach only the route's address.
// The route is every network connection's dialer, including any redirection a
// driver attempts, so no DNS re-resolution or failover can widen the
// destination decision.
func databaseConnector(d Database, password string, route destination.Route, config *tls.Config) (driver.Connector, error) {
	checked := route.Address()
	host, port, _ := net.SplitHostPort(checked)
	portNumber, _ := strconv.ParseUint(port, 10, 16)
	timeout, _ := time.ParseDuration(d.limits().Timeout)
	switch d.Driver {
	case "postgresql":
		// All connection fields are explicit, overriding ambient PG* connection
		// settings. Clear parsed runtime parameters, fallbacks and lookup behavior.
		u := url.URL{Scheme: "postgres", Host: checked, Path: "/" + d.Name, User: url.UserPassword(d.Username, password)}
		// Explicit parse settings override every PG* input read by the pinned
		// pgx parser before it can load service/passfile/client-key contents. The
		// parser may stat its built-in home defaults, but none can configure us.
		// pgx resolves any present service key, even an empty one. Supply a
		// private, value-free service entry so PGSERVICE cannot select another file.
		service, err := os.CreateTemp("", "readmit-pg-service-*")
		if err != nil {
			return nil, errors.New("cannot create private database parser configuration")
		}
		defer os.Remove(service.Name())
		_, writeErr := service.WriteString("[readmit]\n")
		closeErr := service.Close()
		if writeErr != nil || closeErr != nil {
			return nil, errors.New("cannot prepare private database parser configuration")
		}
		q := url.Values{"sslmode": {"verify-full"}, "passfile": {""}, "service": {"readmit"}, "servicefile": {service.Name()},
			"application_name": {"readmit"}, "connect_timeout": {"0"}, "sslkey": {""}, "sslcert": {""},
			"sslsni": {"1"}, "sslrootcert": {""}, "sslpassword": {""}, "sslnegotiation": {"postgres"},
			"target_session_attrs": {"any"}, "timezone": {"UTC"}, "options": {""},
			"min_protocol_version": {"3.0"}, "max_protocol_version": {"3.0"}, "channel_binding": {"prefer"}, "require_auth": {""}}
		u.RawQuery = q.Encode()
		c, err := pgx.ParseConfig(u.String())
		if err != nil {
			return nil, errors.New("cannot configure database connector")
		}
		c.Host = host
		c.Port = uint16(portNumber)
		c.Database = d.Name
		c.User = d.Username
		c.Password = password
		c.TLSConfig = config
		c.Fallbacks = nil
		c.RuntimeParams = map[string]string{"application_name": "readmit"}
		c.DialFunc = route.DialContext
		c.ConnectTimeout = timeout
		c.DefaultQueryExecMode = pgx.QueryExecModeExec
		c.LookupFunc = func(context.Context, string) ([]string, error) { return []string{host}, nil }
		return stdlib.GetConnector(*c), nil
	case "sqlserver":
		// NewConnectorConfig does not populate the parser's default protocol
		// list. Pin TCP: a nil list yields a nil connection before prelogin, and
		// no named-pipe or other dialer may bypass the approved address.
		c := mssql.NewConnectorConfig(msdsn.Config{Host: host, Port: portNumber, Database: d.Name, User: d.Username, Password: password,
			Protocols:  []string{"tcp"},
			Encryption: msdsn.EncryptionRequired, TLSConfig: config, HostInCertificateProvided: true, DisableRetry: true,
			DialTimeout: timeout, AppName: "readmit", Workstation: "readmit", PacketSize: 4096})
		c.Dialer = route
		return c, nil
	case "oracle":
		u := url.URL{Scheme: "oracle", Host: net.JoinHostPort(d.ServerName, port), Path: "/" + d.Name, User: url.UserPassword(d.Username, password)}
		q := url.Values{"SSL": {"true"}, "SSL VERIFY": {"true"}, "CONNECTION TIMEOUT": {strconv.Itoa(int(timeout.Seconds()) + 1)}, "PREFETCH_ROWS": {"1"}}
		u.RawQuery = q.Encode()
		c, err := oracle.ParseConfig(u.String())
		if err != nil {
			return nil, errors.New("cannot configure Oracle connector")
		}
		c.TLSConfig = config.Clone()
		c.TLSConfig.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("missing Oracle certificate")
			}
			return state.PeerCertificates[0].VerifyHostname(d.ServerName)
		}
		c.Dialer = route
		// The upstream default populates operating-system user, host, process and
		// executable metadata. Use fixed client labels instead.
		c.ClientInfo.HostName = "readmit"
		c.ClientInfo.OSUserName = "readmit"
		c.ClientInfo.DomainName = ""
		c.ClientInfo.ProgramName = "readmit"
		c.ClientInfo.ProgramPath = "readmit"
		c.ClientInfo.PID = 0
		return &oracleConnector{config: c, driver: oracle.NewDriver()}, nil
	}
	return nil, errors.New("unsupported database connector")
}

func openDatabase(d Database, password string, route destination.Route, config *tls.Config) (*sql.DB, error) {
	connector, err := databaseConnector(d, password, route, config)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	return db, nil
}

// oracleConnector uses the driver's public configured-connection API so client
// metadata is explicit. It installs no session parameters or custom datatypes.
type oracleConnector struct {
	config *configurations.ConnectionConfig
	driver *oracle.OracleDriver
}

func (c *oracleConnector) Driver() driver.Driver { return c.driver }
func (c *oracleConnector) Connect(ctx context.Context) (driver.Conn, error) {
	config := *c.config
	config.TLSConfig = c.config.TLSConfig.Clone()
	conn, err := oracle.NewConnection("", &config)
	if err != nil {
		return nil, err
	}
	if err := conn.OpenWithContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
