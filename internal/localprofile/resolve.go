package localprofile

import (
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/profilepack"
)

// Origin says where one resolved rule came from. It is the answer to the only
// question an editor must never guess at: is this rule something the pinned
// profile pack declares, or something this site decided on its own?
type Origin string

const (
	// OriginProfile: the pinned pack declares this rule and the local profile
	// leaves it alone.
	OriginProfile Origin = "profile"
	// OriginOverridden: the local profile replaces what the pinned pack
	// declares. The pack's own value is kept beside it, so what was replaced
	// stays visible.
	OriginOverridden Origin = "overridden"
	// OriginLocal: the local profile declares this rule where the pinned pack
	// declares nothing. It carries no profile backing.
	OriginLocal Origin = "local"
	// OriginUndeclared: neither declares it. Nothing is assumed from silence.
	OriginUndeclared Origin = "undeclared"
)

// FindingKind is the closed set of statements resolving a profile against its
// pinned pack produces. A finding is something to state, never a verdict: this
// package evaluates no message and passes nothing.
type FindingKind string

const (
	// FindingPackNotPinned: the pack offered is not the one this profile
	// pins. Nothing is read from it, and every rule resolves as local or
	// undeclared.
	FindingPackNotPinned FindingKind = "pack_not_pinned"
	// FindingCombinationUnknown: the pinned pack declares nothing about the
	// HL7 version and family this profile constrains.
	FindingCombinationUnknown FindingKind = "combination_unknown"
	// FindingLabelsNotSupported: the pinned pack declares the combination but
	// not its field labels, so no field name here can have profile origin.
	FindingLabelsNotSupported FindingKind = "labels_not_supported"
	// FindingNoStructuralBacking: the pack declares no structural support, so
	// every cardinality, usage, type, terminology, authority and date rule in
	// this profile is local.
	FindingNoStructuralBacking FindingKind = "no_structural_backing"
	// FindingLabelOverridden: the local profile renames a field the pinned
	// pack labels. The pack's name is kept beside the local one.
	FindingLabelOverridden FindingKind = "label_overridden"
	// FindingPositionUnlabelled: the pinned pack supports labels for this
	// combination and neither it nor the profile names this position. A
	// site-defined segment never produces one: no published metadata
	// describes a Z-segment.
	FindingPositionUnlabelled FindingKind = "position_unlabelled"
)

// Finding is one statement about the profile read against its pinned pack.
type Finding struct {
	Kind    FindingKind `json:"kind"`
	Subject string      `json:"subject,omitzero"`
	Detail  string      `json:"detail"`
}

// Support is what the pinned pack declares about the combination this profile
// constrains, at each of the four levels the pack keeps separate.
type Support struct {
	Parse      profilepack.Outcome `json:"parse"`
	Labels     profilepack.Outcome `json:"labels"`
	Structural profilepack.Outcome `json:"structural"`
	Workflow   profilepack.Outcome `json:"workflow"`
}

// ResolvedField is one constrained position with the origin of every rule on
// it. Each rule's origin is answered separately, so a locally invented
// cardinality beside a field name that came from the pack cannot be read as
// one profile-backed row. The pairs are flat rather than one value-and-origin
// object per rule, because this is what an editor renders a row from and a
// nested object per rule buys nothing a reader of it wants.
type ResolvedField struct {
	Position int    `json:"position"`
	Name     string `json:"name,omitzero"`
	// PackName is the name the pinned pack gives this position, kept beside a
	// local name that replaced it. It is empty unless the pack named it.
	PackName          string          `json:"pack_name,omitzero"`
	NameOrigin        Origin          `json:"name_origin"`
	Usage             Usage           `json:"usage"`
	UsageOrigin       Origin          `json:"usage_origin"`
	Condition         *Condition      `json:"condition,omitzero"`
	ConditionOrigin   Origin          `json:"condition_origin"`
	Cardinality       *Cardinality    `json:"cardinality,omitzero"`
	CardinalityOrigin Origin          `json:"cardinality_origin"`
	Type              DataType        `json:"type,omitzero"`
	TypeOrigin        Origin          `json:"type_origin"`
	Terminology       *TerminologySet `json:"terminology,omitzero"`
	TerminologyOrigin Origin          `json:"terminology_origin"`
	Authority         *Authority      `json:"authority,omitzero"`
	AuthorityOrigin   Origin          `json:"authority_origin"`
	Date              *DateHandling   `json:"date,omitzero"`
	DateOrigin        Origin          `json:"date_origin"`
}

