package profileeval

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

// readmit-profile-pack/v5 states each base field and component's edition usage
// code instead of a required boolean, the predicate of a conditional one, its
// length facets and the kind of table it is bound to. It selects evaluator v4.
// A source that cannot tell optional from conditional says "unclassified";
// neither that nor a conditional without a represented predicate can pass.
const PackSchemaV5 = "readmit-profile-pack/v5"
const UsageOperatorVersion = "readmit-profile-evaluator/v4"

var usages = []string{"R", "RE", "O", "C", "B", "W", "X", "unclassified"}

// branches are the usages a condition may select (v2.8.2 section 2.5.3.5,
// C(a/b)); a and b may be the same.
var branches = []string{"R", "RE", "O", "X"}

// Condition is a conditional usage: Then applies while When holds, Else
// otherwise. Sibling operands are positions inside the same datatype or
// segment occurrence, so an absent parent is never evaluated.
type Condition struct {
	When Predicate `json:"when"`
	Then string    `json:"then"`
	Else string    `json:"else"`
}

// Predicate is exactly one of its members. Valued means present and non-null,
// the edition's "populated". An unreadable or ambiguous operand, or an Unknown
// atom for information the message does not carry, leaves it unknown: never
// false, and never a pass.
type Predicate struct {
	Valued     int          `json:"valued,omitzero"`
	NotValued  int          `json:"not_valued,omitzero"`
	ValueIn    *ValueSet    `json:"value_in,omitzero"`
	ValueNotIn *ValueSet    `json:"value_not_in,omitzero"`
	Component  *PartRef     `json:"component,omitzero"`
	Field      *FieldRef    `json:"field,omitzero"`
	Message    *MessagePart `json:"message,omitzero"`
	Repeated   string       `json:"repeated,omitzero"`
	Unknown    string       `json:"unknown,omitzero"`
	All        []Predicate  `json:"all,omitzero"`
	Any        []Predicate  `json:"any,omitzero"`
	Not        *Predicate   `json:"not,omitzero"`
}

// ValueSet compares a sibling's value, or its first component when composite.
type ValueSet struct {
	Position int      `json:"position"`
	Values   []string `json:"values"`
}

// PartRef is one component of a sibling, such as a free-text RXO-6 whose
// first component is left empty.
type PartRef struct {
	Position  int    `json:"position"`
	Component int    `json:"component"`
	State     string `json:"state"`
}

// FieldRef is a field of another segment. Scope "group" names the segment of
// the same group occurrence; it is decided only when the message holds one
// such segment, and is otherwise unknown rather than borrowed from another.
type FieldRef struct {
	Segment  string   `json:"segment"`
	Position int      `json:"position"`
	State    string   `json:"state"`
	Values   []string `json:"values,omitzero"`
	Scope    string   `json:"scope,omitzero"`
}

// MessagePart compares MSH-9's message type, trigger event or structure.
type MessagePart struct {
	Part string   `json:"part"`
	In   []string `json:"in"`
}

// Length keeps the edition's facets apart. "normative" bounds the content by
// Min/Max or an explicit list of Allowed lengths, counted as Count says;
// "conformance-only" states a capacity a receiver must support, never a limit
// on the sender; "not-assigned" is an edition that deliberately states none;
// "unavailable" is information the extraction does not have. Truncation keeps
// the source's marker verbatim: the evaluator never truncates evidence.
type Length struct {
	State       string `json:"state"`
	Min         int    `json:"min,omitzero"`
	Max         int    `json:"max,omitzero"`
	Allowed     []int  `json:"allowed,omitzero"`
	Count       string `json:"count,omitzero"`
	Conformance int    `json:"conformance,omitzero"`
	Truncation  string `json:"truncation,omitzero"`
}

// Length counts. v2.7.1 and v2.8.2 (section 2.7.1) count characters, those
// inside an escape sequence included and its delimiters excluded. v2.3.1-2.6
// count the encoded occurrence with its separators and state no escape rule.
const (
	CountEscapeContent = "characters-escape-content"
	CountEncoded       = "encoded-with-separators"
)

var segmentName = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)

