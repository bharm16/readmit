package profileversion

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/localprofile"
)

// Part names which part of a local profile one change touches. The set is
// closed: a change this package cannot name is a change it does not report.
type Part string

const (
	// PartBase: the pack the profile pins, or the HL7 version and message
	// family it constrains.
	PartBase Part = "base"
	// PartSegment: one constrained segment's own description or cardinality.
	// Its fields are reported separately, so renaming a segment's description
	// and reworking a field are never one line.
	PartSegment Part = "segment"
	// PartField: one constrained position.
	PartField Part = "field"
	// PartTerminology, PartAuthority, PartDate: one declared local code table,
	// assigning authority or date rule.
	PartTerminology Part = "terminology"
	PartAuthority   Part = "authority"
	PartDate        Part = "date"
)

// Kind says what happened to a part between the two versions.
type Kind string

const (
	// KindAdded: the later version declares this part and the earlier one
	// does not.
	KindAdded Kind = "added"
	// KindRemoved: the earlier version declares this part and the later one
	// does not.
	KindRemoved Kind = "removed"
	// KindChanged: both declare it and what it declares differs.
	KindChanged Kind = "changed"
)

// Change is one difference between two versions of one local profile.
//
// Subject names the part in the profile's own terms: a segment identifier, a
// position written the way the shared selector grammar writes one, or the id of
// a declared set. The base carries none, because there is one.
//
// Detail is one readable line. For a change it names the members that differ;
// for an addition or a removal it names what the part declares. It never
// restates a value: a profile's own document is where a rule is read, and a
// comparison that copied values out of it would be a second place they could
// disagree.
type Change struct {
	Part    Part   `json:"part"`
	Kind    Kind   `json:"kind"`
	Subject string `json:"subject,omitzero"`
	Detail  string `json:"detail"`
}

// Comparison is what differs between two versions of one local profile.
//
// It is an answer, not a document: nothing writes it, so it declares no
// contract name. It carries no verdict either. A change is a statement about
// the contract a team wrote down, never a statement about a test result.
type Comparison struct {
	Profile string   `json:"profile"`
	From    string   `json:"from"`
	To      string   `json:"to"`
	Changes []Change `json:"changes"`
}

// Compare reports what differs between two versions of one local profile.
//
// Both profiles are held to their whole contract and canonicalized first, so a
// comparison is of the rules two documents declare and never of the order
// somebody happened to write them in.
//
// Three things are refused rather than reported. Two different profiles are not
// two versions of one. A version compared with itself is not a comparison. And
// two different documents carrying the same version are refused by name: a
// version stands for one document, so a profile that changed carries a new
// version rather than a second meaning for an old one.
func Compare(from, to localprofile.Profile) (Comparison, error) {
	before, beforeSeal, err := canonical(from)
	if err != nil {
		return Comparison{}, err
	}
	after, afterSeal, err := canonical(to)
	if err != nil {
		return Comparison{}, err
	}
	if before.Identity.ID != after.Identity.ID {
		return Comparison{}, errors.New("two versions of one local profile are compared; " + before.Identity.ID + " and " + after.Identity.ID + " are different profiles")
	}
	if before.Identity.Version == after.Identity.Version {
		if beforeSeal.Content != afterSeal.Content {
			return Comparison{}, errors.New("the local profile " + name(before.Identity) + " carries two different documents: a profile that changed carries a new version")
		}
		return Comparison{}, errors.New("a version of the local profile " + before.Identity.ID + " is compared with another version, not with itself")
	}
	comparison := Comparison{
		Profile: before.Identity.ID,
		From:    before.Identity.Version,
		To:      after.Identity.Version,
		Changes: []Change{},
	}
	comparison.Changes = append(comparison.Changes, baseChanges(before.Base, after.Base)...)
	comparison.Changes = append(comparison.Changes, segmentChanges(before.Segments, after.Segments)...)
	comparison.Changes = append(comparison.Changes, terminologyChanges(before.Terminology, after.Terminology)...)
	comparison.Changes = append(comparison.Changes, authorityChanges(before.Authorities, after.Authorities)...)
	comparison.Changes = append(comparison.Changes, dateChanges(before.Dates, after.Dates)...)
	return comparison, nil
}

