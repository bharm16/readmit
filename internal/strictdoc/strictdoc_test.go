package strictdoc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/strictdoc"
)

type sample struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
}

func contract() strictdoc.Document {
	return strictdoc.Document{
		MaxBytes:    1 << 10,
		Schema:      "readmit-sample/v1",
		Required:    []string{"name", "count"},
		Invalid:     "invalid sample JSON",
		TooLarge:    "a sample exceeds its size limit",
		MustDeclare: "a sample must declare readmit-sample/v1",
		Requires:    "a sample requires name and count",
	}
}

func TestDecodeAcceptsACompleteDocument(t *testing.T) {
	var s sample
	err := contract().Decode([]byte(`{"schema":"readmit-sample/v1","name":"one","count":2}`), &s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "one" || s.Count != 2 || s.Schema != "readmit-sample/v1" {
		t.Fatalf("decoded = %+v", s)
	}
}

func TestDecodeRefusesTheFourWaysADocumentCanBetrayItsContract(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"over the size limit", `{"schema":"readmit-sample/v1","name":"` + strings.Repeat("x", 1<<10) + `"}`, "a sample exceeds its size limit"},
		{"not JSON", `not json`, "invalid sample JSON"},
		{"not an object", `[]`, "invalid sample JSON"},
		{"schema missing", `{"name":"one","count":2}`, "a sample must declare readmit-sample/v1"},
		{"schema wrong", `{"schema":"readmit-sample/v2","name":"one","count":2}`, "a sample must declare readmit-sample/v1"},
		{"schema null", `{"schema":null,"name":"one","count":2}`, "a sample must declare readmit-sample/v1"},
		{"required member missing", `{"schema":"readmit-sample/v1","name":"one"}`, "a sample requires name and count"},
		{"required member null", `{"schema":"readmit-sample/v1","name":"one","count":null}`, "a sample requires name and count"},
		{"unknown member", `{"schema":"readmit-sample/v1","name":"one","count":2,"extra":1}`, "invalid sample JSON"},
	}
	var namedUnknown = func() strictdoc.Document {
		d := contract()
		d.Unknown = "a sample declares no member beyond schema, name and count"
		return d
	}()
	if err := namedUnknown.Decode([]byte(`{"schema":"readmit-sample/v1","name":"one","count":2,"extra":1}`), &sample{}); err == nil || err.Error() != namedUnknown.Unknown {
		t.Fatalf("Decode with Unknown = %v, want %q", err, namedUnknown.Unknown)
	}
	if err := namedUnknown.Decode([]byte(`not json`), &sample{}); err == nil || err.Error() != "invalid sample JSON" {
		t.Fatalf("Decode with Unknown still reports Invalid for unparseable bytes: %v", err)
	}
	// A type mismatch is not an unknown member: it earns Invalid even when the
	// contract names the unknown-member refusal its own sentence.
	if err := namedUnknown.Decode([]byte(`{"schema":"readmit-sample/v1","name":"one","count":"many"}`), &sample{}); err == nil || err.Error() != "invalid sample JSON" {
		t.Fatalf("Decode with Unknown reports Invalid for a type mismatch: %v", err)
	}
	for _, c := range cases {
		var s sample
		err := contract().Decode([]byte(c.data), &s)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: Decode = %v, want %q", c.name, err, c.want)
		}
	}
}

// A version mismatch is returned as the caller's own unsupported error, so a
// later version of a contract reads as unsupported — never as invalid — and a
// caller can match it with errors.Is.
func TestDecodeReportsAVersionMismatchAsUnsupported(t *testing.T) {
	unsupported := errors.New("unsupported demo version")
	document := strictdoc.Document{
		MaxBytes:    1024,
		Schema:      "readmit-demo/v1",
		Required:    []string{"name"},
		Invalid:     "invalid demo",
		TooLarge:    "demo too large",
		MustDeclare: "a demo declares its contract version",
		Requires:    "a demo requires a name",
		Unsupported: unsupported,
	}
	if err := document.Decode([]byte(`{"schema":"readmit-demo/v2","name":"x"}`), &struct{}{}); !errors.Is(err, unsupported) {
		t.Fatalf("a later version was not reported as unsupported: %v", err)
	}
	// A document that declares no version at all is not a version mismatch.
	if err := document.Decode([]byte(`{"name":"x"}`), &struct{}{}); err == nil || errors.Is(err, unsupported) {
		t.Fatalf("a missing schema read as a version: %v", err)
	}
	// Without an unsupported error, a mismatch reports MustDeclare.
	plain := document
	plain.Unsupported = nil
	if err := plain.Decode([]byte(`{"schema":"readmit-demo/v2","name":"x"}`), &struct{}{}); err == nil || err.Error() != "a demo declares its contract version" {
		t.Fatalf("a mismatch without a sentinel reported %v", err)
	}
}

// An explicit null names no version at all: it is a missing declaration, never
// a version mismatch, so it must not read as the unsupported error.
func TestDecodeReportsANullSchemaAsUndeclared(t *testing.T) {
	unsupported := errors.New("unsupported demo version")
	document := strictdoc.Document{
		MaxBytes: 1024, Schema: "readmit-demo/v1", Required: []string{"name"},
		Invalid: "invalid demo", TooLarge: "demo too large",
		MustDeclare: "a demo declares its contract version",
		Unsupported: unsupported,
	}
	if err := document.Decode([]byte(`{"schema":null,"name":"x"}`), &sample{}); err == nil || errors.Is(err, unsupported) {
		t.Fatalf("a null schema read as a version mismatch: %v", err)
	}
	if err := document.Decode([]byte(`{"schema":5,"name":"x"}`), &sample{}); err == nil || err.Error() != "invalid demo" {
		t.Fatalf("a non-string schema reported %v", err)
	}
}
