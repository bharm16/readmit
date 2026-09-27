package fhirrest_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestFHIRRESTTLSKeyProvider(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--fhir-key" && i+1 < len(os.Args) {
			raw, e := os.ReadFile(os.Args[i+1])
			if e != nil {
				os.Exit(2)
			}
			os.Stdout.Write(raw)
			os.Exit(0)
		}
	}
}
func TestFHIRRESTMetadataAndWriteUseSeparatelyScopedTLSReferences(t *testing.T) {
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	f := newServer(t)
	handler := f.s.Config.Handler
	f.s.Close()
	f.s = httptest.NewUnstartedServer(handler)
	f.s.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	f.s.StartTLS()
	t.Cleanup(f.s.Close)
	var policy sendpolicy.ScopedPolicy
	if e := json.Unmarshal(f.policy, &policy); e != nil {
		t.Fatal(e)
	}
	_, port, _ := net.SplitHostPort(f.s.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	for i := range policy.Rules {
		policy.Rules[i].Port = number
	}
	f.policy, _ = json.Marshal(policy)
	key, e := x509.MarshalPKCS8PrivateKey(f.s.TLS.Certificates[0].PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "test-provider-key")
	os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600)
	executable, _ := os.Executable()
	spec := f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`))
	spec.HTTP.HTTP.Certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.s.Certificate().Raw})
	spec.HTTP.HTTP.PrivateKey = &networkaction.Credential{Endpoint: f.s.Listener.Addr().String(), Purpose: sendpolicy.FHIRAction, Generation: "1", Locator: networkaction.Provider{Command: executable, Arguments: []string{"-test.run=^TestFHIRRESTTLSKeyProvider$", "--", "--fhir-key", file}}}
	raw, _ := json.Marshal(spec)
	if _, e := fhirrest.Prepare(raw, f.policy); e == nil {
		t.Fatal("metadata silently widened action key purpose")
	}
	metadata := spec.HTTP
	metadata.HTTP.Method = "GET"
	metadata.HTTP.URL = spec.Base + "/metadata"
	metadata.HTTP.Operation = sendpolicy.FHIRMetadata
	metadata.HTTP.Body = nil
	metadata.HTTP.ContentType = ""
	keyReference := *metadata.HTTP.PrivateKey
	keyReference.Purpose = sendpolicy.FHIRMetadata
	metadata.HTTP.PrivateKey = &keyReference
	spec.CapabilityHTTP = &metadata
	raw, _ = json.Marshal(spec)
	plan, e := fhirrest.Prepare(raw, f.policy)
	if e != nil {
		t.Fatal(e)
	}
	var reads atomic.Int32
	ctx := secret.ObserveDeclaredPrograms(context.Background(), func() func() { reads.Add(1); return func() {} })
	dir := filepath.Join(t.TempDir(), "result")
	r, e := plan.Execute(ctx, admission(plan), nil, dir, nil)
	if e != nil || r.State != "succeeded" || reads.Load() != 2 || f.creates != 1 {
		t.Fatal("scoped TLS execution", r.State, reads.Load(), e)
	}
	if _, e := fhirrest.Open(context.Background(), dir); e != nil {
		t.Fatal(e)
	}
	if reads.Load() != 2 {
		t.Fatal("offline read resolved TLS credentials")
	}
}
