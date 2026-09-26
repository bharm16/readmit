// Package importer brings files, folders and archives an engineer already holds
// into a new imported case bundle. Every decision that could otherwise be
// guessed — how a member's bytes divide into messages, where a batch boundary
// falls, which character encoding the source was written in, and which
// direction the traffic travelled — is declared in a versioned import plan and
// read from it. Nothing here transcodes, repairs, reorders or drops a byte: a
// member whose bytes contradict its declaration is refused by name, and a
// record this release cannot parse is retained as quarantined evidence.
//
// A member that carries its evidence inside an envelope — a CSV export, a JSON
// or XML document, a timestamped text log — is divided by a versioned mapping
// recipe instead, which also says where each record's payload, observed time,
// source, direction and channel are. A recipe supplies each of those through
// one typed operator chosen from a closed list; it holds no expression, no
// pattern and no hook, and an envelope this release cannot map is met by adding
// an operator here rather than by making a recipe executable. A record whose
// declared operators do not all resolve is retained with all of its own bytes
// and none of its provenance, so nothing is ever half read.
package importer

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/strictdoc"
)

// The three contract versions this package owns. A new member of any of them is
// a new version string with a reader for every older one, never an added member
// and never an in-place migration.
const (
	PlanSchema    = "readmit-import-plan/v1"
	PreviewSchema = "readmit-import-preview/v1"
	ReceiptSchema = "readmit-import-receipt/v1"
)

// Framing divides one member's bytes into records. There is deliberately no
// automatic value: how a file divides into messages is the operator's
// declaration, never a detection.
type Framing string

const (
	RawFraming   Framing = "raw"
	MLLPFraming  Framing = "mllp"
	BatchFraming Framing = "batch"
)

// storedFormat is the case bundle framing a record of this declared framing is
// stored under. A batch record is one whole message, so it is stored raw and
// the case bundle divides only an MLLP record into frames.
func (f Framing) storedFormat() hl7.Format {
	if f == MLLPFraming {
		return hl7.MLLP
	}
	return hl7.Raw
}

// Boundary names exactly where a new record begins inside a batch member, so a
// multi-message file is split by a stated rule rather than by a likely guess.
type Boundary string

const (
	SegmentStart Boundary = "segment-start"
	HL7Batch     Boundary = "hl7-batch"
)

// Encoding is the declared source character encoding. readmit never decodes
// characters and never transcodes evidence; an encoding is recorded as what the
// operator states the source was written in. UTF8 and USASCII are the two the
// bytes can contradict, and a contradiction is refused rather than
// reinterpreted. Latin1 admits every byte sequence, and UnknownEncoding is an
// explicit statement that the encoding is not known — neither is ever checked,
// and neither claims that any value in the evidence can be decoded.
type Encoding string

const (
	UTF8            Encoding = "utf-8"
	USASCII         Encoding = "us-ascii"
	Latin1          Encoding = "iso-8859-1"
	UnknownEncoding Encoding = "unknown"
)

// MaxPlanBytes bounds one plan document, and MaxMemberSuffixes bounds its
// declared member list. A plan past either bound is refused, never truncated.
const (
	MaxPlanBytes      = 64 << 10
	MaxMemberSuffixes = 32
	maxSuffixBytes    = 32
)

// ErrUnsupportedVersion reports a plan written under a contract version this
// release does not read. It is distinct from a plan this release reads and
// rejects, so a caller can say which one it was handed.
var ErrUnsupportedVersion = errors.New("unsupported import plan version")

// Plan is the complete, reusable import configuration. Every member is
// required; the plan is recorded verbatim in both the preview and the receipt,
// so the declarations an import ran under stay with its evidence.
type Plan struct {
	Schema string `json:"schema"`
	// Framing divides a member's bytes into records.
	Framing Framing `json:"framing"`
	// BatchBoundary names where a record begins. It is declared exactly when
	// Framing is BatchFraming and refused otherwise.
	BatchBoundary Boundary `json:"batch_boundary,omitzero"`
	// Terminator is the declared segment terminator handed to the parser. The
	// automatic value the inspection commands accept is refused here.
	Terminator hl7.Terminator `json:"terminator"`
	// Encoding is the declared source character encoding. It is recorded, and
	// checked only where bytes can contradict it; it never converts evidence.
	Encoding Encoding `json:"encoding"`
	// Direction is the declared direction of every occurrence this plan
	// imports, relative to the observer. It is never read from a message.
	Direction bundle.Direction `json:"direction"`
	// Members are the lowercase file-name suffixes a folder or archive entry
	// must end with to be imported. An empty list makes every regular entry a
	// member; anything else is excluded with that reason recorded.
	Members []string `json:"members"`
}

// planDocument is the one strict reading of a plan: the four refusals are
// strictdoc's, tested there once, and a later contract version reads as
// unsupported rather than as invalid.
var planDocument = strictdoc.Document{
	MaxBytes:    MaxPlanBytes,
	Schema:      PlanSchema,
	Required:    []string{"framing", "terminator", "encoding", "direction", "members"},
	Invalid:     "invalid import plan",
	TooLarge:    "import plan exceeds its size limit",
	MustDeclare: "an import plan declares its contract version",
	Requires:    "an import plan declares framing, terminator, encoding, direction, and an explicit member list",
	Unsupported: ErrUnsupportedVersion,
}

