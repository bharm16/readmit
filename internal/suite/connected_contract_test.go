package suite_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/suite"
)

func TestConnectedSuiteRequiresExactRevisionBindingsAndKeepsTheLegacyReaderFrozen(t *testing.T) {
	digest := strings.Repeat("a", 64)
	raw := []byte(`{"schema":"readmit-suite/v2","id":"regression","owner":"interop","tags":[],"parallelism":2,"tests":[{"id":"booking","revision":"1","definition":"` + digest + `","release":"release.json","release_identity":"` + digest + `","after":[],"state":"enabled"}],"environments":[{"id":"qa","bindings":[{"test":"booking","plan":"plan","plan_identity":"` + digest + `","config":"config.json"}]}]}`)
	if _, err := suite.Decode(raw); err == nil {
		t.Fatal("the legacy reader accepted connected suite members")
	}
	if _, err := suite.DecodeConnected(raw); err != nil {
		t.Fatal(err)
	}
	for name, changed := range map[string]string{
		"unknown-member":   strings.Replace(string(raw), `"owner":"interop"`, `"owner":"interop","retry":true`, 1),
		"missing-revision": strings.Replace(string(raw), `"revision":"1",`, "", 1),
		"unbound-test":     strings.Replace(string(raw), `"test":"booking"`, `"test":"other"`, 1),
		"null-denominator": strings.Replace(string(raw), `"tests":[`, `"tests":null,"unused":[`, 1),
		"unknown-state":    strings.Replace(string(raw), `"state":"enabled"`, `"state":"passed"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := suite.DecodeConnected([]byte(changed)); err == nil {
				t.Fatal("invalid connected suite was accepted")
			}
		})
	}
}
