package runnerprotocol_test

import (
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"testing"
)

func TestAdmissionContractsRefuseUnsupportedAndMissingFields(t *testing.T) {
	good := `{"schema":"readmit-runner-request/v1","environment":"test","instance":"test-session","job":"test-job","engine":"dev","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`
	if _, err := runnerprotocol.DecodeRequest([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `null`, good[:len(good)-1] + `,"extra":true}`, `{"schema":"readmit-runner-request/v1","environment":"test","instance":"test-session","job":"test-job","engine":"dev","spec":"readmit-test/v2","profile":"readmit-siu-v1"}`} {
		if _, err := runnerprotocol.DecodeRequest([]byte(bad)); err == nil {
			t.Fatal("admitted invalid request")
		}
	}
}
