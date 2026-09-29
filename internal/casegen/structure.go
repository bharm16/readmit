package casegen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/scenario"
)

// structures is HL7 Table 0354's message structure for each trigger event a
// lifecycle profile declares. The same structure holds in every edition that
// defines the event at all; whether an edition does is the pack's decision,
// read from the entry the pack declares for the event itself, which must
// equal the entry of this structure. Nothing here makes an event available.
var structures = map[string]string{
	"ADT^A01": "ADT_A01", "ADT^A04": "ADT_A01", "ADT^A08": "ADT_A01", "ADT^A13": "ADT_A01",
	"ADT^A02": "ADT_A02", "ADT^A03": "ADT_A03", "ADT^A11": "ADT_A09", "ADT^A40": "ADT_A39",
	"SIU^S12": "SIU_S12", "SIU^S13": "SIU_S12", "SIU^S14": "SIU_S12", "SIU^S15": "SIU_S12", "SIU^S26": "SIU_S12",
	"SIU^S18": "SIU_S12", "SIU^S20": "SIU_S12",
	"ORM^O01": "ORM_O01", "ORU^R01": "ORU_R01",
}

// familyOf is the message family each lifecycle profile's events are sent in.
var familyOf = map[scenario.ProfileName]string{
	scenario.ADTLifecycle:         "ADT",
	scenario.SIULifecycle:         "SIU",
	scenario.SIUResourceLifecycle: "SIU",
	scenario.ORMLifecycle:         "ORM",
	scenario.ORULifecycle:         "ORU",
}

// trigger is the MSH-9.2 trigger event a lifecycle event is sent as. An order
// request is an ORM^O01 whatever its order control; a result report is an
// ORU^R01 whatever its status.
func trigger(event scenario.Event) string {
	switch {
	case strings.HasPrefix(string(event), "ORM-"):
		return "O01"
	case strings.HasPrefix(string(event), "ORU-"):
		return "R01"
	}
	return string(event)
}

// Support is one lifecycle event's availability under the selected profile
// and pack: the trigger it is sent as, the message structure that edition
// declares for it, and why an unsupported event is unsupported.
type Support struct {
	Event     string `json:"event"`
	Trigger   string `json:"trigger"`
	Structure string `json:"structure,omitzero"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitzero"`
}

// Support statuses.
const (
	Supported   = "supported"
	Unsupported = "unsupported"
)

// UnsupportedError is a generation refused because the selected profile and
// pack do not support every event the scenario uses. Support names each one.
type UnsupportedError struct{ Support []Support }

func (e *UnsupportedError) Error() string {
	names := []string{}
	for _, s := range e.Support {
		if s.Status == Unsupported {
			names = append(names, s.Event+" ("+s.Reason+")")
		}
	}
	return "the selected profile does not support: " + strings.Join(names, "; ")
}