// ResolvedSegment is one constrained segment and its fields.
type ResolvedSegment struct {
	ID          string `json:"id"`
	Description string `json:"description,omitzero"`
	// SiteDefined is true for a Z-segment, which HL7 reserves for local use
	// and which therefore no published metadata describes.
	SiteDefined       bool            `json:"site_defined"`
	Cardinality       *Cardinality    `json:"cardinality,omitzero"`
	CardinalityOrigin Origin          `json:"cardinality_origin"`
	Fields            []ResolvedField `json:"fields"`
}

// Resolution is one local profile read together with the pack it pins: what
// the pack supports for the combination, every rule with the origin it has,
// and every statement that follows from the two being read together.
type Resolution struct {
	Profile Identity `json:"profile"`
	Base    Base     `json:"base"`
	// Pinned is whether the pack offered is exactly the pinned one. When it
	// is false nothing at all is read from the pack.
	Pinned   bool              `json:"pinned"`
	Support  Support           `json:"support"`
	Segments []ResolvedSegment `json:"segments"`
	Findings []Finding         `json:"findings"`
}

// Resolve reads one local profile together with the pack it pins and answers,
// per rule, where that rule came from.
//
// The pin is checked first and the pack is read only when it holds: an
// unpinned pack contributes nothing, exactly as profilepack refuses to answer
// for one. Under readmit-profile-pack/v1 a pack carries field labels and
// nothing else, so a field name is the only rule that can ever have profile
// origin; every other rule resolves local when the profile declares it and
// undeclared when nobody does, and the resolution states that rather than
// leaving it to be inferred.
//
// The profile need not have come through Decode, because an editor assembles
// one before it is ever a document. That is safe here and a Pack assembled the
// same way is not: profile origin is decided entirely by what the pack
// answers, and a pack that did not come through its own reader satisfies no
// pin, so nothing a profile claims about itself can give one of its rules a
// standing the pack did not give it.
func Resolve(profile Profile, pack profilepack.Pack) Resolution {
	pinned := pack.Satisfies(profile.Base.Pack) == nil
	resolution := Resolution{
		Profile:  profile.Identity,
		Base:     profile.Base,
		Pinned:   pinned,
		Support:  support(profile.Base, pack, pinned),
		Segments: make([]ResolvedSegment, 0, len(profile.Segments)),
	}
	resolution.Findings = combinationFindings(resolution.Support, pinned)
	declared := index(profile)
	for _, segment := range profile.Segments {
		resolved := ResolvedSegment{
			ID:                segment.ID,
			Description:       segment.Description,
			SiteDefined:       segment.SiteDefined(),
			Cardinality:       cloneCardinality(segment.Cardinality),
			CardinalityOrigin: localOrigin(segment.Cardinality != nil),
			Fields:            make([]ResolvedField, 0, len(segment.Fields)),
		}
		for _, field := range segment.Fields {
			row, findings := resolveField(profile.Base, pack, pinned, declared, segment, field)
			resolved.Fields = append(resolved.Fields, row)
			resolution.Findings = append(resolution.Findings, findings...)
		}
		resolution.Segments = append(resolution.Segments, resolved)
	}
	return resolution
}

func support(base Base, pack profilepack.Pack, pinned bool) Support {
	if !pinned {
		return Support{
			Parse:      profilepack.OutcomeUnknown,
			Labels:     profilepack.OutcomeUnknown,
			Structural: profilepack.OutcomeUnknown,
			Workflow:   profilepack.OutcomeUnknown,
		}
	}
	return Support{
		Parse:      pack.Support(base.HL7Version, base.Family, profilepack.LevelParse),
		Labels:     pack.Support(base.HL7Version, base.Family, profilepack.LevelLabels),
		Structural: pack.Support(base.HL7Version, base.Family, profilepack.LevelStructural),
		Workflow:   pack.Support(base.HL7Version, base.Family, profilepack.LevelWorkflow),
	}
}