func validateUsage(usage string, c *Condition, l *Length, v5 bool, self, limit int) error {
	if !v5 {
		if usage != "" || c != nil || l != nil {
			return invalid
		}
		return nil
	}
	if !slices.Contains(usages, usage) || c != nil && usage != "C" || l == nil {
		return invalid
	}
	if c != nil {
		nodes := 0
		if !slices.Contains(branches, c.Then) || !slices.Contains(branches, c.Else) || validatePredicate(c.When, self, limit, 0, &nodes) != nil {
			return invalid
		}
	}
	switch l.State {
	case "normative":
		bounded := l.Max > 0 || l.Min > 0
		if bounded == (len(l.Allowed) > 0) || l.Min < 0 || l.Max < 0 || l.Max > 0 && l.Min > l.Max || l.Max > MaxBytes || len(l.Allowed) > 16 || !slices.Contains([]string{CountEscapeContent, CountEncoded}, l.Count) {
			return invalid
		}
		for i, n := range l.Allowed {
			if n < 1 || n > MaxBytes || i > 0 && n <= l.Allowed[i-1] {
				return invalid
			}
		}
	case "conformance-only":
		if l.Min != 0 || l.Max != 0 || l.Allowed != nil || l.Count != "" || l.Conformance < 1 {
			return invalid
		}
	case "not-assigned", "unavailable":
		if l.Min != 0 || l.Max != 0 || l.Allowed != nil || l.Count != "" || l.Conformance != 0 || l.Truncation != "" {
			return invalid
		}
	default:
		return invalid
	}
	if l.Conformance < 0 || l.Conformance > MaxBytes || !slices.Contains([]string{"", "=", "#"}, l.Truncation) || l.Truncation != "" && l.Conformance == 0 {
		return invalid
	}
	return nil
}

