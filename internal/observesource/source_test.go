package observesource_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observesource"
)

func TestADeclaredSourceIsReadExactlyAsWritten(t *testing.T) {
	source, err := observesource.DecodeSource([]byte(declaredFileSource))
	if err != nil {
		t.Fatal(err)
	}
	if source.Of.Kind != observesource.FileExport || source.Of.Scope != "appointments" || !source.Enabled {
		t.Fatalf("decoded %+v", source.Of)
	}
	if source.File == nil || source.File.MaxBytes != 65536 || source.HTTP != nil {
		t.Fatal("a file-export source declares exactly one file export")
	}
	endpoint, err := observesource.DecodeSource([]byte(strings.Replace(declaredHTTPSource, "ENDPOINT", "https://lab.example.invalid:8443/appointments", 1)))
	if err != nil {
		t.Fatal(err)
	}
	address, err := endpoint.HTTP.Endpoint()
	if err != nil || address != "lab.example.invalid:8443" {
		t.Fatalf("endpoint = %q, %v", address, err)
	}
}

func TestAnHTTPSEndpointWithoutAPortIsReadAsTheOneHTTPSImplies(t *testing.T) {
	source, err := observesource.DecodeSource([]byte(strings.Replace(declaredHTTPSource, "ENDPOINT", "https://lab.example.invalid/appointments", 1)))
	if err != nil {
		t.Fatal(err)
	}
	address, err := source.HTTP.Endpoint()
	if err != nil || address != "lab.example.invalid:443" {
		t.Fatalf("endpoint = %q, %v", address, err)
	}
}

func TestAnUnreadableDeclarationIsRefusedRatherThanRepaired(t *testing.T) {
	endpoint := strings.Replace(declaredHTTPSource, "ENDPOINT", "https://lab.example.invalid:8443/appointments", 1)
	for _, test := range []struct {
		name     string
		document string
	}{
		{"an unknown member", strings.Replace(declaredFileSource, `"enabled": true`, `"enabled": true, "observe_everything": true`, 1)},
		{"a member that is not declared at all", strings.Replace(declaredFileSource, `"enabled": true,`, "", 1)},
		{"a transport that is not declared as an explicit null", strings.Replace(declaredFileSource, `"http": null`, `"nothing": null`, 1)},
		{"both transports declared", strings.Replace(declaredFileSource, `"http": null`, `"http": {"url": "https://lab.example.invalid:8443/a", "classification": "nonproduction", "ca_file": "", "server_name": "", "timeout": "1s", "max_bytes": 16, "retry": {"attempts": 0, "delay": "0s"}, "credential": null}`, 1)},
		{"a source kind no collector here reaches", strings.Replace(declaredFileSource, `"kind": "file-export"`, `"kind": "downstream-capture"`, 1)},
		{"a transport the source kind does not name", strings.Replace(declaredFileSource, `"kind": "file-export"`, `"kind": "http-api"`, 1)},
		{"a freshness bound that is not a duration", strings.Replace(declaredFileSource, `"max_age": "1h"`, `"max_age": "soon"`, 1)},
		{"a freshness bound of no time at all", strings.Replace(declaredFileSource, `"max_age": "1h"`, `"max_age": "0s"`, 1)},
		{"a freshness bound past the declared ceiling", strings.Replace(declaredFileSource, `"max_age": "1h"`, `"max_age": "200h"`, 1)},
		{"a read bound of nothing", strings.Replace(declaredFileSource, `"max_bytes": 65536`, `"max_bytes": 0`, 1)},
		{"a read bound past the per-source evidence bound", strings.Replace(declaredFileSource, `"max_bytes": 65536`, `"max_bytes": 268435456`, 1)},
		{"two dialects for one envelope", strings.Replace(declaredFileSource, `"record_key"`, `"text": {"field_separator": "|", "record_separator": "lf", "fields": 2}, "record_key"`, 1)},
		{"a record key past the declared field count", strings.Replace(declaredFileSource, `"header": "present"`, `"header": "absent"`, 1)},
		{"a plaintext endpoint", strings.Replace(endpoint, "https://lab.example.invalid:8443", "http://lab.example.invalid:8443", 1)},
		{"an endpoint carrying a credential in its URL", strings.Replace(endpoint, "https://lab.example.invalid:8443", "https://user:secret@lab.example.invalid:8443", 1)},
		{"an endpoint carrying a fragment", strings.Replace(endpoint, "/appointments", "/appointments#part", 1)},
		{"an endpoint with no recorded class", strings.Replace(endpoint, `"classification": "nonproduction"`, `"classification": ""`, 1)},
		{"a timeout past the declared ceiling", strings.Replace(endpoint, `"timeout": "3s"`, `"timeout": "10m"`, 1)},
		{"more retries than this release makes", strings.Replace(endpoint, `"attempts": 0`, `"attempts": 9`, 1)},
		{"a retry delay past the declared ceiling", strings.Replace(endpoint, `"delay": "5ms"`, `"delay": "30s"`, 1)},
		{"a credential that is not declared as an explicit null", strings.Replace(endpoint, `"credential": null`, `"authentication": null`, 1)},
		{"a credential with no declared store", strings.Replace(endpoint, `"credential": null`, `"credential": {"store": "somewhere", "address": "lab.example.invalid:8443", "header": "Authorization", "command": "/usr/bin/true", "arguments": []}`, 1)},
		{"a credential read through a program resolved on PATH", strings.Replace(endpoint, `"credential": null`, `"credential": {"store": "os-keychain", "address": "lab.example.invalid:8443", "header": "Authorization", "command": "security", "arguments": []}`, 1)},
		{"a credential presented in a header that is not one", strings.Replace(endpoint, `"credential": null`, `"credential": {"store": "os-keychain", "address": "lab.example.invalid:8443", "header": "Authorization: x\r\nX-Other", "command": "/usr/bin/true", "arguments": []}`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := observesource.DecodeSource([]byte(test.document)); err == nil {
				t.Fatal("the declaration was accepted")
			}
		})
	}
}

