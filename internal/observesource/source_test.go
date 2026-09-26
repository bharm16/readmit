package observesource_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
)

func TestADeclaredSourceIsReadExactlyAsWritten(t *testing.T) {
	source, err := observesource.DecodeSource([]byte(declaredFileSource))
	if err != nil {
		t.Fatal(err)
	}
	if source.Observes.Kind != observesource.FileExport || source.Observes.Scope != "appointments" || !source.Enabled {
		t.Fatalf("decoded %+v", source.Observes)
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
		{"a source kind declared with a transport it does not name", strings.Replace(declaredFileSource, `"kind": "file-export"`, `"kind": "downstream-capture"`, 1)},
		{"a source kind no collector here reaches", strings.Replace(declaredFileSource, `"kind": "file-export"`, `"kind": "message-queue"`, 1)},
		{"a capture with no declared scope", strings.Replace(declaredCaptureSource, `"kinds": ["message"]`, `"kinds": []`, 1)},
		{"a capture scoping an occurrence nobody could parse", strings.Replace(declaredCaptureSource, `"kinds": ["message"]`, `"kinds": ["unparsed"]`, 1)},
		{"a capture naming one occurrence kind twice", strings.Replace(declaredCaptureSource, `"kinds": ["message"]`, `"kinds": ["message", "message"]`, 1)},
		{"a capture record key that is not a field selector", strings.Replace(declaredCaptureSource, `"record_key": "SCH-1.1"`, `"record_key": "the appointment id"`, 1)},
		{"a capture read bound of nothing", strings.Replace(declaredCaptureSource, `"max_occurrences": 100`, `"max_occurrences": 0`, 1)},
		{"a capture read bound past the occurrences a case holds", strings.Replace(declaredCaptureSource, `"max_occurrences": 100`, `"max_occurrences": 10001`, 1)},
		{"a capture declaring an envelope it reads nothing through", strings.Replace(declaredCaptureSource, `"extraction": null`, `"extraction": {`+csvExtraction+`}`, 1)},
		{"a capture that is not declared at all", strings.Replace(declaredCaptureSource, `"capture": {"path": "downstream.case", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}`, `"capture": null`, 1)},
		{"a v2 document with no capture member", strings.Replace(declaredFileSource, observesource.SchemaV1, observesource.Schema, 1)},
		{"a document with no extraction member at all", strings.Replace(declaredCaptureSource, `"extraction": null,`, "", 1)},
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
	for _, document := range []string{
		strings.Replace(declaredFileSource, observesource.SchemaV1, "readmit-observation-source/v99", 1),
		strings.Replace(declaredCaptureSource, observesource.Schema, "readmit-observation-source/v99", 1),
	} {
		_, err := observesource.DecodeSource([]byte(document))
		if !errors.Is(err, observesource.ErrUnsupportedVersion) {
			t.Fatalf("error = %v, want an unsupported version", err)
		}
	}
}

// The capture transport is a new version string, not a member added to the one
// that shipped. A v1 document means exactly what it always meant, and one
// carrying the v2 transport is refused rather than read as though v1 had
// always allowed it.
func TestTheEarlierContractVersionKeepsItsMeaningAndRefusesTheLaterTransport(t *testing.T) {
	source, err := observesource.DecodeSource([]byte(declaredFileSource))
	if err != nil {
		t.Fatal(err)
	}
	if source.Schema != observesource.SchemaV1 || source.Capture != nil || source.Extraction == nil {
		t.Fatalf("a v1 document was not read as the document it is: %+v", source)
	}
	widened := strings.Replace(declaredFileSource, `"http": null`,
		`"http": null, "capture": {"path": "downstream.case", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}`, 1)
	if _, err := observesource.DecodeSource([]byte(widened)); err == nil {
		t.Fatal("a v1 document carrying the v2 capture transport was accepted")
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

// The extraction declaration reuses the envelope half of a mapping recipe
// rather than restating it, which is what keeps one reading of what a record
// is. That reuse has a cost this test pins: a member added to one of those
// types would silently become a member of readmit-observation-source/v1, which
// the version rule forbids. When this fails, the answer is a new version string
// here, never an accepted extra member on this one.
func TestTheExtractionContractIsPinnedToTheMembersItReuses(t *testing.T) {
	for _, test := range []struct {
		name    string
		shape   any
		members []string
	}{
		{"extraction", observesource.Extraction{}, []string{"envelope", "encoding", "csv", "text", "json", "xml", "record_key"}},
		{"csv dialect", importer.CSVDialect{}, []string{"delimiter", "record_separator", "header", "fields"}},
		{"text dialect", importer.TextDialect{}, []string{"field_separator", "record_separator", "fields"}},
		{"document dialect", importer.DocumentDialect{}, []string{"record_path"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			shape := reflect.TypeOf(test.shape)
			var members []string
			for index := range shape.NumField() {
				name, _, _ := strings.Cut(shape.Field(index).Tag.Get("json"), ",")
				members = append(members, name)
			}
			if !slices.Equal(members, test.members) {
				t.Fatalf("members are %v, want %v; a change here is a new contract version", members, test.members)
			}
		})
	}
}

// A new source is written under the first version that declares its kind's
// transport, and a declared one keeps its version while it observes the same
// kind, so writing it again never moves it to another contract.
func TestVersionForIsTheFirstVersionDeclaringTheTransport(t *testing.T) {
	for kind, want := range map[string]string{
		observesource.FileExport:        observesource.SchemaV1,
		observesource.HTTPAPI:           observesource.SchemaV1,
		observesource.DownstreamCapture: observesource.Schema,
		observesource.DatabaseQuery:     observesource.SchemaDatabase,
	} {
		if got := observesource.VersionFor(kind, nil); got != want {
			t.Errorf("a new %s source is written under %s, not %s", kind, got, want)
		}
	}
	declared := observesource.Source{Schema: observesource.Schema}
	declared.Observes.Kind = observesource.FileExport
	if got := observesource.VersionFor(observesource.FileExport, &declared); got != observesource.Schema {
		t.Errorf("a declared v2 file export is written under %s", got)
	}
	if got := observesource.VersionFor(observesource.HTTPAPI, &declared); got != observesource.SchemaV1 {
		t.Errorf("a v2 file export changed to an http api is written under %s", got)
	}
}
