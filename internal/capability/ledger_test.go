package capability_test

import (
	"os"
	"testing"

	"github.com/bharm16/readmit/internal/capability"
)

// The shipped ledger is itself read through the strict contract it declares,
// so a row that skips a member, an owner, or its screen/action/test fails
// here before any surface check leans on it.
func TestShippedLedgerIsAValidContractDocument(t *testing.T) {
	data, err := os.ReadFile("../../docs/capability-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := capability.Decode(data)
	if err != nil {
		t.Fatalf("the shipped capability ledger does not read as %s: %v", capability.Schema, err)
	}
	if ledger.Program != "bharm16/readmit#244" {
		t.Fatalf("the ledger does not name the program it covers: %s", ledger.Program)
	}
	if len(ledger.Rows) < 3 {
		t.Fatalf("the ledger covers too little to seed the program: %d rows", len(ledger.Rows))
	}
	for _, kind := range []string{capability.KindCLI, capability.KindDesktop, capability.KindHub} {
		if !hasKind(ledger, kind) {
			t.Errorf("the ledger seeds no %s rows", kind)
		}
	}
}

func hasKind(ledger capability.Ledger, kind string) bool {
	for _, row := range ledger.Rows {
		if row.Kind == kind {
			return true
		}
	}
	return false
}

func TestLedgerRefusals(t *testing.T) {
	valid := `{"schema":"readmit-capability-ledger/v1","program":"bharm16/readmit#244","rows":[` +
		`{"id":"cli.test","source":"readmit test","kind":"cli","capability":"run a test","owner":"bharm16/readmit#257",` +
		`"screen":"s","action":"a","test":"t","implemented":false}]}`
	if _, err := capability.Decode([]byte(valid)); err != nil {
		t.Fatalf("a complete row was refused: %v", err)
	}
	for name, broken := range map[string]string{
		"unknown member":  `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[],"extra":1}`,
		"unknown version": `{"schema":"readmit-capability-ledger/v9","program":"p","rows":[]}`,
		"no declaration":  `{"program":"p","rows":[]}`,
		"no owner":        `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c","screen":"s","action":"a","test":"t","implemented":false}]}`,
		"no screen":       `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"cli","capability":"c","owner":"bharm16/readmit#1","action":"a","test":"t","implemented":false}]}`,
		"two rows one source": `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[` +
			`{"id":"a","source":"readmit test","kind":"cli","capability":"c","owner":"bharm16/readmit#1","screen":"s","action":"a","test":"t","implemented":false},` +
			`{"id":"b","source":"readmit test","kind":"cli","capability":"c2","owner":"bharm16/readmit#2","screen":"s","action":"a","test":"t","implemented":false}]}`,
		"disposed and implemented": `{"schema":"readmit-capability-ledger/v1","program":"p","rows":[{"id":"x","source":"s","kind":"hub","capability":"c","owner":"bharm16/readmit#1","disposition":"not customer work","screen":"also a screen","implemented":false}]}`,
	} {
		if _, err := capability.Decode([]byte(broken)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
