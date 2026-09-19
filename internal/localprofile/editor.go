package localprofile

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
)

// Editor holds one local profile being edited. Every operation is a typed
// change to one named part of the document, and every operation is checked
// against the whole contract before it is kept: a refused change leaves the
// profile exactly as it was, so a half-applied edit is not a state this
// surface has. Nothing here reads or writes a file; Encode is the only way a
// profile leaves an editor, and abandoning an editor discards every change.
type Editor struct {
	opened  Profile
	current Profile
}

// NewEditor begins a local profile for one pinned pack and one combination.
// The identity and the pinned combination are held to the contract here, so a
// profile that could never be written is refused at the point the mistake was
// made rather than at every edit after it. The new profile holds no segment
// yet, so it is not a document until one is added: Encode refuses it by name,
// exactly as Decode would refuse the same bytes.
func NewEditor(identity Identity, base Base) (*Editor, error) {
	profile := Profile{Schema: Schema, Identity: identity, Base: base}
	if err := profile.check(false); err != nil {
		return nil, err
	}
	return &Editor{opened: profile, current: profile}, nil
}

// Open begins editing a profile that already exists. The profile is held to
// the whole contract first, so an editor never opens a document a reader would
// have refused.
func Open(profile Profile) (*Editor, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	opened := profile.clone()
	opened.canonicalize()
	return &Editor{opened: opened, current: opened.clone()}, nil
}

// Profile is the profile as it stands, copied, so a caller that keeps it
// cannot reach back into the editor through a shared slice.
func (e *Editor) Profile() Profile { return e.current.clone() }

// Changed reports whether the profile differs from the one the editor opened.
func (e *Editor) Changed() bool { return !equal(e.opened, e.current) }

// Revert discards every change and returns to the profile the editor opened.
// It is the cancel of this surface: nothing was written, so nothing is undone
// anywhere else.
func (e *Editor) Revert() { e.current = e.opened.clone() }

// Encode writes the profile as the contract's JSON. It holds the profile to
// the whole contract first, including the first segment an editor may still be
// about to add, so the bytes this returns are bytes Decode accepts. Members are
// written in the order the contract declares them and every list is in its
// canonical order, so the same profile is always the same bytes.
func (e *Editor) Encode() ([]byte, error) {
	if err := e.current.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(e.current, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("a local profile could not be written as JSON")
	}
	return append(data, '\n'), nil
}

// SetSegment adds one constrained segment or replaces the one with the same
// id. A site-defined Z-segment and a standard segment are the same operation
// here: the profile records what a team decided about a segment, and which of
// the two it is follows from the identifier rather than from a separate kind.
func (e *Editor) SetSegment(segment Segment) error {
	return e.apply(func(p *Profile) {
		p.Segments = replace(p.Segments, segment, func(s Segment) string { return s.ID })
	})
}

// RemoveSegment removes one constrained segment. Removing a segment a
// condition elsewhere names is allowed: a condition names a position of a
// message, not a rule of this profile.
func (e *Editor) RemoveSegment(id string) error {
	if indexOf(e.current.Segments, id, func(s Segment) string { return s.ID }) < 0 {
		return errors.New("this profile does not constrain the segment " + id)
	}
	return e.apply(func(p *Profile) {
		p.Segments = remove(p.Segments, id, func(s Segment) string { return s.ID })
	})
}

// SetField adds one field rule to a constrained segment or replaces the rule
// at the same position.
func (e *Editor) SetField(segmentID string, field Field) error {
	at := indexOf(e.current.Segments, segmentID, func(s Segment) string { return s.ID })
	if at < 0 {
		return errors.New("this profile does not constrain the segment " + segmentID)
	}
	return e.apply(func(p *Profile) {
		p.Segments[at].Fields = replace(p.Segments[at].Fields, field, func(f Field) int { return f.Position })
	})
}

