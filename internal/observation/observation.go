// Package observation defines the file handoff from a fixture receiver to a
// test runner. It does not infer workflow success from acknowledgements.
package observation

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

const (
	Schema         = "readmit-observation/v1"
	Profile        = "readmit-siu-v1"
	MaxBytes       = 8 << 20
	MaxOccurrences = 5000
)

type Mode string

const (
	Fixed     Mode = "fixed"
	Defective Mode = "defective"
)

// Identifier retains assigning authority. Equal values in different namespaces
// are distinct identifiers; absent authority is explicitly the empty tuple.
type Identifier struct {
	Value           string `json:"value"`
	Namespace       string `json:"namespace"`
	UniversalID     string `json:"universal_id"`
	UniversalIDType string `json:"universal_id_type"`
}

func (id *Identifier) UnmarshalJSON(data []byte) error {
	var required struct {
		Value           *string `json:"value"`
		Namespace       *string `json:"namespace"`
		UniversalID     *string `json:"universal_id"`
		UniversalIDType *string `json:"universal_id_type"`
	}
	if err := json.Unmarshal(data, &required, json.RejectUnknownMembers(true)); err != nil || required.Value == nil || required.Namespace == nil || required.UniversalID == nil || required.UniversalIDType == nil {
		return errors.New("observation identifier requires four explicit string fields")
	}
	*id = Identifier{Value: *required.Value, Namespace: *required.Namespace, UniversalID: *required.UniversalID, UniversalIDType: *required.UniversalIDType}
	return nil
}

type Record struct {
	RecordID         string     `json:"record_id"`
	PatientID        Identifier `json:"patient_id"`
	PlacerID         Identifier `json:"placer_id"`
	FillerID         Identifier `json:"filler_id"`
	AppointmentStart string     `json:"appointment_start"`
}

type Occurrence struct {
	OccurrenceID string `json:"occurrence_id"`
	ControlID    string `json:"control_id"`
}

type Snapshot struct {
	Schema     string       `json:"schema"`
	Profile    string       `json:"profile"`
	SessionID  string       `json:"session_id"`
	Mode       Mode         `json:"mode"`
	Processed  []Occurrence `json:"processed"`
	Consistent bool         `json:"consistent"`
	Records    []Record     `json:"records"`
}

var sessionPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)

func (s Snapshot) Validate() error {
	if s.Schema != Schema || s.Profile != Profile || !sessionPattern.MatchString(s.SessionID) || s.Mode != Fixed && s.Mode != Defective {
		return errors.New("invalid observation schema, profile, session, or mode")
	}
	if len(s.Processed) > MaxOccurrences || len(s.Records) > MaxOccurrences {
		return errors.New("observation exceeds occurrence limit")
	}
	seen := make(map[string]bool)
	for _, occurrence := range s.Processed {
		if !occurrencePattern.MatchString(occurrence.OccurrenceID) || seen[occurrence.OccurrenceID] || !textValue(occurrence.ControlID) || occurrence.ControlID == "" {
			return errors.New("invalid processed occurrence in observation")
		}
		seen[occurrence.OccurrenceID] = true
	}
	for i, record := range s.Records {
		if record.RecordID != fmt.Sprintf("r%06d", i+1) || !textValue(record.AppointmentStart) || record.AppointmentStart == "" {
			return errors.New("invalid observation ledger record")
		}
		for _, id := range []Identifier{record.PatientID, record.PlacerID, record.FillerID} {
			if id.Value == "" || !textValue(id.Value) || !textValue(id.Namespace) || !textValue(id.UniversalID) || !textValue(id.UniversalIDType) || (id.UniversalID == "") != (id.UniversalIDType == "") {
				return errors.New("invalid identifier in observation")
			}
		}
	}
	return nil
}

func textValue(s string) bool {
	return len(s) <= 1024 && utf8.ValidString(s)
}

func Encode(snapshot Snapshot) ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(snapshot, json.Deterministic(true))
	if err != nil || len(data) >= MaxBytes {
		return nil, errors.New("cannot encode bounded observation")
	}
	return append(data, '\n'), nil
}

func Decode(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	if len(data) > MaxBytes {
		return snapshot, errors.New("observation exceeds size limit")
	}
	var required struct {
		Consistent *bool         `json:"consistent"`
		Processed  *[]Occurrence `json:"processed"`
		Records    *[]Record     `json:"records"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Consistent == nil || required.Processed == nil || required.Records == nil {
		return snapshot, errors.New("observation requires consistent, processed, and records")
	}
	if err := json.Unmarshal(data, &snapshot, json.RejectUnknownMembers(true)); err != nil {
		return snapshot, errors.New("invalid observation JSON")
	}
	return snapshot, snapshot.Validate()
}

func Read(path string) (Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, errors.New("cannot open observation file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Snapshot{}, errors.New("observation must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return Snapshot{}, errors.New("cannot read observation file")
	}
	return Decode(data)
}

// Create installs a complete first snapshot exclusively. Linking a synced
// temporary file avoids both overwriting a stale observation and exposing an
// empty/partial destination during startup. Temp and destination share a volume.
func Create(path string, snapshot Snapshot) error { return write(path, snapshot, true) }

// Write replaces a snapshot by same-directory rename. Readers see either the
// previous complete JSON document or this one; the live file is never truncated.
func Write(path string, snapshot Snapshot) error { return write(path, snapshot, false) }

func write(path string, snapshot Snapshot, create bool) error {
	data, err := Encode(snapshot)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".readmit-observation-*")
	if err != nil {
		return errors.New("cannot create temporary observation file")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write observation file")
	}
	if create {
		err = os.Link(file.Name(), path)
	} else {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		return errors.New("cannot install observation file; startup destination must be new")
	}
	return nil
}
