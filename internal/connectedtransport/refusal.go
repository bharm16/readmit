package connectedtransport

import (
	"encoding/json/v2"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
)

const RefusalSchema = "readmit-connected-transport-refusal/v1"

// Refusal is a diagnostic from an interrupted transport preparation, never a
// sealed execution receipt. DNS admission may already have happened; neither
// a dial nor a payload write has occurred at the client-identity boundary.
// Provider diagnostics and private material are deliberately absent.
type Refusal struct {
	Schema string `json:"schema"`
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

func retainClientRefusal(w *artifactdir.Writer, reason string) error {
	if err := put(w, "refusal.json", Refusal{Schema: RefusalSchema, Stage: "before-dial", Reason: reason}); err != nil {
		return err
	}
	return w.Sync()
}

// ReadRefusal reads a bounded offline diagnostic. It does not verify execution
// evidence, confer authority, resume the operation or resolve its credential.
func ReadRefusal(directory string) (Refusal, error) {
	raw, err := (artifactdir.Document{MaxBytes: 1024}).Read(filepath.Join(directory, "refusal.json"))
	var result Refusal
	if err != nil || json.Unmarshal(raw, &result, json.RejectUnknownMembers(true)) != nil || result.Schema != RefusalSchema || result.Stage != "before-dial" || result.Reason != "client-key-unavailable" && result.Reason != "client-key-pair-invalid" {
		return Refusal{}, refused
	}
	return result, nil
}