// declaredChanges is the one shape every declared part of a profile is compared
// by: index both sides, walk every key either side declares in order, and answer
// added, removed, or changed with the members that differ. Stating it once is
// what keeps a data type and a date rule from acquiring two different ideas of
// what "changed" means.
//
// summary says what a part that appeared or disappeared declares; differing
// names what differs between two that both do. A part that differs in nothing
// produces no change at all.
func declaredChanges[T any, K cmp.Ordered](
	part Part,
	before, after []T,
	key func(T) K,
	subject func(K) string,
	summary func(T) string,
	differing func(before, after T) []string,
) []Change {
	left := byKey(before, key)
	right := byKey(after, key)
	var changes []Change
	for _, declared := range keys(left, right) {
		earlier, was := left[declared]
		later, still := right[declared]
		switch {
		case !was:
			changes = append(changes, Change{Part: part, Kind: KindAdded, Subject: subject(declared), Detail: summary(later)})
		case !still:
			changes = append(changes, Change{Part: part, Kind: KindRemoved, Subject: subject(declared), Detail: summary(earlier)})
		default:
			if members := differing(earlier, later); len(members) > 0 {
				changes = append(changes, Change{Part: part, Kind: KindChanged, Subject: subject(declared), Detail: phrase(members)})
			}
		}
	}
	return changes
}

func baseChanges(before, after localprofile.Base) []Change {
	members := differingRules(
		rule{"pinned pack", before.Pack != after.Pack},
		rule{"HL7 version", before.HL7Version != after.HL7Version},
		rule{"message family", before.Family != after.Family},
	)
	if len(members) == 0 {
		return nil
	}
	return []Change{{Part: PartBase, Kind: KindChanged, Detail: phrase(members)}}
}

// segmentChanges is the one part that does not go through declaredChanges: a
// segment both versions declare also carries the constrained positions below it,
// and those are their own changes rather than a member of this one.
func segmentChanges(before, after []localprofile.Segment) []Change {
	left := byKey(before, segmentID)
	right := byKey(after, segmentID)
	var changes []Change
	for _, id := range keys(left, right) {
		earlier, was := left[id]
		later, still := right[id]
		switch {
		case !was:
			changes = append(changes, Change{Part: PartSegment, Kind: KindAdded, Subject: id, Detail: segmentSummary(later)})
		case !still:
			changes = append(changes, Change{Part: PartSegment, Kind: KindRemoved, Subject: id, Detail: segmentSummary(earlier)})
		default:
			if members := segmentMembers(earlier, later); len(members) > 0 {
				changes = append(changes, Change{Part: PartSegment, Kind: KindChanged, Subject: id, Detail: phrase(members)})
			}
			changes = append(changes, fieldChanges(id, earlier.Fields, later.Fields)...)
		}
	}
	return changes
}

func segmentID(segment localprofile.Segment) string { return segment.ID }

func segmentMembers(before, after localprofile.Segment) []string {
	return differingRules(
		rule{"description", before.Description != after.Description},
		rule{"cardinality", !sameCardinality(before.Cardinality, after.Cardinality)},
	)
}

// segmentSummary says what a segment that appeared or disappeared declares. A
// site-defined segment is named as one: no published metadata describes a
// Z-segment, so adding or removing one is always a local decision.
func segmentSummary(segment localprofile.Segment) string {
	kind := "a standard segment"
	if segment.SiteDefined() {
		kind = "a site-defined segment"
	}
	return kind + " constraining " + count(len(segment.Fields), "position", "positions")
}

func fieldChanges(segment string, before, after []localprofile.Field) []Change {
	return declaredChanges(PartField, before, after,
		func(field localprofile.Field) int { return field.Position },
		func(position int) string { return segment + "-" + strconv.Itoa(position) },
		func(field localprofile.Field) string { return "usage " + string(field.Usage) },
		fieldMembers)
}

