package testrunner

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

var assertionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var occurrenceID = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)

func DecodeSpec(data []byte) (Spec, error) {
	var spec Spec
	if len(data) > MaxSpecBytes || json.Unmarshal(data, &spec, json.RejectUnknownMembers(true)) != nil {
		return spec, errors.New("invalid test spec JSON")
	}
	return spec, spec.Validate()
}

func ReadSpec(path string) (Spec, error) {
	data, err := readLocal(path, MaxSpecBytes)
	if err != nil {
		return Spec{}, errors.New("cannot read test spec")
	}
	return DecodeSpec(data)
}

func (s Spec) Validate() error {
	invalid := errors.New("invalid test spec contract")
	if s.Schema != SpecSchema || !text(s.Name, 256) || !text(s.Input.Case, 4096) || !text(s.Target, 4096) || !text(s.Setup.ResetInstructions, 8192) || len(s.Input.Messages) == 0 || len(s.Input.Messages) > replay.MaxMessages || len(s.Assertions) == 0 || len(s.Assertions) > MaxAssertions {
		return invalid
	}
	if s.Observation.Boundary == LedgerBoundary {
		if s.Setup.InitialState != "empty-ledger" || !text(s.Observation.Path, 4096) {
			return invalid
		}
	} else if s.Observation.Boundary == ACKBoundary {
		if s.Setup.InitialState != "operator-declared" || s.Observation.Path != "" {
			return invalid
		}
	} else {
		return invalid
	}
	messages := make(map[string]bool)
	for _, id := range s.Input.Messages {
		if !occurrenceID.MatchString(id) || messages[id] {
			return invalid
		}
		messages[id] = true
	}
	ids := make(map[string]bool)
	ledgerAssertions := 0
	for _, assertion := range s.Assertions {
		if !assertionID.MatchString(assertion.ID) || ids[assertion.ID] {
			return invalid
		}
		ids[assertion.ID] = true
		v := assertion.Expected
		switch assertion.Operator {
		case "ledger_count":
			if s.Observation.Boundary != LedgerBoundary || assertion.Message != "" || assertion.Selector != "" || v.Count == nil || *v.Count < 0 || *v.Count > observation.MaxOccurrences || v.Records != nil || v.Field != nil {
				return invalid
			}
			ledgerAssertions++
		case "ledger_equals":
			if s.Observation.Boundary != LedgerBoundary || assertion.Message != "" || assertion.Selector != "" || v.Records == nil || v.Count != nil || v.Field != nil {
				return invalid
			}
			snapshot := observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: strings.Repeat("0", 32), Mode: observation.Fixed, Consistent: true, Processed: []observation.Occurrence{}, Records: *v.Records}
			if snapshot.Validate() != nil {
				return invalid
			}
			ledgerAssertions++
		case "ack_field_equals":
			selector, err := hl7.ParseSelector(assertion.Selector)
			if err != nil {
				return invalid
			}
			if segment := selector.Parts().Segment; segment != "MSA" && segment != "ERR" || !messages[assertion.Message] || v.Field == nil || v.Count != nil || v.Records != nil {
				return invalid
			}
			if v.Field.Validate() != nil {
				return invalid
			}
		default:
			return invalid
		}
	}
	if s.Observation.Boundary == LedgerBoundary && ledgerAssertions == 0 {
		return invalid
	}
	return nil
}

// Validate holds one expected value to the four states the shared selector
// returns. Only a present value carries text; every other state omits it, so an
// absent field and an empty one stay two different expectations. It is exported
// because a caller authoring a spec holds a value to the rule this reader holds
// it to rather than to a second copy of it.
func (f FieldValue) Validate() error {
	invalid := errors.New("invalid expected field value")
	if f.State == hl7.Present {
		if f.Text == nil || len(*f.Text) == 0 || len(*f.Text) > MaxExpectedTextBytes || !utf8.ValidString(*f.Text) {
			return invalid
		}
		return nil
	}
	if f.State != hl7.Empty && f.State != hl7.Null && f.State != hl7.Omitted || f.Text != nil {
		return invalid
	}
	return nil
}

func text(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// Prepare reads locally only. Paths resolve relative to the real spec file,
// after resolving its symlinks; target CA paths follow replay's own contract.
func Prepare(specPath string) (*Plan, error) {
	return PrepareWithDurability(specPath, artifactdir.Durable)
}

// PrepareWithDurability is Prepare for a plan that executes with the durability
// its caller chose, as RunWithDurability does.
func PrepareWithDurability(specPath string, durability artifactdir.Durability) (*Plan, error) {
	resolved, err := filepath.EvalSymlinks(specPath)
	if err != nil {
		return nil, errors.New("cannot resolve test spec")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, errors.New("cannot resolve test spec")
	}
	raw, err := readLocal(resolved, MaxSpecBytes)
	if err != nil {
		return nil, errors.New("cannot read test spec")
	}
	plan, err := prepareSpec(raw, filepath.Dir(resolved), durability)
	if err != nil {
		return nil, err
	}
	plan.specPath = resolved
	return plan, nil
}

// PrepareSpec prepares the exact specification bytes resolved from dir: the
// plan Prepare builds for the file holding those bytes, without the bytes
// having to be on disk. A caller that compiles a specification itself — a
// suite expansion binds rows to templates it does not retain — asks the same
// input preparation of the compiled bytes, so its evidence send order and its
// target are decided by this one reader before anything retains or executes
// them. The plan carries no spec path: it is for inspection, never a start.
func PrepareSpec(raw []byte, dir string) (*Plan, error) {
	return prepareSpec(raw, dir, artifactdir.Durable)
}

func prepareSpec(raw []byte, dir string, durability artifactdir.Durability) (*Plan, error) {
	spec, err := DecodeSpec(raw)
	if err != nil {
		return nil, err
	}
	source := artifactpath.JoinReference(dir, spec.Input.Case)
	target, err := replay.ReadTarget(artifactpath.JoinReference(dir, spec.Target))
	if err != nil {
		return nil, err
	}
	prepared, err := replay.Prepare(source, target, replay.Options{Occurrences: spec.Input.Messages, Durability: durability})
	if err != nil {
		return nil, err
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return nil, errors.New("cannot inspect test case")
	}
	path := ""
	if spec.Observation.Boundary == LedgerBoundary {
		path = artifactpath.JoinReference(dir, spec.Observation.Path)
	}
	return &Plan{spec: spec, raw: raw, sourcePath: source, sourceInfo: sourceInfo, observationPath: path, replay: prepared, durability: durability}, nil
}