// RemoveField removes one field rule. The last rule of a segment cannot be
// removed on its own, because a segment that constrains no position is not a
// rule: remove the segment instead.
func (e *Editor) RemoveField(segmentID string, position int) error {
	at := indexOf(e.current.Segments, segmentID, func(s Segment) string { return s.ID })
	if at < 0 {
		return errors.New("this profile does not constrain the segment " + segmentID)
	}
	if indexOf(e.current.Segments[at].Fields, position, func(f Field) int { return f.Position }) < 0 {
		return errors.New("this profile does not constrain that position of " + segmentID)
	}
	if len(e.current.Segments[at].Fields) == 1 {
		return errors.New("this is the only position " + segmentID + " constrains, so removing it removes the segment: remove the segment instead")
	}
	return e.apply(func(p *Profile) {
		p.Segments[at].Fields = remove(p.Segments[at].Fields, position, func(f Field) int { return f.Position })
	})
}

// SetTerminology declares one local code table or replaces the one with the
// same id.
func (e *Editor) SetTerminology(set TerminologySet) error {
	return e.apply(func(p *Profile) {
		p.Terminology = replace(p.Terminology, set, func(s TerminologySet) string { return s.ID })
	})
}

// RemoveTerminology removes one local code table. A table a field still binds
// to cannot be removed: the change is refused and the profile is unchanged,
// rather than leaving a rule pointing at nothing.
func (e *Editor) RemoveTerminology(id string) error {
	if indexOf(e.current.Terminology, id, func(s TerminologySet) string { return s.ID }) < 0 {
		return errors.New("this profile does not declare the terminology set " + id)
	}
	return e.apply(func(p *Profile) {
		p.Terminology = remove(p.Terminology, id, func(s TerminologySet) string { return s.ID })
	})
}

// SetAuthority declares one assigning authority or replaces the one with the
// same id.
func (e *Editor) SetAuthority(authority Authority) error {
	return e.apply(func(p *Profile) {
		p.Authorities = replace(p.Authorities, authority, func(a Authority) string { return a.ID })
	})
}

// RemoveAuthority removes one assigning authority. One a field still names
// cannot be removed.
func (e *Editor) RemoveAuthority(id string) error {
	if indexOf(e.current.Authorities, id, func(a Authority) string { return a.ID }) < 0 {
		return errors.New("this profile does not declare the authority " + id)
	}
	return e.apply(func(p *Profile) {
		p.Authorities = remove(p.Authorities, id, func(a Authority) string { return a.ID })
	})
}

// SetDate declares one date-handling rule or replaces the one with the same id.
func (e *Editor) SetDate(date DateHandling) error {
	return e.apply(func(p *Profile) {
		p.Dates = replace(p.Dates, date, func(d DateHandling) string { return d.ID })
	})
}

// RemoveDate removes one date-handling rule. One a field still names cannot be
// removed.
func (e *Editor) RemoveDate(id string) error {
	if indexOf(e.current.Dates, id, func(d DateHandling) string { return d.ID }) < 0 {
		return errors.New("this profile does not declare the date rule " + id)
	}
	return e.apply(func(p *Profile) {
		p.Dates = remove(p.Dates, id, func(d DateHandling) string { return d.ID })
	})
}

// apply makes one change on a copy, puts the result back in canonical order,
// holds it to every rule of the contract a profile under construction is held
// to, and keeps it only if it passed. Ordering here rather than at Encode is
// what makes the editor's profile canonical at every instant, so Encode reads
// it without changing it and the order edits arrived in is never visible. The
// editor is therefore never in a state a completed document could not be
// reached from, and a refused change is a change that did not happen.
func (e *Editor) apply(change func(*Profile)) error {
	candidate := e.current.clone()
	change(&candidate)
	candidate.canonicalize()
	if err := candidate.check(false); err != nil {
		return err
	}
	e.current = candidate
	return nil
}

// replace puts one entry into a list by key: the entry with this key is
// replaced where it stands, and a new one is appended. apply then puts the
// list back in canonical order, so this does not have to know what that is.
func replace[T any, K comparable](list []T, entry T, key func(T) K) []T {
	if at := indexOf(list, key(entry), key); at >= 0 {
		list[at] = entry
		return list
	}
	return append(list, entry)
}

