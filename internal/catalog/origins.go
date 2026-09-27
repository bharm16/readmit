package catalog

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/strictdoc"
)

// An object the application generated from another object of the project
// records where it came from: a case generated from a synthetic scenario
// names that scenario and the exact revision its plan was. The association is
// recorded in readmit-origins/v1 beside the catalog, never in the generated
// evidence, so the case's bytes stay exactly what the generator wrote. An
// object is recorded once; generating it again from the same plan records
// nothing new.
const (
	// OriginsSchema is the origins document contract.
	OriginsSchema = "readmit-origins/v1"
	// OriginsDocumentName is the document in the catalog's folder.
	OriginsDocumentName = "origins.json"
	// MaxOrigins bounds one project's recorded origins.
	MaxOrigins = 4096

	maxOriginsBytes = 1 << 20
)

// The kinds of origin this release records: a case generated from a
// scenario, and a case the built-in sample fixture received, with the
// observation ledger the fixture wrote beside it. Both are synthetic.
const (
	OriginScenario = "scenario"
	OriginFixture  = "fixture"
)

// ErrTooManyOrigins refuses an origin past MaxOrigins.
var ErrTooManyOrigins = errors.New("the project holds as many recorded origins as this release keeps")

// Origin is one generated object's source: the object, what kind of source
// it came from and when the application recorded it; for a scenario, the
// source object and the revision of it; for the fixture, the mode it ran in
// and the project entry of the observation ledger it wrote.
type Origin struct {
	Item        string `json:"item"`
	Kind        string `json:"kind"`
	Source      string `json:"source,omitzero"`
	Revision    int    `json:"revision,omitzero"`
	Mode        string `json:"mode,omitzero"`
	Observation string `json:"observation,omitzero"`
	RecordedAt  string `json:"recorded_at"`
}

type originsDoc struct {
	Schema  string   `json:"schema"`
	Origins []Origin `json:"origins"`
}

var originsReading = strictdoc.Document{
	MaxBytes:    maxOriginsBytes,
	Schema:      OriginsSchema,
	Invalid:     "invalid origins document",
	TooLarge:    "origins document exceeds its size limit",
	MustDeclare: "an origins document declares its contract version",
	Unsupported: ErrUnsupportedVersion,
}

var originsFile = artifactdir.Document{
	MaxBytes: maxOriginsBytes,
	Staging:  artifactdir.StagingTemp(OriginsDocumentName + ".*.incomplete"),
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the origins here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write the origins"),
		Install:     errors.New("cannot replace the origins"),
		Sync:        errors.New("cannot confirm the origins were retained durably"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("the project holds no origins"),
		Irregular: errors.New("the origins must be a regular file"),
		Open:      errors.New("the origins cannot be opened"),
		Read:      errors.New("the origins cannot be read"),
	},
}

// Origins reads every origin the project records, oldest first. A project
// that never generated anything records none.
func (s *Store) Origins() ([]Origin, error) {
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := originsFile.ReadIn(root, path.Join(Folder, OriginsDocumentName))
	if errors.Is(err, fs.ErrNotExist) {
		return []Origin{}, nil
	}
	if err != nil {
		return nil, err
	}
	var document originsDoc
	if err := originsReading.Decode(data, &document); err != nil {
		return nil, err
	}
	if err := validateOrigins(document.Origins); err != nil {
		return nil, err
	}
	return document.Origins, nil
}

// RecordOrigin records where one object came from, unless the project
// already records its origin, which is kept as first recorded. It answers the
// origin the project holds for the object. RecordedAt is set from now.
func (s *Store) RecordOrigin(origin Origin, now time.Time) (Origin, error) {
	item := origin.Item
	origin.RecordedAt = Stamp(now)
	if err := validateOrigins([]Origin{origin}); err != nil {
		return Origin{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Origin{}, err
	}
	defer unlock()
	held, err := s.Origins()
	if err != nil {
		return Origin{}, err
	}
	if index := slices.IndexFunc(held, func(recorded Origin) bool { return recorded.Item == item }); index >= 0 {
		return held[index], nil
	}
	if len(held) >= MaxOrigins {
		return Origin{}, ErrTooManyOrigins
	}
	held = append(held, origin)
	data, err := json.Marshal(originsDoc{Schema: OriginsSchema, Origins: held}, json.Deterministic(true))
	if err != nil {
		return Origin{}, errors.New("cannot encode the origins")
	}
	data = append(data, '\n')
	if len(data) > maxOriginsBytes {
		return Origin{}, errors.New("the origins document exceeds its size limit")
	}
	managed, err := s.managed()
	if err != nil {
		return Origin{}, err
	}
	defer managed.Close()
	if err := originsFile.ReplaceIn(managed, OriginsDocumentName, data); err != nil {
		return Origin{}, err
	}
	return origin, nil
}

func validateOrigins(origins []Origin) error {
	if len(origins) > MaxOrigins {
		return errors.New("a project keeps at most " + strconv.Itoa(MaxOrigins) + " origins")
	}
	items := map[string]bool{}
	for _, origin := range origins {
		scenario := origin.Kind == OriginScenario && ValidID(origin.Source) && origin.Revision >= 1 && origin.Mode == "" && origin.Observation == ""
		fixture := origin.Kind == OriginFixture && origin.Source == "" && origin.Revision == 0 && (origin.Mode == "fixed" || origin.Mode == "defective") &&
			entryName(origin.Observation)
		if !ValidID(origin.Item) || !scenario && !fixture || !stamp(origin.RecordedAt) || items[origin.Item] {
			return errors.New("an origin names its object, where it came from and when, once")
		}
		items[origin.Item] = true
	}
	return nil
}

// entryName is one entry of the project folder: a name, never a path.
func entryName(value string) bool {
	return value != "" && len(value) <= 255 && path.Base(value) == value && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00")
}