// fieldMembers names every rule of one constrained position that differs. Each
// rule is answered separately, exactly as localprofile answers each rule's
// origin separately: a changed usage beside an unchanged data type must not
// read as one undifferentiated edit.
func fieldMembers(before, after localprofile.Field) []string {
	return differingRules(
		rule{"name", before.Name != after.Name},
		rule{"usage", before.Usage != after.Usage},
		rule{"condition", !sameCondition(before.Condition, after.Condition)},
		rule{"cardinality", !sameCardinality(before.Cardinality, after.Cardinality)},
		rule{"data type", before.Type != after.Type},
		rule{"terminology set", before.Terminology != after.Terminology},
		rule{"assigning authority", before.Authority != after.Authority},
		rule{"date rule", before.Date != after.Date},
	)
}

func terminologyChanges(before, after []localprofile.TerminologySet) []Change {
	return declaredChanges(PartTerminology, before, after,
		func(set localprofile.TerminologySet) string { return set.ID }, ownID,
		func(set localprofile.TerminologySet) string {
			return "a " + string(set.Binding) + " local code table of " + count(len(set.Codes), "code", "codes")
		},
		func(before, after localprofile.TerminologySet) []string {
			return differingRules(
				rule{"description", before.Description != after.Description},
				rule{"binding", before.Binding != after.Binding},
				rule{"codes", !slices.Equal(before.Codes, after.Codes)},
			)
		})
}

func authorityChanges(before, after []localprofile.Authority) []Change {
	return declaredChanges(PartAuthority, before, after,
		func(authority localprofile.Authority) string { return authority.ID }, ownID,
		func(localprofile.Authority) string { return "an assigning authority" },
		func(before, after localprofile.Authority) []string {
			return differingRules(
				rule{"description", before.Description != after.Description},
				rule{"namespace", before.Namespace != after.Namespace},
				rule{"universal id", before.UniversalID != after.UniversalID},
				rule{"universal id type", before.UniversalIDType != after.UniversalIDType},
			)
		})
}

func dateChanges(before, after []localprofile.DateHandling) []Change {
	return declaredChanges(PartDate, before, after,
		func(date localprofile.DateHandling) string { return date.ID }, ownID,
		func(date localprofile.DateHandling) string { return "a date rule to the " + string(date.Precision) },
		func(before, after localprofile.DateHandling) []string {
			return differingRules(
				rule{"description", before.Description != after.Description},
				rule{"precision", before.Precision != after.Precision},
				rule{"time zone rule", before.TimeZone != after.TimeZone},
			)
		})
}

// ownID is the subject of a part a profile keys by its own declared id.
func ownID(id string) string { return id }

// rule pairs one named member of a part with whether it differs between two
// versions. Every part names what changed through the same pairs, so a member
// that is compared is a member that can be reported.
type rule struct {
	name    string
	differs bool
}

func differingRules(rules ...rule) []string {
	var members []string
	for _, declared := range rules {
		if declared.differs {
			members = append(members, declared.name)
		}
	}
	return members
}

func sameCardinality(before, after *localprofile.Cardinality) bool {
	if before == nil || after == nil {
		return before == after
	}
	return *before == *after
}

func sameCondition(before, after *localprofile.Condition) bool {
	if before == nil || after == nil {
		return before == after
	}
	return before.Segment == after.Segment && before.Position == after.Position &&
		before.Operator == after.Operator && slices.Equal(before.Values, after.Values)
}

// byKey indexes one declared list by the identity its entries carry. A profile
// that reached here went through its own contract, so no key appears twice.
func byKey[T any, K comparable](list []T, key func(T) K) map[K]T {
	indexed := make(map[K]T, len(list))
	for _, entry := range list {
		indexed[key(entry)] = entry
	}
	return indexed
}

// keys are every key either side declares, in order, so the same pair of
// profiles always produces the same changes in the same order.
func keys[T any, K cmp.Ordered](left, right map[K]T) []K {
	all := slices.Collect(maps.Keys(left))
	for key := range right {
		if _, both := left[key]; !both {
			all = append(all, key)
		}
	}
	slices.Sort(all)
	return all
}

// phrase renders the members of one part that differ as the line a person
// reads. A part reported as changed always differs in at least one member, so
// there is no empty case to phrase.
func phrase(members []string) string {
	named := make([]string, 0, len(members))
	for _, member := range members {
		named = append(named, "the "+member)
	}
	if len(named) == 1 {
		return named[0] + " differs"
	}
	return strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1] + " differ"
}

func count(number int, singular, plural string) string {
	if number == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(number) + " " + plural
}
