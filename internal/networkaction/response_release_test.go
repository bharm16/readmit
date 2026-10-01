package networkaction_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const responseFixtureCredential = "private-fixture-token+\">>>"

// The provider is an actual separately invoked program on every platform.
func TestResponseFixtureCredentialProvider(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "response-fixture-provider" {
		fmt.Print(responseFixtureCredential)
		os.Exit(0)
	}
}

type responseFixtureProvider struct{}

func (responseFixtureProvider) Identity() string {
	return networkaction.Digest([]byte("response-fixture"))
}
func (responseFixtureProvider) Material(context.Context, networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	return networkaction.BearerMaterial([]byte(responseFixtureCredential)), nil
}

func TestHTTPAdaptersWithholdCredentialEchoesFromReturnedAndRetainedEvidence(t *testing.T) {
	for _, row := range []struct {
		name, echo string
	}{
		{"raw", responseFixtureCredential},
		{"json", `private-fixture-token+\">>>`},
		{"base64", "cHJpdmF0ZS1maXh0dXJlLXRva2VuKyI+Pj4="},
		{"base64-unpadded", "cHJpdmF0ZS1maXh0dXJlLXRva2VuKyI+Pj4"},
		{"base64url", "cHJpdmF0ZS1maXh0dXJlLXRva2VuKyI-Pj4="},
		{"base64url-unpadded", "cHJpdmF0ZS1maXh0dXJlLXRva2VuKyI-Pj4"},
	} {
		for _, location := range []string{"body", "header"} {
			t.Run(row.name+"-"+location, func(t *testing.T) {
				exerciseResponseRelease(t, row.echo, location, true)
			})
		}
	}
	t.Run("clean", func(t *testing.T) { exerciseResponseRelease(t, "independent-synthetic-response", "body", false) })
}

func exerciseResponseRelease(t *testing.T, echo, location string, withheld bool) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+responseFixtureCredential {
			t.Error("fixture credential was not presented to the admitted target")
		}
		if location == "header" {
			w.Header().Set("Content-Type", "text/"+echo)
			fmt.Fprint(w, "independent-synthetic-response")
		} else {
			fmt.Fprint(w, echo)
		}
	}))
	defer server.Close()
	spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.ObservationRead, Method: "GET", URL: server.URL + "/export", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), TimeoutMS: 5000, MaxBytes: 4096}
	policyBytes := policy(t, server.Listener.Addr().String(), sendpolicy.ObservationRead)
	spec.Credential = &networkaction.Credential{Endpoint: server.Listener.Addr().String(), Purpose: sendpolicy.ObservationRead, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: os.Args[0], Arguments: []string{"-test.run=^TestResponseFixtureCredentialProvider$", "--", "response-fixture-provider"}}}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := networkaction.PrepareHTTP(raw, policyBytes)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "response")
	response, result, err := direct.Execute(t.Context(), approved(direct.Binding()), output, nil)
	if withheld {
		if err == nil || len(response.Body.Expose()) != 0 || result.ResponseRetained {
			t.Fatal("direct HTTP released a credential echo")
		}
		if _, err := os.Stat(filepath.Join(output, "response.bin")); !os.IsNotExist(err) {
			t.Fatal("credential echo was retained")
		}
	} else if err != nil || string(response.Body.Expose()) != echo || !result.ResponseRetained {
		t.Fatal("clean direct response was withheld", err)
	}
	if opened, err := networkaction.OpenHTTP(output); err != nil || opened.State != "responded" {
		t.Fatal("the actual response status was not retained independently of its withheld body", err)
	}
	spec.Credential = nil
	provider := responseFixtureProvider{}
	raw, err = json.Marshal(networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Authorization: provider.Identity(), HTTP: spec})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := networkaction.PrepareRuntimeHTTP(raw, policyBytes)
	if err != nil {
		t.Fatal(err)
	}
	response, receipt, err := runtime.Execute(t.Context(), approved(runtime.Binding()), nil, provider)
	if withheld {
		if err == nil || len(response.Body.Expose()) != 0 {
			t.Fatal("runtime HTTP released a credential echo")
		}
	} else if err != nil || string(response.Body.Expose()) != echo {
		t.Fatal("clean runtime response was withheld", err)
	}
	if receipt.State != "responded" {
		t.Fatal("runtime response status lost")
	}
}
