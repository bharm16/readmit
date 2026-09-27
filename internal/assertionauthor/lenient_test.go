package assertionauthor_test

import (
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
)

func TestReadLenientKeepsEveryClauseAndNamesTheUnsupportedOnes(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/assertion-set.json")
	if err != nil {
		t.Fatal(err)
	}
	strict, err := assertion.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	draft, unsupported, err := assertionauthor.ReadLenient(data)
	if err != nil || len(unsupported) != 0 || len(draft.Assertions) != len(strict.Assertions) || draft.Name != strict.Name {
		t.Fatalf("a supported set read leniently: %v, %d unsupported, %d clauses", err, len(unsupported), len(draft.Assertions))
	}

	future := strings.Replace(string(data), `"operator": "field_not_equals"`, `"operator": "field_resembles"`, 1)
	draft, unsupported, err = assertionauthor.ReadLenient([]byte(future))
	if err != nil {
		t.Fatal(err)
	}
	if len(unsupported) != 1 || unsupported[0].ID != "ack-not-rejected" || unsupported[0].Operator != "field_resembles" ||
		unsupported[0].Position != 1 || !strings.Contains(unsupported[0].Raw, "field_resembles") || unsupported[0].Reason == "" {
		t.Fatalf("the unsupported clause: %+v", unsupported)
	}
	if len(draft.Assertions)+len(unsupported) != len(strict.Assertions) {
		t.Fatalf("a clause was dropped: %d supported, %d unsupported of %d", len(draft.Assertions), len(unsupported), len(strict.Assertions))
	}
	if _, err := assertion.Decode([]byte(future)); err == nil {
		t.Fatal("the strict reader accepted an operator it does not evaluate")
	}

	for _, refused := range []string{`{"schema":"readmit-assertion-set/v2","name":"x","assertions":[]}`, `{"schema":"readmit-assertion-set/v1","name":"x"}`, `not json`} {
		if _, _, err := assertionauthor.ReadLenient([]byte(refused)); err == nil {
			t.Errorf("read %s", refused)
		}
	}
}
