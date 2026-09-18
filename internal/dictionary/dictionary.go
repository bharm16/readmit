// Package dictionary supplies a finite set of version-specific field labels.
// These labels do not provide semantic validation or a conformance profile.
package dictionary

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
)

// Field-label data is adapted from nHapi under MPL-2.0; see
// docs/dictionary-provenance.md and licenses/nhapi-MPL-2.0.txt.
//
//go:embed fields-v251.json
var labels []byte

type Dictionary struct {
	Contract   string                    `json:"contract"`
	HL7Version string                    `json:"hl7_version"`
	Segments   map[string]map[int]string `json:"segments"`
}

func Load() (*Dictionary, error) {
	var dictionary Dictionary
	if err := json.Unmarshal(labels, &dictionary, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("cannot load bundled field labels")
	}
	if dictionary.Contract != "readmit-field-labels/v1" || dictionary.HL7Version != "2.5.1" {
		return nil, fmt.Errorf("unsupported bundled field labels")
	}
	return &dictionary, nil
}