func TestAContractVersionThisReleaseDoesNotReadIsReportedAsThat(t *testing.T) {
	later := strings.Replace(declaredFileSource, observesource.Schema, "readmit-observation-source/v2", 1)
	_, err := observesource.DecodeSource([]byte(later))
	if !errors.Is(err, observesource.ErrUnsupportedVersion) {
		t.Fatalf("error = %v, want an unsupported version", err)
	}
}

func TestADocumentPastItsSizeLimitIsRefusedRatherThanTruncated(t *testing.T) {
	oversized := strings.Replace(declaredFileSource, `"path": "export.csv"`,
		`"path": "`+strings.Repeat("e", observesource.MaxSourceBytes)+`"`, 1)
	if _, err := observesource.DecodeSource([]byte(oversized)); err == nil {
		t.Fatal("an oversized document was accepted")
	}
}

func TestACredentialReferenceNeverCarriesAValue(t *testing.T) {
	endpoint := strings.Replace(declaredHTTPSource, "ENDPOINT", "https://lab.example.invalid:8443/appointments", 1)
	document := strings.Replace(endpoint, `"credential": null`,
		`"credential": {"store": "os-keychain", "address": "lab.example.invalid:8443", "header": "Authorization", "command": "/usr/bin/true", "arguments": ["-s", "lab"], "value": "nope"}`, 1)
	if _, err := observesource.DecodeSource([]byte(document)); err == nil {
		t.Fatal("a credential value was accepted in a reference")
	}
	scoped := strings.Replace(endpoint, `"credential": null`,
		`"credential": {"store": "os-keychain", "address": "elsewhere.example.invalid:8443", "header": "Authorization", "command": "/usr/bin/true", "arguments": []}`, 1)
	source, err := observesource.DecodeSource([]byte(scoped))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.HTTP.BindCredential(); err == nil {
		t.Fatal("a credential scoped to another endpoint was bound to this one")
	}
}

func TestAnObservationSourceIsSelectedExplicitlyAsARegularFile(t *testing.T) {
	directory := t.TempDir()
	if _, err := observesource.ReadSource(directory); err == nil {
		t.Fatal("a directory was read as an observation source")
	}
	path := write(t, directory, "source.json", declaredFileSource)
	source, err := observesource.ReadSource(path)
	if err != nil {
		t.Fatal(err)
	}
	// The export is resolved against the document's own directory, never the
	// caller's working directory. The directory is the physical one the
	// document was read from, so a path reached through a symbolic link names
	// what the filesystem names.
	if !filepath.IsAbs(source.File.Path) || filepath.Base(source.File.Path) != "export.csv" {
		t.Fatalf("export resolved to %q", source.File.Path)
	}
	if _, err := os.Stat(source.File.Path); err == nil {
		t.Fatal("this test writes no export, so the resolved path should not exist")
	}
}