func remove[T any, K comparable](list []T, target K, key func(T) K) []T {
	return slices.DeleteFunc(list, func(entry T) bool { return key(entry) == target })
}

func indexOf[T any, K comparable](list []T, target K, key func(T) K) int {
	return slices.IndexFunc(list, func(entry T) bool { return key(entry) == target })
}

// canonicalize sorts every list in place into the one order the contract
// writes it in: segments by identifier, fields by position, and every declared
// set by its id. Ordering is not meaning here — this contract version
// constrains named segments and says nothing about where they appear in a
// message — so the same profile is always the same bytes whatever order its
// parts were added in.
func (p *Profile) canonicalize() {
	slices.SortFunc(p.Segments, func(a, b Segment) int { return cmp.Compare(a.ID, b.ID) })
	for i := range p.Segments {
		slices.SortFunc(p.Segments[i].Fields, func(a, b Field) int { return cmp.Compare(a.Position, b.Position) })
	}
	slices.SortFunc(p.Terminology, func(a, b TerminologySet) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(p.Authorities, func(a, b Authority) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(p.Dates, func(a, b DateHandling) int { return cmp.Compare(a.ID, b.ID) })
}

// clone copies every list, so an edited profile and the profile it came from
// never share a slice.
func (p Profile) clone() Profile {
	copied := p
	copied.Terminology = slices.Clone(p.Terminology)
	for i, set := range copied.Terminology {
		copied.Terminology[i].Codes = slices.Clone(set.Codes)
	}
	copied.Authorities = slices.Clone(p.Authorities)
	copied.Dates = slices.Clone(p.Dates)
	copied.Segments = slices.Clone(p.Segments)
	for i, segment := range copied.Segments {
		copied.Segments[i].Cardinality = cloneCardinality(segment.Cardinality)
		copied.Segments[i].Fields = slices.Clone(segment.Fields)
		for j, field := range copied.Segments[i].Fields {
			copied.Segments[i].Fields[j].Cardinality = cloneCardinality(field.Cardinality)
			if field.Condition != nil {
				condition := *field.Condition
				condition.Values = slices.Clone(field.Condition.Values)
				copied.Segments[i].Fields[j].Condition = &condition
			}
		}
	}
	return copied
}

func cloneCardinality(cardinality *Cardinality) *Cardinality {
	if cardinality == nil {
		return nil
	}
	copied := *cardinality
	return &copied
}

// equal compares two profiles by what they declare, following the pointers a
// rule may hold rather than the addresses they happen to have.
func equal(a, b Profile) bool {
	if a.Schema != b.Schema || a.Identity != b.Identity || a.Base != b.Base {
		return false
	}
	if !slices.EqualFunc(a.Terminology, b.Terminology, func(x, y TerminologySet) bool {
		return x.ID == y.ID && x.Description == y.Description && x.Binding == y.Binding && slices.Equal(x.Codes, y.Codes)
	}) {
		return false
	}
	if !slices.Equal(a.Authorities, b.Authorities) || !slices.Equal(a.Dates, b.Dates) {
		return false
	}
	return slices.EqualFunc(a.Segments, b.Segments, func(x, y Segment) bool {
		return x.ID == y.ID && x.Description == y.Description &&
			equalCardinality(x.Cardinality, y.Cardinality) &&
			slices.EqualFunc(x.Fields, y.Fields, equalField)
	})
}

func equalField(a, b Field) bool {
	if a.Position != b.Position || a.Name != b.Name || a.Usage != b.Usage || a.Type != b.Type ||
		a.Terminology != b.Terminology || a.Authority != b.Authority || a.Date != b.Date {
		return false
	}
	if !equalCardinality(a.Cardinality, b.Cardinality) {
		return false
	}
	if (a.Condition == nil) != (b.Condition == nil) {
		return false
	}
	return a.Condition == nil || a.Condition.Segment == b.Condition.Segment &&
		a.Condition.Position == b.Condition.Position &&
		a.Condition.Operator == b.Condition.Operator &&
		slices.Equal(a.Condition.Values, b.Condition.Values)
}

func equalCardinality(a, b *Cardinality) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || *a == *b
}
