// Package observation defines the file handoff from a fixture receiver to a
// test runner. It does not infer workflow success from acknowledgements.
package observation

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"regexp"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
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

// EmptyInitialState reports the declared starting state a ledger run requires:
// nothing processed, no appointment records, and a receiver that called its own
// snapshot consistent. One rule has one implementation, so the runner that
// refuses to evaluate against a dirty ledger and the reset that confirms a
// fixture came back up clean are asking the same question of the same bytes.
func (s Snapshot) EmptyInitialState() bool {
	return s.Consistent && len(s.Processed) == 0 && len(s.Records) == 0
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
	data, err := snapshotFile.Read(path)
	if err != nil {
		return Snapshot{}, err
	}
	return Decode(data)
}

// Create installs a complete first snapshot exclusively. Linking a synced
// temporary file avoids both overwriting a stale observation and exposing an
// empty/partial destination during startup. Temp and destination share a volume.
func Create(path string, snapshot Snapshot) error {
	return CreateWithDurability(path, snapshot, artifactdir.Durable)
}

// CreateWithDurability is Create with an explicit durability: only a first
// snapshot in a throwaway workspace its owner removes before it answers may be
// Scratch, and neither it nor its folder is then flushed.
func CreateWithDurability(path string, snapshot Snapshot, durability artifactdir.Durability) error {
	data, err := Encode(snapshot)
	if err != nil {
		return err
	}
	document := snapshotFile
	document.Durability = durability
	return document.Create(path, data)
}

// Write replaces a snapshot by same-directory rename. Readers see either the
// previous complete JSON document or this one; the live file is never truncated.
func Write(path string, snapshot Snapshot) error { return Install(path, snapshot, (*os.File).Sync) }

// Install replaces a snapshot by the same rename as Write, first calling flush
// on the complete temporary file unless flush is nil, and then syncing the
// folder naming it. A nil flush is for a live observation read back only by the
// process writing it, which keeps any copy it retains through its own synced
// write; such a file is flushed nowhere and is not claimed to survive a system
// crash.
func Install(path string, snapshot Snapshot, flush func(*os.File) error) error {
	data, err := Encode(snapshot)
	if err != nil {
		return err
	}
	document := snapshotFile
	if flush == nil {
		document.Durability = artifactdir.Scratch
	} else {
		document.Flush = flush
	}
	return document.Replace(path, data)
}

// snapshotFile is how an observation snapshot is created, replaced and read.
// A replacement is staged under a fresh temporary name, so one a crash left
// behind never refuses the next, and a first snapshot is linked into place
// whole. A snapshot a person names is read through a link at its name.
var snapshotFile = artifactdir.Document{
	MaxBytes:     MaxBytes,
	Links:        artifactdir.FollowLinks,
	Staging:      artifactdir.StagingTemp(".readmit-observation-*"),
	CreateByLink: true,
	Errors: artifactdir.DocumentErrors{
		Create:  errors.New("cannot create temporary observation file"),
		Write:   errors.New("cannot write observation file"),
		Install: errors.New("cannot install observation file; startup destination must be new"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("observation must be a regular file"),
		Open:      errors.New("cannot open observation file"),
		Read:      errors.New("cannot read observation file"),
		Size:      errors.New("observation exceeds size limit"),
	},
}
