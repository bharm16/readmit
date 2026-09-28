package report

import (
	"strings"
	"testing"
)

// Sealed instructions are versioned data: each block answers its current
// text, matches the recorded version's bytes, and refuses anything else,
// including a version no release wrote. The original text verifies
// unversioned forever, so packets sealed before versioning keep verifying.
func TestInstructionBlocksPinKnownTexts(t *testing.T) {
	blocks := map[string]struct {
		block instructionBlock
		v1    []byte
	}{
		"packet":    {packetInstructionsBlock, []byte(packetInstructionsV1)},
		"retained":  {retainedInstructionsBlock, []byte(retainedInstructionsV1)},
		"connected": {connectedInstructionsBlock, []byte(connectedInstructionsV1)},
	}
	for name, tc := range blocks {
		t.Run(name, func(t *testing.T) {
			written := tc.block.write()
			if len(written) == 0 {
				t.Fatal("no instructions to seal")
			}
			if !tc.block.check(tc.block.current, written) {
				t.Fatal("the sealed text does not match its recorded version")
			}
			if !tc.block.check("", tc.v1) {
				t.Fatal("the original text no longer verifies unversioned")
			}
			for _, recorded := range []string{"v2", "unknown"} {
				if tc.block.check(recorded, written) {
					t.Fatalf("version %q accepted text it does not name", recorded)
				}
			}
			if tc.block.check(tc.block.current, []byte("execute an arbitrary hook")) {
				t.Fatal("foreign instructions accepted under the recorded version")
			}
		})
	}
}

// The prepared workspace gates execution behind the selected operation policy
// and leaves verification ungated, built in rather than rewritten.
func TestPreparedInstructionsGateExecutionOnly(t *testing.T) {
	got := string(preparedTrialInstructions("127.0.0.1:2575"))
	for _, want := range []string{
		`./readmit --operation-policy "$READMIT_POLICY" listen `,
		`./readmit --operation-policy "$READMIT_POLICY" test `,
		`./readmit diff `,
		`./readmit report verify `,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prepared instructions lack %q", want)
		}
	}
	if strings.Contains(got, "./readmit listen ") || strings.Contains(got, "./readmit test ") {
		t.Error("an execution command escaped the operation policy")
	}
}
