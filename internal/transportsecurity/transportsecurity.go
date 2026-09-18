// Package transportsecurity owns readmit's one TLS rule, for every path that
// negotiates TLS in either direction.
//
// The rule is short and has no options: TLS 1.2 is the floor and TLS 1.3 is
// permitted, certificate verification is always on, and an explicitly
// configured certificate authority replaces the platform's roots rather than
// being added beside them. There is no insecure mode, no verification callback
// and no cipher or version negotiation flag, because a session that skipped
// verification would be reported as a trust readmit never established.
//
// It is one implementation so a diagnosis, a send and a capture cannot honour
// three different versions of the same settings. A caller that needs different
// behaviour changes the rule here, where every path sees the change.
package transportsecurity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// ClientConfig is the configuration for a connection readmit opens. serverName
// is the name the endpoint's certificate is verified against and is required:
// an empty name would leave crypto/tls with nothing to check the certificate's
// subject against. authorities, when present, are PEM certificates that replace
// the platform roots for this connection.
func ClientConfig(serverName string, authorities []byte) (*tls.Config, error) {
	if serverName == "" {
		return nil, errors.New("a verified TLS connection requires the server name its certificate is checked against")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if len(authorities) == 0 {
		return config, nil
	}
	pool, err := certificates(authorities)
	if err != nil {
		return nil, err
	}
	config.RootCAs = pool
	return config, nil
}

// ServerConfig is the configuration for a listener readmit binds. certificate
// and key are the PEM chain and the private key that pair with it; the key is
// resolved from the store its credential reference names and exists only for
// the lifetime of this process, never in a file readmit wrote.
//
// clientAuthorities turns on mutual TLS: a client certificate is required and
// verified against exactly those authorities. Declaring them has no weaker
// meaning. Verifying a client against the platform's public roots would accept
// any certificate a public authority ever issued, and verifying one only when a
// client happens to offer it would let "no certificate" pass as "verified" —
// both are the unknown-reads-as-approved shape this refuses. Without them no
// client certificate is requested, and the transport authenticates nobody.
func ServerConfig(certificate, key, clientAuthorities []byte) (*tls.Config, error) {
	if len(certificate) == 0 || len(key) == 0 {
		return nil, errors.New("a TLS listener requires a certificate and the private key its credential reference names")
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		// The parser's own message is replaced rather than wrapped, because it
		// describes key material and readmit repeats nothing a credential says.
		return nil, errors.New("the configured certificate and the private key its reference names are not a pair")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	if len(clientAuthorities) == 0 {
		return config, nil
	}
	pool, err := certificates(clientAuthorities)
	if err != nil {
		return nil, err
	}
	config.ClientCAs, config.ClientAuth = pool, tls.RequireAndVerifyClientCert
	return config, nil
}

func certificates(authorities []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(authorities) {
		return nil, errors.New("the configured certificate authority file contains no certificates")
	}
	return pool, nil
}
