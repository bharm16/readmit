package diagnose

import (
	"bytes"
	_ "embed"
	"encoding/json/v2"
	"errors"
)

//go:embed profile.json
var profileJSON []byte

// ProfileSnapshot returns the actual embedded profile and ruleset definition
// for retained evidence, without exposing mutable package storage.
func ProfileSnapshot() []byte { return bytes.Clone(profileJSON) }

type profileDefinition struct {
	Profile         string              `json:"profile"`
	Ruleset         string              `json:"ruleset"`
	HL7Version      string              `json:"hl7_version"`
	Triggers        []triggerDefinition `json:"triggers"`
	AuthorityFields [][]string          `json:"authority_fields"`
}

type triggerDefinition struct {
	Code           string   `json:"code"`
	RequiredFields []string `json:"required_fields"`
}

func loadProfile() (profileDefinition, error) {
	var p profileDefinition
	if err := json.Unmarshal(profileJSON, &p, json.RejectUnknownMembers(true)); err != nil || p.Profile != Profile || p.Ruleset != Ruleset {
		return p, errors.New("invalid embedded diagnosis profile")
	}
	return p, nil
}

func (p profileDefinition) trigger(code string) (triggerDefinition, bool) {
	for _, trigger := range p.Triggers {
		if trigger.Code == code {
			return trigger, true
		}
	}
	return triggerDefinition{}, false
}