func validatePredicate(p Predicate, self, limit, depth int, nodes *int) error {
	*nodes++
	set := 0
	for _, member := range []bool{p.Valued != 0, p.NotValued != 0, p.ValueIn != nil, p.ValueNotIn != nil, p.Component != nil, p.Field != nil, p.Message != nil, p.Repeated != "", p.Unknown != "", p.All != nil, p.Any != nil, p.Not != nil} {
		if member {
			set++
		}
	}
	if depth > 6 || *nodes > 64 || set != 1 {
		return invalid
	}
	sibling := func(position int) bool { return position >= 1 && position <= limit && position != self }
	values := func(list []string) bool {
		if len(list) < 1 || len(list) > 64 {
			return false
		}
		for _, v := range list {
			if v == "" || len(v) > 64 {
				return false
			}
		}
		return true
	}
	switch {
	case p.Valued != 0 && !sibling(p.Valued), p.NotValued != 0 && !sibling(p.NotValued):
		return invalid
	case p.ValueIn != nil && (!sibling(p.ValueIn.Position) || !values(p.ValueIn.Values)),
		p.ValueNotIn != nil && (!sibling(p.ValueNotIn.Position) || !values(p.ValueNotIn.Values)):
		return invalid
	case p.Component != nil && (!sibling(p.Component.Position) || p.Component.Component < 1 || p.Component.Component > 64 || !slices.Contains([]string{"valued", "not_valued"}, p.Component.State)):
		return invalid
	case p.Field != nil:
		f := p.Field
		if !segmentName.MatchString(f.Segment) || f.Position < 1 || f.Position > 999 || !slices.Contains([]string{"", "group"}, f.Scope) {
			return invalid
		}
		switch f.State {
		case "valued", "not_valued":
			if f.Values != nil {
				return invalid
			}
		case "in", "not_in":
			if !values(f.Values) {
				return invalid
			}
		default:
			return invalid
		}
	case p.Message != nil && (!slices.Contains([]string{"type", "trigger", "structure"}, p.Message.Part) || !values(p.Message.In)):
		return invalid
	case p.Repeated != "" && !slices.Contains([]string{"field", "segment"}, p.Repeated):
		return invalid
	case p.Unknown != "" && len(p.Unknown) > 200:
		return invalid
	case p.Not != nil:
		return validatePredicate(*p.Not, self, limit, depth+1, nodes)
	}
	for _, list := range [][]Predicate{p.All, p.Any} {
		if list != nil && len(list) < 2 {
			return invalid
		}
		for _, child := range list {
			if err := validatePredicate(child, self, limit, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

// Every edition's control chapter lets a site extend an HL7 table without
// redefining its values, so an HL7 table is "extensible" and an unfamiliar
// code is no base violation. A user-defined table's published values are
// suggestions. External and imported tables need a pinned vocabulary, and an
// unknown kind cannot be assessed. "closed" exists only for a profile that
// pins and closes a table itself.
func validateBinding(table, kind, policy string, codes []string) error {
	if table == "" {
		if kind != "" || policy != "" || len(codes) > 0 {
			return invalid
		}
		return nil
	}
	if !slices.Contains([]string{"hl7", "user", "external", "imported", "unknown"}, kind) || (kind == "hl7") != (policy != "") ||
		policy != "" && !slices.Contains([]string{"closed", "extensible"}, policy) || policy == "closed" && len(codes) == 0 || kind != "hl7" && len(codes) > 0 {
		return invalid
	}
	return nil
}

// usageOf returns a declaration's effective usage. Before v5 a pack carried a
// required boolean. Upstream sources collapse optional and conditional into
// false, so a base declaration that is neither required nor prohibited is
// unclassified in new evaluations; a local profile's author states optional.
func usageOf(usage string, required, prohibited, base bool) string {
	switch {
	case usage != "":
		return usage
	case required:
		return "R"
	case prohibited:
		return "W"
	case base:
		return "unclassified"
	}
	return "O"
}

// scope locates one conditional element for its predicate: its siblings'
// selector prefix, its own segment and field, whether it is a component.
type scope struct {
	prefix, segment, field string
	component              bool
}

var fieldOf = regexp.MustCompile(`^([A-Z][A-Z0-9]{2})\[(\d+)\]-(\d+)`)

func scopeOf(prefix string, component bool) scope {
	s := scope{prefix: prefix, component: component}
	if m := fieldOf.FindStringSubmatch(prefix); m != nil {
		s.segment, s.field = m[1], m[1]+"["+m[2]+"]-"+m[3]
	} else if len(prefix) >= 3 {
		s.segment = prefix[:3]
	}
	return s
}

// predicate evaluates p: 1 holds, 0 does not, -1 unknown.
func (e *evaluator) predicate(p Predicate, s scope) int {
	truth := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	switch {
	case p.Valued != 0 || p.NotValued != 0:
		r := e.read(s.prefix + strconv.Itoa(max(p.Valued, p.NotValued)))
		if r.Reason != "" {
			return -1
		}
		return truth((r.State == hl7.Present) == (p.Valued != 0))
	case p.ValueIn != nil || p.ValueNotIn != nil:
		set, in := p.ValueIn, true
		if set == nil {
			set, in = p.ValueNotIn, false
		}
		r := e.read(s.prefix + strconv.Itoa(set.Position))
		if r.Reason != "" {
			return -1
		}
		if !s.component && r.State == hl7.Present {
			// A composite field compares its first component.
			if first := e.read(s.prefix + strconv.Itoa(set.Position) + ".1"); first.Reason == "" {
				r = first
			}
		}
		found := r.State == hl7.Present && slices.Contains(set.Values, string(r.Decoded))
		return truth(found == in)
	case p.Component != nil:
		r := e.read(s.prefix + strconv.Itoa(p.Component.Position) + "." + strconv.Itoa(p.Component.Component))
		if r.Reason != "" {
			return -1
		}
		return truth((r.State == hl7.Present) == (p.Component.State == "valued"))
	case p.Field != nil:
		f := p.Field
		count := 0
		for _, seg := range e.doc.Messages[0].Segments {
			if seg.ID == f.Segment {
				count++
			}
		}
		if count > 1 {
			return -1
		}
		valued, value := false, ""
		if count == 1 {
			r := e.read(fmt.Sprintf("%s-%d", f.Segment, f.Position))
			if r.Reason != "" {
				return -1
			}
			valued = r.State == hl7.Present
			if first := e.read(fmt.Sprintf("%s-%d.1", f.Segment, f.Position)); valued && first.Reason == "" && first.State == hl7.Present {
				value = string(first.Decoded)
			}
		}
		switch f.State {
		case "valued":
			return truth(valued)
		case "not_valued":
			return truth(!valued)
		case "in":
			return truth(valued && slices.Contains(f.Values, value))
		default:
			return truth(!valued || !slices.Contains(f.Values, value))
		}
	case p.Message != nil:
		part := map[string]string{"type": "MSH-9.1", "trigger": "MSH-9.2", "structure": "MSH-9.3"}[p.Message.Part]
		r := e.read(part)
		if r.Reason != "" || r.State != hl7.Present {
			return -1
		}
		return truth(slices.Contains(p.Message.In, string(r.Decoded)))
	case p.Repeated == "segment":
		count := 0
		for _, seg := range e.doc.Messages[0].Segments {
			if seg.ID == s.segment {
				count++
			}
		}
		return truth(count > 1)
	case p.Repeated == "field":
		if !s.component || s.field == "" {
			return -1
		}
		m := fieldOf.FindStringSubmatch(s.field)
		occ, _ := strconv.Atoi(m[2])
		position, _ := strconv.Atoi(m[3])
		at := 0
		for _, seg := range e.doc.Messages[0].Segments {
			if seg.ID == m[1] {
				if at++; at == occ {
					field := seg.Field(position)
					return truth(field.State != hl7.Omitted && len(field.Repetitions) > 1)
				}
			}
		}
		return -1
	case p.Unknown != "":
		return -1
	case p.Not != nil:
		if v := e.predicate(*p.Not, s); v >= 0 {
			return 1 - v
		}
		return -1
	case p.All != nil:
		result := 1
		for _, child := range p.All {
			switch e.predicate(child, s) {
			case 0:
				return 0
			case -1:
				result = -1
			}
		}
		return result
	default:
		result := 0
		for _, child := range p.Any {
			switch e.predicate(child, s) {
			case 1:
				return 1
			case -1:
				result = -1
			}
		}
		return result
	}
}

// usage applies a v5 declaration's usage to one reading; s addresses its
// siblings. Unclassified usage is reported once per occurrence by the caller.
func (e *evaluator) usage(usage string, c *Condition, kind string, s scope, selector, origin string, r hl7.Reading) {
	if usage == "C" {
		if c == nil {
			e.add("conditional-predicate-unrepresented-"+kind, origin, "unsupported", selector, r)
			return
		}
		switch e.predicate(c.When, s) {
		case -1:
			e.add("conditional-predicate-unknown-"+kind, origin, "undecided", selector, r)
			return
		case 1:
			usage = c.Then
		default:
			usage = c.Else
		}
		kind = "conditional-" + kind
	}
	switch usage {
	case "R":
		if r.State != hl7.Present {
			e.add("required-"+kind, origin, "fail", selector, r)
		}
	case "W", "X":
		if r.State == hl7.Present || r.State == hl7.Null {
			e.add("prohibited-"+kind, origin, "fail", selector, r)
		}
	}
}

// length checks a normative bound. Under the escape-content count the result
// is exact. Under the encoded count the edition states no escape rule, so the
// value is bounded by counting each escape sequence as one character and as
// all its characters; a value inside one bound and outside the other is
// undecided, never passed.
func (e *evaluator) length(l *Length, kind, selector, origin string, r hl7.Reading) {
	if l == nil || l.State != "normative" {
		return
	}
	raw := e.doc.Bytes(r.Span)
	escape := e.doc.Messages[0].Delimiters.Escape
	characters := func(b []byte) int {
		if utf8.Valid(b) {
			return utf8.RuneCount(b)
		}
		return len(b)
	}
	most := characters(raw)
	least := most
	if l.Count == CountEscapeContent {
		for _, b := range raw {
			if b == escape {
				least--
			}
		}
		most = least
	} else {
		inside, sequence := false, 0
		for _, b := range raw {
			switch {
			case b == escape && inside:
				least -= sequence + 1
				inside = false
			case b == escape:
				inside, sequence = true, 0
			case inside:
				sequence++
			}
		}
	}
	someWithin, allWithin := false, true
	if len(l.Allowed) > 0 {
		for _, n := range l.Allowed {
			someWithin = someWithin || n >= least && n <= most
		}
		allWithin = least == most && slices.Contains(l.Allowed, least)
	} else {
		someWithin = most >= l.Min && (l.Max == 0 || least <= l.Max)
		allWithin = least >= l.Min && (l.Max == 0 || most <= l.Max)
	}
	switch {
	case !someWithin:
		e.add(kind+"-length", origin, "fail", selector, r)
	case !allWithin:
		e.add(kind+"-length-unit-undetermined", origin, "undecided", selector, r)
	}
}

// binding checks a present value against its v5 table binding; kind names
// the element ("component" or "field") in the finding.
func (e *evaluator) binding(table, tableKind, policy string, codes []string, kind, selector, origin string, r hl7.Reading) {
	switch {
	case table == "" || tableKind == "user":
	case tableKind == "hl7":
		if policy == "closed" && !slices.Contains(codes, string(r.Decoded)) {
			e.add(kind+"-code-set", origin, "fail", selector, r)
		}
	default:
		e.add(fmt.Sprintf("%s-terminology-%s-unavailable-%s", kind, tableKind, table), origin, "unsupported", selector, r)
	}
}