// UnmarshalJSON requires every member explicitly, so an omitted declaration
// cannot decode into a silently permissive zero value, and refuses unknown
// members so a misspelled declaration is an error rather than a declaration
// that quietly did nothing. The reading is strictdoc's.
func (p *Plan) UnmarshalJSON(data []byte) error {
	type plainPlan Plan
	var value plainPlan
	if err := planDocument.Decode(data, &value); err != nil {
		return err
	}
	*p = Plan(value)
	return nil
}

// DecodePlan reads a plan document. Unknown members and unknown versions are
// errors; there is no migration and no repair. Diagnostics name the declaration
// at fault and never repeat the value that failed.
func DecodePlan(data []byte) (Plan, error) {
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Plan{}, ErrUnsupportedVersion
		}
		return Plan{}, errors.New("invalid import plan")
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// PlanVocabulary is every value a plan member can declare, in the order a
// person is offered them, so a window composing a plan offers exactly what
// Validate accepts. PayloadFramings are the framings that divide a member
// without a batch boundary, the ones a mapping recipe's payload declares.
type PlanVocabulary struct {
	Framings        []Framing          `json:"framings"`
	PayloadFramings []Framing          `json:"payload_framings"`
	Boundaries      []Boundary         `json:"boundaries"`
	Terminators     []hl7.Terminator   `json:"terminators"`
	Encodings       []Encoding         `json:"encodings"`
	Directions      []bundle.Direction `json:"directions"`
}

// Vocabulary is the plan vocabulary this release reads.
func Vocabulary() PlanVocabulary {
	return PlanVocabulary{
		Framings:        []Framing{RawFraming, MLLPFraming, BatchFraming},
		PayloadFramings: []Framing{RawFraming, MLLPFraming},
		Boundaries:      []Boundary{SegmentStart, HL7Batch},
		Terminators:     []hl7.Terminator{hl7.CR, hl7.LF, hl7.CRLF},
		Encodings:       []Encoding{UTF8, USASCII, Latin1, UnknownEncoding},
		Directions:      []bundle.Direction{bundle.Inbound, bundle.Outbound, bundle.Unknown},
	}
}

// Validate reports the first reason a plan cannot be used.
func (p Plan) Validate() error {
	if p.Schema != PlanSchema {
		return ErrUnsupportedVersion
	}
	switch p.Framing {
	case RawFraming, MLLPFraming:
		if p.BatchBoundary != "" {
			return errors.New("a batch boundary is declared only with batch framing")
		}
	case BatchFraming:
		if p.BatchBoundary != SegmentStart && p.BatchBoundary != HL7Batch {
			return errors.New("batch framing declares a boundary of segment-start or hl7-batch")
		}
	default:
		return errors.New("framing must be declared as raw, mllp, or batch")
	}
	if p.Terminator != hl7.CR && p.Terminator != hl7.LF && p.Terminator != hl7.CRLF {
		return errors.New("an import plan declares a terminator of cr, lf, or crlf")
	}
	switch p.Encoding {
	case UTF8, USASCII, Latin1, UnknownEncoding:
	default:
		return errors.New("encoding must be declared as utf-8, us-ascii, iso-8859-1, or unknown")
	}
	if p.Direction != bundle.Unknown && p.Direction != bundle.Inbound && p.Direction != bundle.Outbound {
		return errors.New("direction must be declared as unknown, inbound, or outbound")
	}
	return validateMembers(p.Members, "an import plan")
}

// Selects reports whether this plan's declared members select an entry of this
// name. It is the same rule an import applies to a folder or archive entry,
// exported so a collection from an approved source selects the entries of a
// listing the way an import selects the entries of a container. A second copy
// of the rule would drift the first time either one changed.
func (p Plan) Selects(name string) bool { return selected(p.Members, name) }

// validateMembers is one member-suffix rule, so a folder or archive is
// filtered the same way whichever declaration an import ran under. The caller
// names itself, because a plan and a recipe say so in their own diagnostics.
func validateMembers(members []string, declaration string) error {
	if members == nil {
		return errors.New(declaration + " declares an explicit member list")
	}
	if len(members) > MaxMemberSuffixes {
		return errors.New(declaration + " declares at most " + strconv.Itoa(MaxMemberSuffixes) + " member suffixes")
	}
	for i, suffix := range members {
		if err := memberSuffix(suffix); err != nil {
			return errors.New("member suffix: " + err.Error())
		}
		if slices.Contains(members[:i], suffix) {
			return errors.New("a member suffix is declared twice")
		}
	}
	return nil
}

// memberSuffix is the character set of a declared member suffix: lowercase, so
// matching is the same on every filesystem, and free of any separator, so a
// suffix can never name a directory or reach outside the container.
func memberSuffix(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxSuffixBytes {
		return errors.New("must be at most " + strconv.Itoa(maxSuffixBytes) + " characters")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return errors.New("must use only lowercase letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}
