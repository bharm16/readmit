package suite_test

import (
	"github.com/bharm16/readmit/internal/suite"
	"strings"
	"testing"
)

const document = `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":2,"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"east.json"}]}],"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`

func TestDecodeRefusesUnknownMissingAndAmbiguousSuiteDeclarations(t *testing.T) {
	if _, err := suite.Decode([]byte(document)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(document, `"isolation":"shared",`, "", 1),
		strings.Replace(document, `"isolation":"shared"`, `"isolation":null`, 1),
		strings.Replace(document, `"target":"east.json"`, `"target":"east.json","password":"no"`, 1),
		strings.Replace(document, `"case":"case-one"`, `"case":"case-one","expected":{"missing":{"count":null}}`, 1),
		strings.Replace(document, `"table":"patients"`, `"table":"absent"`, 1),
		strings.Replace(document, `"sequence":["s0001-e000001"]`, `"sequence":["s0001-e000001"],"after":["booking"]`, 1),
		strings.Replace(document, `"tags":["siu"]`, `"tags":["siu","siu"]`, 1),
		strings.Replace(document, `"spec":"booking.json"`, `"spec":"../booking.json"`, 1),
		strings.Replace(document, `"schema":"readmit-suite/v1"`, `"schema":"readmit-suite/v1","disabled":true`, 1),
	} {
		if _, err := suite.Decode([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid document: %s", raw)
		}
	}
}

func FuzzDecodeSuite(f *testing.F) {
	f.Add([]byte(document))
	f.Add([]byte(`{"schema":"readmit-suite/v1"}`))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = suite.Decode(raw) })
}
func TestSuiteSelectionIsStrict(t *testing.T) {
	for _, raw := range []string{`{"schema":"readmit-suite-selection/v1","suite":"nightly","environment":"east","site":"a","unknown":true}`, `{"schema":"readmit-suite-selection/v1","suite":"nightly","environment":"east","site":null}`, `{"schema":"readmit-suite-selection/v2","suite":"nightly","environment":"east","site":"a"}`} {
		if _, e := suite.DecodeSelection([]byte(raw)); e == nil {
			t.Fatal("accepted invalid selection")
		}
	}
}