func combinationFindings(declared Support, pinned bool) []Finding {
	findings := []Finding{}
	if !pinned {
		findings = append(findings, Finding{
			Kind:   FindingPackNotPinned,
			Detail: "the selected pack is not the pack id and version this profile pins, so nothing was read from it",
		})
	} else if declared.Labels == profilepack.OutcomeUnknown {
		findings = append(findings, Finding{
			Kind:   FindingCombinationUnknown,
			Detail: "the pinned pack declares nothing about this HL7 version and message family",
		})
	} else if !declared.Labels.Passing() {
		findings = append(findings, Finding{
			Kind:   FindingLabelsNotSupported,
			Detail: "the pinned pack declares field labels " + string(declared.Labels) + " for this combination, so no field name here has profile origin",
		})
	}
	// Structural backing is stated for every profile, because no
	// readmit-profile-pack/v1 pack can declare it supported: the contract
	// carries no structural content for a claim to stand on.
	findings = append(findings, Finding{
		Kind:   FindingNoStructuralBacking,
		Detail: "the pinned pack declares structural support " + string(declared.Structural) + ", so every cardinality, usage, type, terminology, authority and date rule in this profile is local and no pack backs it",
	})
	return findings
}

func resolveField(base Base, pack profilepack.Pack, pinned bool, declared declarations, segment Segment, field Field) (ResolvedField, []Finding) {
	var packName string
	labels := profilepack.FieldLabel{Outcome: profilepack.OutcomeUnknown}
	if pinned {
		labels = pack.Label(base.HL7Version, base.Family, segment.ID, field.Position)
		packName = labels.Name
	}
	where := segment.ID + "-" + strconv.Itoa(field.Position)
	row := ResolvedField{
		Position:          field.Position,
		Usage:             field.Usage,
		UsageOrigin:       OriginLocal,
		Condition:         cloneCondition(field.Condition),
		ConditionOrigin:   localOrigin(field.Condition != nil),
		Cardinality:       cloneCardinality(field.Cardinality),
		CardinalityOrigin: localOrigin(field.Cardinality != nil),
		Type:              field.Type,
		TypeOrigin:        localOrigin(field.Type != ""),
		Terminology:       lookupTerminology(declared.terminology, field.Terminology),
		TerminologyOrigin: localOrigin(field.Terminology != ""),
		Authority:         lookup(declared.authorities, field.Authority),
		AuthorityOrigin:   localOrigin(field.Authority != ""),
		Date:              lookup(declared.dates, field.Date),
		DateOrigin:        localOrigin(field.Date != ""),
	}
	findings := []Finding{}
	switch {
	case field.Name != "" && packName != "":
		row.Name, row.PackName, row.NameOrigin = field.Name, packName, OriginOverridden
		findings = append(findings, Finding{
			Kind:    FindingLabelOverridden,
			Subject: where,
			Detail:  "this profile names the field " + field.Name + " where the pinned pack names it " + packName,
		})
	case field.Name != "":
		row.Name, row.NameOrigin = field.Name, OriginLocal
	case packName != "":
		row.Name, row.PackName, row.NameOrigin = packName, packName, OriginProfile
	default:
		row.NameOrigin = OriginUndeclared
		if labels.Outcome.Passing() && !segment.SiteDefined() {
			findings = append(findings, Finding{
				Kind:    FindingPositionUnlabelled,
				Subject: where,
				Detail:  "neither this profile nor the pinned pack names this position",
			})
		}
	}
	return row, findings
}

// localOrigin is the origin of every rule no profile pack can ever declare:
// local where the profile declares it, undeclared where nobody does.
func localOrigin(present bool) Origin {
	if present {
		return OriginLocal
	}
	return OriginUndeclared
}

// lookupTerminology copies the set and its codes, so a caller that keeps a
// resolved rule cannot reach back into the profile through a shared slice.
func lookupTerminology(declared map[string]TerminologySet, id string) *TerminologySet {
	set := lookup(declared, id)
	if set == nil {
		return nil
	}
	set.Codes = slices.Clone(set.Codes)
	return set
}

func lookup[T any](declared map[string]T, id string) *T {
	if id == "" {
		return nil
	}
	entry, ok := declared[id]
	if !ok {
		return nil
	}
	return &entry
}

func cloneCondition(condition *Condition) *Condition {
	if condition == nil {
		return nil
	}
	copied := *condition
	copied.Values = slices.Clone(condition.Values)
	return &copied
}