// target is the selected local profile and pack, read once and held to their
// pins, with the pack's message rules of the profile's version and family.
type target struct {
	profile, pack []byte
	version       string
	family        string
	rules         map[string]*profileeval.MessageRule
	// local is the local profile's own rules by segment and position.
	local map[string]map[int]localprofile.Field
	// variants names each table-bound data type the pack defines exactly as
	// the type it varies (2.4's CE_0278 is CE), which is written as that type.
	variants map[string]string
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// open reads the profile and pack, checks each against its pin and against
// each other, and holds the pack's rules for the profile's version and family.
func open(r Request, lifecycle scenario.ProfileName, profile, pack []byte) (*target, error) {
	if digest(profile) != r.Profile.SHA256 || digest(pack) != r.Pack.SHA256 {
		return nil, errors.New("the profile or pack bytes differ from the request's pins")
	}
	p, err := profileeval.DecodeProfile(profile)
	if err != nil {
		return nil, errors.New("the pinned local profile cannot be read: " + err.Error())
	}
	k, err := profileeval.DecodePack(pack)
	if err != nil {
		return nil, errors.New("the pinned pack cannot be read: " + err.Error())
	}
	if p.Schema != r.Profile.Schema || p.Definition.Identity.ID != r.Profile.ID || p.Definition.Identity.Version != r.Profile.Version {
		return nil, errors.New("the local profile is not the one the request pins")
	}
	if k.Schema != r.Pack.Schema || k.Metadata.Identity.ID != r.Pack.ID || k.Metadata.Identity.Version != r.Pack.Version {
		return nil, errors.New("the pack is not the one the request pins")
	}
	if p.Definition.Base.Pack != k.Metadata.Identity {
		return nil, errors.New("the local profile pins another pack")
	}
	family := familyOf[lifecycle]
	if p.Definition.Base.Family != family {
		return nil, errors.New("the scenario's " + string(lifecycle) + " sends " + family + " messages, and the local profile constrains " + p.Definition.Base.Family)
	}
	t := &target{profile: profile, pack: pack, version: p.Definition.Base.HL7Version, family: family, rules: map[string]*profileeval.MessageRule{}, local: map[string]map[int]localprofile.Field{}, variants: variants(pack)}
	for _, s := range p.Definition.Segments {
		t.local[s.ID] = map[int]localprofile.Field{}
		for _, f := range s.Fields {
			t.local[s.ID][f.Position] = f
		}
	}
	for i := range k.Messages {
		m := &k.Messages[i]
		if m.HL7Version == t.version && m.Family == family {
			t.rules[m.Structure] = m
		}
	}
	return t, nil
}

// support answers one lifecycle event from the pack alone: the pack must
// declare a message rule for the event, under its own name, and that rule must
// be the rule of the structure Table 0354 assigns it. Nothing is borrowed from
// another version or family, and no event is available by default.
func (t *target) support(event scenario.Event) (Support, *profileeval.MessageRule) {
	s := Support{Event: string(event), Trigger: trigger(event), Status: Unsupported}
	name, known := structures[t.family+"^"+s.Trigger]
	if !known {
		s.Reason = "the generator writes no " + t.family + "^" + s.Trigger + " message"
		return s, nil
	}
	own := t.rules[t.family+"_"+s.Trigger]
	if own == nil {
		s.Reason = "the pack declares no " + t.family + "^" + s.Trigger + " message in HL7 " + t.version
		return s, nil
	}
	rule := t.rules[name]
	if rule == nil {
		s.Reason = "the pack declares no " + name + " structure in HL7 " + t.version
		return s, nil
	}
	if own != rule && !sameRule(*own, *rule) {
		s.Reason = "the pack's " + t.family + "_" + s.Trigger + " differs from the " + name + " structure Table 0354 assigns it"
		return s, nil
	}
	if len(rule.Sequence) == 0 {
		s.Reason = "the pack declares no segment sequence for " + name
		return s, nil
	}
	s.Status, s.Structure = Supported, name
	return s, rule
}

// variants reads the pack's table-bound data types, BASE_nnnn, that it defines
// component for component as BASE: the same positions of the same types. A
// variant defined any other way is not one.
func variants(pack []byte) map[string]string {
	var declared struct {
		Datatypes []struct {
			Name       string `json:"name"`
			Components []struct {
				Position int    `json:"position"`
				DataType string `json:"datatype"`
			} `json:"components"`
		} `json:"datatypes"`
	}
	out := map[string]string{}
	if json.Unmarshal(pack, &declared) != nil {
		return out
	}
	shapes := map[string]string{}
	for _, d := range declared.Datatypes {
		shape := ""
		for _, c := range d.Components {
			shape += strconv.Itoa(c.Position) + ":" + c.DataType + ";"
		}
		shapes[d.Name] = shape
	}
	for name, shape := range shapes {
		base, table, ok := strings.Cut(name, "_")
		if ok && len(table) == 4 && strings.Trim(table, "0123456789") == "" && shapes[base] != "" && shapes[base] == shape {
			out[name] = base
		}
	}
	return out
}

func sameRule(a, b profileeval.MessageRule) bool {
	a.Structure, b.Structure = "", ""
	return reflect.DeepEqual(a, b)
}

// supportOf answers every distinct event the scenario uses, in first-use order.
func (t *target) supportOf(w scenario.Workflow) []Support {
	out := []Support{}
	seen := map[scenario.Event]bool{}
	for _, step := range w.Steps {
		if seen[step.Event] {
			continue
		}
		seen[step.Event] = true
		s, _ := t.support(step.Event)
		out = append(out, s)
	}
	return out
}

// fieldRule is the rule a pack declares for one field of one segment of a
// message structure.
func fieldRule(rule *profileeval.MessageRule, id string, position int) (profileeval.FieldRule, bool) {
	for _, s := range rule.Segments {
		if s.ID != id {
			continue
		}
		for _, f := range s.Fields {
			if f.Position == position {
				return f, true
			}
		}
	}
	return profileeval.FieldRule{}, false
}

// place orders the message's segments along the structure's sequence. Each
// segment node takes the segments at the head that it names, up to its
// maximum; a group repeats while its subtree holds the head; a choice takes the
// alternative that holds it. A required segment the scenario model does not
// supply, or a segment the structure has no place for, is refused by name.
func place(sequence []profileeval.Node, units []*segment) ([]*segment, error) {
	p := &placer{units: units}
	if err := p.nodes(sequence); err != nil {
		return nil, err
	}
	if p.at < len(p.units) {
		return nil, errors.New("the structure has no place for " + p.units[p.at].id + " where the scenario model writes it")
	}
	return p.out, nil
}

type placer struct {
	units []*segment
	at    int
	out   []*segment
}

func maximum(max string) int {
	if max == "*" {
		return 1 << 30
	}
	n, err := strconv.Atoi(max)
	if err != nil {
		return 0
	}
	return n
}

func (p *placer) nodes(nodes []profileeval.Node) error {
	for _, n := range nodes {
		if err := p.node(n); err != nil {
			return err
		}
	}
	return nil
}

func (p *placer) head() string {
	if p.at < len(p.units) {
		return p.units[p.at].id
	}
	return ""
}

func holds(n profileeval.Node, id string) bool {
	if n.Segment != "" {
		return n.Segment == id
	}
	return slices.ContainsFunc(n.Children, func(c profileeval.Node) bool { return holds(c, id) })
}

func (p *placer) node(n profileeval.Node) error {
	max := maximum(n.Max)
	if n.Segment != "" {
		count := 0
		for count < max && p.head() == n.Segment {
			p.out = append(p.out, p.units[p.at])
			p.at++
			count++
		}
		if count < n.Min {
			return errors.New("the structure requires " + n.Segment + ", which the scenario model does not supply")
		}
		return nil
	}
	reps := 0
	for reps < max && p.head() != "" && holds(n, p.head()) {
		start := p.at
		var err error
		if n.Choice {
			err = p.choose(n)
		} else {
			err = p.nodes(n.Children)
		}
		if err != nil {
			return err
		}
		if p.at == start {
			break
		}
		reps++
	}
	if reps < n.Min {
		// A required group or choice with nothing to place still needs its
		// required members, which the scenario model does not supply.
		if n.Choice {
			return errors.New("the structure requires one alternative of " + n.Name + ", which the scenario model does not supply")
		}
		return p.nodes(n.Children)
	}
	return nil
}

func (p *placer) choose(n profileeval.Node) error {
	for _, c := range n.Children {
		if holds(c, p.head()) {
			return p.node(c)
		}
	}
	return nil
}

// mustCanonical encodes a value of this package's own types deterministically,
// for digests. Those types always encode; a failure is a defect, never an
// empty digest.
func mustCanonical(v any) []byte {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		panic("casegen: " + err.Error())
	}
	return b
}
