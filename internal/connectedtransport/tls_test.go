package connectedtransport_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json/v2"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type testCertificate struct {
	cert        *x509.Certificate
	key         *ecdsa.PrivateKey
	pem, keyPEM []byte
	tls         tls.Certificate
}

func certificate(t *testing.T, parent *testCertificate, ca, expired bool, usage x509.ExtKeyUsage) testCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Synthetic lab"}, DNSNames: []string{"receiver.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: ca, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	if ca {
		template.KeyUsage |= x509.KeyUsageCertSign
		template.ExtKeyUsage = nil
	}
	if expired {
		template.NotAfter = time.Now().Add(-time.Hour)
	}
	issuer, signing := template, key
	if parent != nil {
		issuer, signing = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, key.Public(), signing)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return testCertificate{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), tls: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}}
}
func TestConnectedTransportTLSVerificationAndPurposeBoundClientKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX synthetic secret-provider fixture")
	}
	for _, name := range []string{"tls", "mtls", "tls-ip", "mtls-ip", "name-mismatch", "expired-ca", "expired-client"} {
		t.Run(name, func(t *testing.T) {
			ca := certificate(t, nil, true, name == "expired-ca", x509.ExtKeyUsageServerAuth)
			serverCert := certificate(t, &ca, false, false, x509.ExtKeyUsageServerAuth)
			config := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverCert.tls}}
			mutual := name == "mtls" || name == "mtls-ip" || name == "expired-client"
			if mutual {
				config.ClientAuth = tls.RequireAndVerifyClientCert
				config.ClientCAs = x509.NewCertPool()
				config.ClientCAs.AppendCertsFromPEM(ca.pem)
			}
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			count := serve(t, tls.NewListener(l, config), false)
			var providerMarker string
			p, selected, dir := prepared(t, l.Addr().String(), func(dir string, target *replay.Target, s *connectedtransport.Selection) {
				target.Transport = "tls"
				target.ServerName = "receiver.test"
				if name == "tls-ip" || name == "mtls-ip" {
					target.ServerName = "127.0.0.1"
				}
				if name == "name-mismatch" {
					target.ServerName = "wrong.test"
				}
				target.CAFile = filepath.Join(dir, "ca.pem")
				os.WriteFile(target.CAFile, ca.pem, 0600)
				if mutual {
					client := certificate(t, &ca, false, name == "expired-client", x509.ExtKeyUsageClientAuth)
					target.ClientCertificate = filepath.Join(dir, "client.pem")
					os.WriteFile(target.ClientCertificate, client.pem, 0600)
					key := filepath.Join(dir, "private.pem")
					os.WriteFile(key, client.keyPEM, 0600)
					provider := filepath.Join(dir, "provider")
					providerMarker = filepath.Join(dir, "provider-used")
					os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\ncat \"$2\"\n"), 0700)
					target.Credential = replay.Credential{SecretsFile: filepath.Join(dir, "secrets.json"), Reference: "client-key"}
					if err := secret.WriteStore(target.Credential.SecretsFile, secret.Document{Schema: secret.Schema, References: []secret.Reference{{Name: "client-key", Store: secret.CustomerManaged, Purpose: secret.MLLPEndpoint, Address: target.Address, Command: provider, Arguments: []string{providerMarker, key}, Generation: 1, RotatedAt: time.Now()}}}); err != nil {
						t.Fatal(err)
					}
					s.Credential = filepath.Join(dir, "credential.json")
					write(t, s.Credential, connectedtransport.Credential{Schema: connectedtransport.CredentialSchema, Project: "lab", Environment: "test", Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Address: target.Address, Generation: 1, Reference: "client-key"})
				}
			})
			if mutual {
				if _, err := os.Stat(providerMarker); !os.IsNotExist(err) {
					t.Fatal("offline preparation resolved private key")
				}
			}
			a := grant(t, p, dir)
			if mutual {
				bad := a
				bad.Generation = "stale"
				if _, err := connectedtransport.Execute(context.Background(), p, bad, "denied", filepath.Join(dir, "denied"), nil); err == nil {
					t.Fatal("stale grant")
				}
				if _, err := os.Stat(providerMarker); !os.IsNotExist(err) {
					t.Fatal("secret before authorization")
				}
			}
			r, err := connectedtransport.Execute(context.Background(), p, a, "run", filepath.Join(dir, "result"), nil)
			if err != nil {
				t.Fatal(err)
			}
			success := name == "tls" || name == "mtls" || name == "tls-ip" || name == "mtls-ip"
			if success && (r.State != "settled" || count.Load() != 2) {
				t.Fatalf("TLS failed: %+v received %d", r, count.Load())
			}
			if name == "mtls" {
				plan, err := connectedtest.OpenPlan(filepath.Join(dir, "result", "plan"))
				if err != nil {
					t.Fatal(err)
				}
				markerBefore, _ := os.ReadFile(providerMarker)
				raw, _ := os.ReadFile(selected.Credential)
				var credential connectedtransport.Credential
				json.Unmarshal(raw, &credential)
				credential.Operation = sendpolicy.ObservationRead
				write(t, selected.Credential, credential)
				if _, err := connectedtransport.Prepare(plan, selected); err == nil {
					t.Fatal("read-only secret scope allowed stimulus")
				}
				os.WriteFile(selected.Credential, raw, 0600)
				target, err := replay.ReadTarget(selected.Target)
				if err != nil {
					t.Fatal(err)
				}
				store, err := secret.ReadStore(target.Credential.SecretsFile)
				if err != nil {
					t.Fatal(err)
				}
				store.References[0].Purpose = secret.SourceEndpoint
				write(t, target.Credential.SecretsFile, store)
				if _, err := connectedtransport.Prepare(plan, selected); err == nil {
					t.Fatal("source credential allowed stimulus")
				}
				markerAfter, _ := os.ReadFile(providerMarker)
				if !bytes.Equal(markerBefore, markerAfter) {
					t.Fatal("invalid scope resolved a secret")
				}
			}
			if !success && (r.State == "settled" || count.Load() != 0) {
				t.Fatalf("TLS verification bypassed: %+v received %d", r, count.Load())
			}
		})
	}
}
