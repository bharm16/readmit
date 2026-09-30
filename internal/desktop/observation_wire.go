package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// UnmarshalJSON decodes the selected adapter only. FHIR authoring carries no
// legacy source/window; irrelevant UI placeholders cannot be interpreted as
// frozen source contracts. Legacy callers still use their strict shared reader.
func (d *ObservationDraft) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Connected  *ConnectedObservation `json:"connected,omitzero"`
		Source     jsontext.Value        `json:"source,omitzero"`
		Window     jsontext.Value        `json:"window,omitzero"`
		Credential string                `json:"credential,omitzero"`
	}
	if err := json.Unmarshal(raw, &wire, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	*d = ObservationDraft{Connected: wire.Connected, Credential: wire.Credential}
	if wire.Connected != nil && wire.Connected.FHIR != nil {
		return nil
	}
	if len(wire.Source) > 0 {
		var source observesource.Source
		err := json.Unmarshal(logicalObservationSource(wire.Source), &source, json.RejectUnknownMembers(true))
		if err != nil {
			return err
		}
		d.Source = source
	}
	if len(wire.Window) > 0 {
		var window observewindow.Window
		err := json.Unmarshal(wire.Window, &window, json.RejectUnknownMembers(true))
		if err != nil {
			return err
		}
		d.Window = window
	}
	return nil
}

// logicalObservationSource removes only null placeholders outside a selected
// frozen version. Actual newer clauses remain present and are refused by the
// ordinary domain reader. This adapter never changes artifact-reader rules.
func logicalObservationSource(raw []byte) []byte {
	var fields map[string]jsontext.Value
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	var schema string
	_ = json.Unmarshal(fields["schema"], &schema)
	if schema == observesource.SchemaV1 && string(fields["capture"]) == "null" {
		delete(fields, "capture")
	}
	if (schema == observesource.SchemaV1 || schema == observesource.Schema) && string(fields["database"]) == "null" {
		delete(fields, "database")
	}
	encoded, err := json.Marshal(fields, json.Deterministic(true))
	if err != nil {
		return raw
	}
	return encoded
}

func (d ObservationDraft) MarshalJSON() ([]byte, error) {
	var wire struct {
		Connected  *ConnectedObservation `json:"connected,omitzero"`
		Source     jsontext.Value        `json:"source,omitzero"`
		Window     jsontext.Value        `json:"window,omitzero"`
		Credential string                `json:"credential,omitzero"`
	}
	wire.Connected = d.Connected
	wire.Credential = d.Credential
	if d.Connected == nil || d.Connected.FHIR == nil {
		raw, err := json.Marshal(d.Source, json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		wire.Source = logicalObservationSource(raw)
		raw, err = json.Marshal(d.Window, json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		wire.Window = raw
	}
	return json.Marshal(wire, json.Deterministic(true))
}

func (r *ObservationFieldsRequest) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Typed        bool           `json:"typed,omitzero"`
		Context      RequestContext `json:"context"`
		Environment  string         `json:"environment,omitzero"`
		Resource     string         `json:"resource,omitzero"`
		SchemaSample string         `json:"schema_sample,omitzero"`
		Source       jsontext.Value `json:"source,omitzero"`
	}
	if err := json.Unmarshal(raw, &wire, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	*r = ObservationFieldsRequest{Context: wire.Context, Environment: wire.Environment, Resource: wire.Resource, SchemaSample: wire.SchemaSample, Typed: wire.Typed}
	if wire.Resource == "" && len(wire.Source) > 0 && string(wire.Source) != "null" {
		var source observesource.Source
		err := json.Unmarshal(logicalObservationSource(wire.Source), &source, json.RejectUnknownMembers(true))
		if err != nil {
			return err
		}
		r.Source = &source
	}
	return nil
}
