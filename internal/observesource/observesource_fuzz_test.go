package observesource_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observesource"
)

// FuzzObservationSourceDocument exercises the declared-source reader: the
// strict decode that refuses unknown members and every bound a declaration is
// held to, across both contract versions it reads. No input may panic, and a
// source the reader accepts must be one a collection could actually be
// attempted against — in particular one that names exactly one transport,
// bounds what a read may take, and never carries a credential value or an
// endpoint readmit would reach in the clear.
func FuzzObservationSourceDocument(f *testing.F) {
	endpoint := strings.Replace(declaredHTTPSource, "ENDPOINT", "https://lab.example.invalid:8443/appointments", 1)
	for _, seed := range []string{
		declaredFileSource,
		databaseSource,
		endpoint,
		strings.Replace(endpoint, `"credential": null`,
			`"credential": {"store": "os-keychain", "address": "lab.example.invalid:8443", "header": "Authorization", "command": "/usr/bin/true", "arguments": ["-s", "lab"]}`, 1),
		strings.Replace(declaredFileSource, `"envelope": "csv"`, `"envelope": "text"`, 1),
		strings.Replace(declaredFileSource, `"enabled": true`, `"enabled": false`, 1),
		strings.Replace(declaredFileSource, "source/v1", "source/v2", 1),
		declaredCaptureSource,
		strings.Replace(declaredCaptureSource, `"kinds": ["message"]`, `"kinds": ["message", "ack"]`, 1),
		strings.Replace(declaredCaptureSource, "source/v2", "source/v1", 1),
		`{}`,
		`{"schema":"readmit-observation-source/v1"}`,
		`{"schema":"readmit-observation-source/v2"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		source, err := observesource.DecodeSource(data)
		if err != nil {
			return
		}
		declared := 0
		for _, present := range []bool{source.File != nil, source.HTTP != nil, source.Capture != nil, source.Database != nil} {
			if present {
				declared++
			}
		}
		if declared != 1 {
			t.Fatalf("accepted a source declaring %d transports", declared)
		}
		// A capture is declared only by the version that has it, reads no
		// envelope, and bounds what one read may take by occurrences the case
		// contract itself can hold.
		if source.Database != nil {
			if source.Schema != observesource.SchemaDatabase || source.Observes.Kind != observesource.DatabaseQuery || source.Extraction != nil {
				t.Fatal("database outside declared version/kind")
			}
			return
		}
		if source.Capture != nil {
			if (source.Schema != observesource.Schema && source.Schema != observesource.SchemaDatabase) || source.Observes.Kind != observesource.DownstreamCapture || source.Extraction != nil {
				t.Fatalf("accepted a capture outside the version and kind that declare it: %+v", source)
			}
			if source.Capture.MaxOccurrences < 1 || source.Capture.MaxOccurrences > 10000 || len(source.Capture.Kinds) == 0 {
				t.Fatalf("accepted an unbounded or unscoped capture: %+v", source.Capture)
			}
			if _, err := source.Capture.Selector(); err != nil {
				t.Fatalf("accepted a capture record key that is not a field selector: %v", err)
			}
			return
		}
		if source.Extraction == nil {
			t.Fatalf("accepted a document-reading transport with no envelope: %+v", source)
		}
		if source.File != nil {
			if source.Observes.Kind != observesource.FileExport || source.File.MaxBytes < 1 || source.File.MaxBytes > observesource.MaxReadBytes {
				t.Fatalf("accepted an unbounded file export: %+v", source.File)
			}
			return
		}
		if source.Observes.Kind != observesource.HTTPAPI {
			t.Fatalf("accepted an http transport under the kind %q", source.Observes.Kind)
		}
		if source.HTTP.MaxBytes < 1 || source.HTTP.MaxBytes > observesource.MaxReadBytes {
			t.Fatalf("accepted an unbounded http read: %+v", source.HTTP)
		}
		// An accepted endpoint names one host and numeric port readmit can
		// decide against a policy, reached over TLS and never in the clear.
		if _, err := source.HTTP.Endpoint(); err != nil || !strings.HasPrefix(source.HTTP.URL, "https://") {
			t.Fatalf("accepted an endpoint readmit cannot reach safely: %q, %v", source.HTTP.URL, err)
		}
		// A credential is bound to this endpoint before any value is read, and
		// binding never reads one.
		locator, header, err := source.HTTP.BindCredential()
		if err != nil {
			return
		}
		if (header == "") != (locator.Command == "") {
			t.Fatalf("a bound credential names a header and a locator together: %q, %q", header, locator.Command)
		}
	})
}
