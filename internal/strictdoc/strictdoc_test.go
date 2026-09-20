package strictdoc_test

import (
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
	for _, c := range cases {
		var s sample
		err := contract().Decode([]byte(c.data), &s)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: Decode = %v, want %q", c.name, err, c.want)
		}
	}
}
