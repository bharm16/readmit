// Package hub implements the customer-operated artifact service. It is separate
// from the offline evidence engine and never interprets or rewrites its bytes.
package hub

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net"
	"os"
	"path"
)

// Config contains paths and account names only, never credential values.
type Config struct {
	Schema      string `json:"schema"`
	Listen      string `json:"listen"`
	Root        string `json:"artifact_root"`
	Socket      string `json:"postgres_socket"`
	Port        uint16 `json:"postgres_port"`
	Database    string `json:"postgres_database"`
	User        string `json:"postgres_user"`
	Certificate string `json:"tls_certificate"`
	Key         string `json:"tls_key"`
	ClientCA    string `json:"client_ca"`
	MaxBytes    int64  `json:"max_storage_bytes"`
}

func ReadConfig(data []byte) (Config, error) {
	var c Config
	if len(data) > 16384 {
		return c, errors.New("configuration too large")
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return c, errors.New("invalid configuration")
	}
	if len(members) != 11 {
		return c, errors.New("all configuration members required")
	}
	for _, v := range members {
		if string(v) == "null" {
			return c, errors.New("null configuration member")
		}
	}
	if err := json.Unmarshal(data, &c, json.RejectUnknownMembers(true)); err != nil {
		return c, errors.New("invalid configuration")
	}
	if c.Schema != "readmit-hub-config/v1" || c.Port == 0 || c.Database == "" || c.User == "" || c.MaxBytes < MaxArtifactBytes {
		return c, errors.New("invalid configuration values")
	}
	for _, p := range []string{c.Root, c.Socket, c.Certificate, c.Key, c.ClientCA} {
		if !path.IsAbs(p) || path.Clean(p) != p {
			return c, errors.New("paths must be absolute and clean")
		}
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || port == "" {
		return c, errors.New("listen must name an IP address and port")
	}
	return c, nil
}

func (c Config) TLS() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(c.Certificate, c.Key)
	if err != nil {
		return nil, errors.New("TLS identity unavailable")
	}
	data, err := os.ReadFile(c.ClientCA)
	if err != nil {
		return nil, errors.New("client trust unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("invalid client trust")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}, nil
}
