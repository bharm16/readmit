package receiver

import (
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/observation"
)

// VerifyLedger checks successful built-in fixture testimony against the exact
// requests, starting with a fresh session. It performs no I/O and does not
// establish authenticity. Independent fixture tests remain the behavior oracle;
// this check only prevents retained evidence from inventing a different ledger.
func VerifyLedger(requests [][]byte, initial, final observation.Snapshot) error {
	invalid := errors.New("observation is not the built-in fixture ledger for these requests")
	if initial.Validate() != nil || final.Validate() != nil || !initial.Consistent || !final.Consistent || initial.SessionID != final.SessionID || initial.Mode != final.Mode || len(initial.Records) != 0 || len(initial.Processed) != 0 || len(requests) == 0 || len(requests) != len(final.Processed) {
		return invalid
	}
	profile, err := decodeProfile(fixtureProfileJSON)
	if err != nil {
		return err
	}
	r := &Receiver{config: Config{Mode: final.Mode}, profile: profile}
	expected := observation.Snapshot{Records: []observation.Record{}}
	for _, raw := range requests {
		request, err := r.parseRequest(raw)
		if err != nil || r.apply(&expected, request) != nil {
			return invalid
		}
	}
	if !slices.Equal(expected.Records, final.Records) {
		return invalid
	}
	return nil
}
