package suite

import (
	"encoding/json/v2"
	"errors"
)

const SelectionSchema = "readmit-suite-selection/v1"

// Selection records which declared site/environment produced the retained
// configuration. It is local metadata, not an approval or execution verdict.
type Selection struct {
	Schema      string `json:"schema"`
	Suite       string `json:"suite"`
	Environment string `json:"environment"`
	Site        string `json:"site"`
}

func DecodeSelection(raw []byte) (Selection, error) {
	var selected Selection
	if len(raw) > 4096 || required(raw, &selected, "schema", "suite", "environment", "site") != nil || selected.Schema != SelectionSchema || !identifier.MatchString(selected.Suite) || !identifier.MatchString(selected.Environment) || !text(selected.Site, 256) {
		return Selection{}, errors.New("invalid suite selection")
	}
	return selected, nil
}
func retainSelection(dir string, doc Document, env Environment) error {
	raw, err := json.Marshal(Selection{Schema: SelectionSchema, Suite: doc.ID, Environment: env.ID, Site: env.Site}, json.Deterministic(true))
	if err != nil {
		return err
	}
	return retain(dir, "selection.json", raw)
}
