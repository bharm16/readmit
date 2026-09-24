package hubclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// CheckItem represents one diagnostic check.
type CheckItem struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitzero"`
}

// Prerequisites holds the full suite of diagnostic checks for connecting to the customer hub.
type Prerequisites struct {
	Passed bool        `json:"passed"`
	Checks []CheckItem `json:"checks"`
}

// DiagnosePrerequisites checks CA readability, client certificate and key reference,
// keypair pairing, hub TLS handshake and hostname, health probe, and IdP endpoint format.
func DiagnosePrerequisites(ctx context.Context, c Config) Prerequisites {
	var checks []CheckItem
	allPassed := true

	record := func(name string, passed bool, msg, detail string) {
		checks = append(checks, CheckItem{
			Name:    name,
			Passed:  passed,
			Message: msg,
			Detail:  detail,
		})
		if !passed {
			allPassed = false
		}
	}

	// 1. CA certificate check
	caBytes, err := readLocalPEM(c.CA)
	if err != nil {
		record("ca_certificate", false, "the CA certificate could not be read", err.Error())
	} else {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			record("ca_certificate", false, "the CA certificate contains no valid certificates", "")
		} else {
			record("ca_certificate", true, "CA certificate is valid", "")
		}
	}

	// 2. Client certificate check
	certBytes, err := readLocalPEM(c.Certificate)
	if err != nil {
		record("client_certificate", false, "the client certificate could not be read", err.Error())
	} else {
		record("client_certificate", true, "client certificate file is readable", "")
	}

	// 3. Client private key reference check
	keyVal, err := c.Key.Locator().Read(ctx)
	if err != nil {
		record("client_key_reference", false, "the private key could not be read from its declared store", err.Error())
	} else {
		record("client_key_reference", true, "private key reference resolved successfully", "")
	}

	// 4. KeyPair check
	var pair tls.Certificate
	if certBytes != nil && keyVal.Expose() != nil {
		var pairErr error
		pair, pairErr = tls.X509KeyPair(certBytes, keyVal.Expose())
		if pairErr != nil {
			record("key_pair_match", false, "the configured certificate and private key do not form a valid pair", "")
		} else {
			record("key_pair_match", true, "certificate and private key match", "")
		}
	} else {
		record("key_pair_match", false, "cannot verify key pair without certificate and key", "")
	}

	// 5. Hub URL and TLS reachability check
	u, err := url.Parse(c.Hub)
	if err != nil {
		record("hub_endpoint", false, "the hub endpoint URL is invalid", err.Error())
	} else {
		record("hub_endpoint", true, "hub endpoint URL is well-formed", u.Host)
	}

	if u != nil && caBytes != nil && len(pair.Certificate) > 0 {
		tc, err := transportsecurity.ClientConfig(u.Hostname(), caBytes)
		if err != nil {
			record("hub_tls_handshake", false, "TLS client configuration failed", err.Error())
		} else {
			tc.MinVersion = tls.VersionTLS13
			tc.Certificates = []tls.Certificate{pair}

			hostPort := u.Host
			if u.Port() == "" {
				hostPort = net.JoinHostPort(u.Hostname(), "443")
			}

			dialer := &net.Dialer{Timeout: 5 * time.Second}
			conn, dialErr := tls.DialWithDialer(dialer, "tcp", hostPort, tc)
			if dialErr != nil {
				record("hub_tls_handshake", false, "cannot establish mutual TLS connection to hub", dialErr.Error())
			} else {
				conn.Close()
				record("hub_tls_handshake", true, "mutual TLS connection established", "")

				// 6. Probes: /health/live and /health/ready
				client := &http.Client{
					Transport: &http.Transport{
						TLSClientConfig:   tc,
						DisableKeepAlives: true,
					},
					Timeout: 5 * time.Second,
				}

				resp, err := client.Get(c.Hub + "/health/live")
				if err != nil || resp.StatusCode != http.StatusNoContent {
					statusStr := ""
					if resp != nil {
						statusStr = fmt.Sprintf("status %d", resp.StatusCode)
						resp.Body.Close()
					}
					record("hub_liveness", false, "hub liveness probe failed", statusStr)
				} else {
					resp.Body.Close()
					record("hub_liveness", true, "hub is live", "")
				}

				respReady, errReady := client.Get(c.Hub + "/health/ready")
				if errReady != nil || respReady.StatusCode != http.StatusNoContent {
					statusStr := ""
					if respReady != nil {
						statusStr = fmt.Sprintf("status %d", respReady.StatusCode)
						respReady.Body.Close()
					}
					record("hub_readiness", false, "hub readiness probe failed", statusStr)
				} else {
					respReady.Body.Close()
					record("hub_readiness", true, "hub is ready", "")
				}
			}
		}
	} else {
		record("hub_tls_handshake", false, "cannot test TLS handshake without valid CA and client credentials", "")
		record("hub_liveness", false, "probe skipped due to TLS prerequisites", "")
		record("hub_readiness", false, "probe skipped due to TLS prerequisites", "")
	}

	// 7. IdP endpoints format check
	idpErr := validateIdP(c.IdP)
	if idpErr != nil {
		record("idp_configuration", false, "IdP configuration is invalid", idpErr.Error())
	} else {
		record("idp_configuration", true, "IdP configuration is valid", c.IdP.Issuer)
	}

	return Prerequisites{
		Passed: allPassed,
		Checks: checks,
	}
}

func readLocalPEM(path string) ([]byte, error) {
	data, err := localPEM.Read(path)
	if errors.Is(err, errLocalPEMIrregular) {
		return nil, fmt.Errorf("must be a regular file: %s", path)
	}
	return data, err
}

// errLocalPEMIrregular is a named PEM file that is not a regular file.
var errLocalPEMIrregular = errors.New("must be a regular file")

// localPEM is how a client configuration's CA or certificate is read, through
// a link at its name and within 1 MiB.
var localPEM = artifactdir.Document{
	MaxBytes: 1 << 20,
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errLocalPEMIrregular,
		Open:      artifactdir.FilesystemReport,
		Read:      artifactdir.FilesystemReport,
		Size:      errors.New("exceeds 1 MiB"),
	},
}
