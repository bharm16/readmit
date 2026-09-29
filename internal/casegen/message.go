package casegen

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/scenario"
)

// Data types each written field is rendered for. A field whose edition types
// it otherwise is not written, and an essential one refuses the message.
var (
	timestampTypes  = []string{"TS", "DTM"}
	codeTypes       = []string{"CE", "CWE", "CNE"}
	classTypes      = []string{"IS", "CWE"}
	identifierTypes = []string{"CX"}
	entityTypes     = []string{"EI"}
	nameTypes       = []string{"XPN"}
	personTypes     = []string{"XCN"}
	locationTypes   = []string{"PL"}
	numberTypes     = []string{"NM"}
	setTypes        = []string{"SI"}
	textTypes       = []string{"ST", "TX", "FT", "varies"}
	controlTypes    = []string{"ID"}
	idTypes         = []string{"ID", "IS", "CWE", "CNE", "CE"}
)

// fillerStatus is the Table 0278 status each scheduling event reports for its
// appointment (SCH-25) and, but for a cancelled participation, its resources.
var fillerStatus = map[scenario.Event]string{"S12": "Booked", "S13": "Booked", "S14": "Booked", "S15": "Cancelled", "S26": "Noshow", "S18": "Booked", "S20": "Booked"}

// updating are the scheduling events chapter 10 treats as updating or
// modifying an appointment's resource group, for which it requires the
// segment action code.
var updating = map[scenario.Event]bool{"S13": true, "S14": true, "S18": true, "S20": true}

// resourceChange is one resource segment a message sends: the resource, its
// segment action code (empty where none is written) and its filler status.
type resourceChange struct {
	resource Resource
	action   string
	status   string
}

// resourceChanges is what a scheduling step says of its appointment's
// resources, in the declared update mode (HL7 v2.5.1 section 2.10.4).
//
// In action-code mode an updating event sends only what it changes: an
// addition (S18) sends the added resource with A; a cancellation of
// participation (S20) sends the cancelled resource with U and the filler
// status Cancelled, because its participation is stopped, not an erroneous
// entry deleted (D, S22); a reschedule (S13) restates every booked resource
// with U, since their slot is the appointment's; a modification (S14) changes
// no resource and sends none. A booking, a cancellation or a no-show of the
// appointment sends every booked resource without an action code.
//
// In snapshot mode every message states the complete list without action
// codes: every resource that participates, with its filler status, a
// cancelled participation included as Cancelled, so it is never read as
// deleted. A list with no resource is refused: snapshot mode states that with
// a delete-all segment the model never writes.
func (m *model) resourceChanges(step scenario.Step, appointmentID string, f stepFacts, change alteration) ([]resourceChange, error) {
	snapshot := m.r.Wire.ResourceUpdates == SnapshotUpdates
	switch {
	case !snapshot && step.Event == "S18":
		return []resourceChange{{m.subjectResource(step.Subject, change), "A", "Booked"}}, nil
	case !snapshot && step.Event == "S20":
		return []resourceChange{{m.subjectResource(step.Subject, change), "U", "Cancelled"}}, nil
	case !snapshot && step.Event == "S14":
		return []resourceChange{}, nil
	}
	status, action := fillerStatus[step.Event], ""
	if !snapshot && updating[step.Event] {
		action = "U"
	}
	out := []resourceChange{}
	for _, r := range m.appts[appointmentID].Resources {
		out = append(out, resourceChange{r, action, status})
	}
	for _, s := range m.w.Subjects {
		if s.Appointment != appointmentID {
			continue
		}
		switch r := m.subjectResource(s.ID, change); {
		case s.ID == step.Subject && step.Event == "S18":
			out = append(out, resourceChange{r, "", "Booked"})
		case s.ID == step.Subject && step.Event == "S20":
			out = append(out, resourceChange{r, "", "Cancelled"})
		case f.participation[s.ID] == scenario.ResourceBooked:
			out = append(out, resourceChange{r, action, status})
		case snapshot && f.participation[s.ID] == scenario.ResourceCancelled:
			out = append(out, resourceChange{r, "", "Cancelled"})
		}
	}
	if snapshot && len(out) == 0 {
		return nil, errors.New("an appointment without resources cannot be written in snapshot mode, which states that with a delete-all segment the scenario model does not write")
	}
	return out, nil
}

// subjectResource is a resource subject as its messages write it: the
// identifier and coding system it declares, as this step's variant alters
// them, with its binding's kind, text and role.
func (m *model) subjectResource(id string, change alteration) Resource {
	s, b := m.subjects[id], m.resources[id]
	code, system := s.Identifier, s.Namespace
	if v, ok := change.identifiers[id]; ok {
		code = v
	}
	if v, ok := change.namespaces[id]; ok {
		system = v
	}
	return Resource{ID: id, Kind: b.Kind, Identifier: Code{Code: code, Text: b.Text, System: system}, Role: b.Role, IdentifierType: b.IdentifierType, NameType: b.NameType}
}

// slotFacts is an appointment's slot: minutes after the base time and length.
type slotFacts struct {
	start    time.Duration
	duration int
}

// carried is what the bound subjects hold at one point of the sequence. A step
// that the lifecycle takes carries its edit to every later step; a refused
// step's edit is written in its own message only, because a refused step
// changes nothing.
type carried struct {
	names     map[string][2]string
	locations map[string]Location
	slots     map[string]slotFacts
	// participation is each resource subject's lifecycle state.
	participation map[string]scenario.State
}

func (c carried) clone() carried {
	out := carried{names: map[string][2]string{}, locations: map[string]Location{}, slots: map[string]slotFacts{}, participation: map[string]scenario.State{}}
	for k, v := range c.names {
		out.names[k] = v
	}
	for k, v := range c.locations {
		out.locations[k] = v
	}
	for k, v := range c.slots {
		out.slots[k] = v
	}
	for k, v := range c.participation {
		out.participation[k] = v
	}
	return out
}

// stepFacts is one step's view: the facts it writes and, for a transfer, the
// location it moves from.
type stepFacts struct {
	carried
	prior *Location
}

// model is the request read against its scenario: bindings by subject and the
// facts every step writes, for one row.
type model struct {
	r         Request
	w         scenario.Workflow
	timeline  scenario.Timeline
	subjects  map[string]scenario.Subject
	patients  map[string]PatientBinding
	visits    map[string]VisitBinding
	appts     map[string]AppointmentBinding
	resources map[string]ResourceBinding
	orders    map[string]OrderBinding
	placers   map[string]scenario.Order
	results   map[string]scenario.Result
	edits     map[string]Edit
	outcomes  map[string]scenario.Occurrence
	delimiter delimiters
}

func newModel(r Request, w scenario.Workflow) (*model, error) {
	timeline, err := w.Preview()
	if err != nil {
		return nil, err
	}
	m := &model{r: r, w: w, timeline: timeline, subjects: map[string]scenario.Subject{}, patients: map[string]PatientBinding{}, visits: map[string]VisitBinding{},
		appts: map[string]AppointmentBinding{}, resources: map[string]ResourceBinding{}, orders: map[string]OrderBinding{}, placers: map[string]scenario.Order{}, results: map[string]scenario.Result{},
		edits: map[string]Edit{}, outcomes: map[string]scenario.Occurrence{}, delimiter: declared(r.Wire.Delimiters)}
	for _, s := range w.Subjects {
		m.subjects[s.ID] = s
	}
	for _, p := range r.Bindings.Patients {
		m.patients[p.Subject] = p
	}
	for _, v := range r.Bindings.Visits {
		m.visits[v.Subject] = v
	}
	for _, a := range r.Bindings.Appointments {
		m.appts[a.Subject] = a
	}
	for _, rb := range r.Bindings.Resources {
		m.resources[rb.Subject] = rb
	}
	for _, o := range r.Bindings.Orders {
		m.orders[o.Subject] = o
	}
	for _, o := range w.Orders {
		m.placers[o.Subject] = o
	}
	for _, res := range w.Results {
		m.results[res.Step] = res
	}
	for _, e := range r.Bindings.Edits {
		m.edits[e.Step] = e
	}
	for _, o := range timeline.Steps {
		m.outcomes[o.ID] = o
	}
	return m, nil
}

// facts computes, in authored order, what each step writes for one row.
// Arrival order never changes it: a reordered step still says what it said.
func (m *model) facts(row Row) map[string]stepFacts {
	c := carried{names: map[string][2]string{}, locations: map[string]Location{}, slots: map[string]slotFacts{}, participation: map[string]scenario.State{}}
	for _, s := range m.w.Subjects {
		switch s.Kind {
		case scenario.PatientSubject:
			c.names[s.ID] = [2]string{row.Family, row.Given}
		case scenario.ResourceSubject:
			c.participation[s.ID] = s.InitialState
		}
	}
	for id, v := range m.visits {
		c.locations[id] = v.Location
	}
	for id, a := range m.appts {
		start, _ := time.ParseDuration(a.Start)
		c.slots[id] = slotFacts{start: start, duration: a.Duration}
	}
	out := map[string]stepFacts{}
	for _, step := range m.w.Steps {
		view := stepFacts{carried: c.clone()}
		if e, ok := m.edits[step.ID]; ok {
			switch e.Op {
			case RenameEdit:
				view.names[m.subjects[step.Subject].Patient] = [2]string{e.Family, *e.Given}
			case RelocateEdit:
				prior := view.locations[step.Subject]
				view.prior = &prior
				view.locations[step.Subject] = *e.Location
			case RescheduleEdit:
				start, _ := time.ParseDuration(e.Start)
				view.slots[step.Subject] = slotFacts{start: start, duration: e.Duration}
			}
		}
		out[step.ID] = view
		if step.Expect == scenario.Accepted {
			c = view.carried.clone()
			// A resource's participation is what the lifecycle made of it.
			if m.subjects[step.Subject].Kind == scenario.ResourceSubject {
				c.participation[step.Subject] = m.outcomes[step.ID].To
			}
		}
	}
	return out
}

// alteration is a negative variant's change to what one step's message says.
type alteration struct {
	identifiers map[string]string
	namespaces  map[string]string
	unlink      bool
}

// writer puts one field where the selected edition defines the position and
// admits a value of the type the generator writes, and the local profile does
// not exclude it. A position neither defines, one the edition withdraws or the
// site marks not supported, or one typed otherwise is left unwritten, and an
// essential one refuses the message by name.
type writer struct {
	rule     *profileeval.MessageRule
	version  string
	local    map[string]map[int]localprofile.Field
	variants map[string]string
	err      error
}

func (w *writer) put(s *segment, position int, value *field, essential bool, types []string) {
	if w.err != nil || value == nil {
		return
	}
	where := s.id + "-" + strconv.Itoa(position)
	refuse := func(reason string) {
		if essential {
			w.err = errors.New(where + reason)
		}
	}
	rule, ok := fieldRule(w.rule, s.id, position)
	site, localized := w.local[s.id][position]
	dataType := rule.DataType
	if localized && site.Type != "" {
		dataType = string(site.Type)
	}
	switch {
	case localized && site.Usage == localprofile.UsageNotSupported:
		refuse(" is not supported by the local profile")
	case !ok && !localized:
		refuse(" is not a field of " + w.rule.Structure + " in HL7 " + w.version)
	case ok && (rule.Usage == "X" || rule.Usage == "W"):
		refuse(" is not supported in HL7 " + w.version)
	case len(types) > 0 && !slices.Contains(types, dataType) && !slices.Contains(types, w.variants[dataType]):
		refuse(" is " + dataType + " in HL7 " + w.version + "; the generator writes " + strings.Join(types, " or "))
	default:
		s.set(position, value)
	}
}

// built is one step's message before encoding.
type built struct {
	step     scenario.Step
	segments []*segment
	charset  string
	at       time.Time
	control  string
	keys     []BusinessKey
}

// BusinessKey is one namespace-qualified identity a message carries.
type BusinessKey struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// controlID is MSH-10: C and the first 16 hexadecimal digits, uppercase, of
// the SHA-256 of the generator version, seed, scenario id, scenario version,
// row and step, joined by NUL. A row's step keeps its control ID under every
// variant, so a retransmission repeats it and a reordered or delayed step
// keeps it; another seed, scenario or row changes it.
func controlID(seed uint64, scenario scenario.Identity, row, step string) string {
	sum := digest([]byte(Version + "\x00" + strconv.FormatUint(seed, 10) + "\x00" + scenario.ID + "\x00" + scenario.Version + "\x00" + row + "\x00" + step))
	return "C" + strings.ToUpper(sum[:16])
}

// message builds one step's segments in the order the scenario model writes
// them, then places them along the selected structure.
func (m *model) message(t *target, rule *profileeval.MessageRule, support Support, step scenario.Step, row Row, f stepFacts, charset, offset string, change alteration) (built, error) {
	outcome := m.outcomes[step.ID]
	at := outcome.At
	w := &writer{rule: rule, version: t.version, local: t.local, variants: t.variants}
	when := func(instant time.Time) *field { return plain(stamp(instant, m.r.Wire.Precision, offset)) }
	control := controlID(m.r.Seed, m.w.Identity(), row.ID, step.ID)
	b := built{step: step, charset: charset, at: at, control: control}

	identity := func(subject string) (string, string) {
		s := m.subjects[subject]
		id, ns := s.Identifier, s.Namespace
		if v, ok := change.identifiers[subject]; ok {
			id = v
		}
		if v, ok := change.namespaces[subject]; ok {
			ns = v
		}
		return id, ns
	}
	cx := func(id, ns, kind string) *field { return components(id, "", "", ns, kind) }
	ei := func(id, ns string) *field { return components(id, ns) }
	code := func(c Code) *field { return components(c.Code, c.Text, c.System) }
	person := func(p Person) *field {
		return components(p.ID, p.Family, p.Given, "", "", "", "", "", p.Authority, p.NameType, "", "", p.IDType)
	}
	location := func(l Location) *field { return components(l.PointOfCare, l.Room, l.Bed) }

	msh := newSegment("MSH")
	w.put(msh, 3, plain(m.r.Wire.Sending.Application), false, nil)
	w.put(msh, 4, plain(m.r.Wire.Sending.Facility), false, nil)
	w.put(msh, 5, plain(m.r.Wire.Receiving.Application), false, nil)
	w.put(msh, 6, plain(m.r.Wire.Receiving.Facility), false, nil)
	w.put(msh, 7, when(at), true, timestampTypes)
	messageType := components(t.family, support.Trigger, support.Structure)
	if !admits(rule, "MSH", 9, t.family+"^"+support.Trigger+"^"+support.Structure) {
		// 2.3.1's MSH-9 holds seven characters: the type and trigger alone.
		messageType = components(t.family, support.Trigger)
	}
	w.put(msh, 9, messageType, true, nil)
	w.put(msh, 10, plain(control), true, []string{"ST"})
	w.put(msh, 11, plain(m.r.Wire.ProcessingID), true, nil)
	w.put(msh, 12, plain(t.version), true, nil)
	w.put(msh, 18, plain(charset), true, nil)
	units := []*segment{msh}

	subject := m.subjects[step.Subject]
	// The appointment a resource event is sent for; every other subject's
	// message is about the subject itself.
	appointmentID := step.Subject
	if subject.Kind == scenario.ResourceSubject {
		appointmentID = subject.Appointment
		subject = m.subjects[appointmentID]
	}
	patientID := subject.ID
	if subject.Kind != scenario.PatientSubject {
		patientID = subject.Patient
	}
	if step.Into != "" {
		patientID = step.Into
	}
	pid := newSegment("PID")
	w.put(pid, 1, plain("1"), false, setTypes)
	id, ns := identity(patientID)
	ids := []*field{cx(id, ns, m.patients[patientID].IdentifierType)}
	for _, extra := range m.patients[patientID].Additional {
		ids = append(ids, cx(extra.Identifier, extra.Namespace, extra.Type))
	}
	w.put(pid, 3, repeated(ids...), true, identifierTypes)
	name := f.names[patientID]
	w.put(pid, 5, components(name[0], name[1]), true, nameTypes)
	b.keys = append(b.keys, BusinessKey{Kind: "patient", Namespace: ns, Value: id})
	if len(row.Notes) > 0 && !holdsSegment(rule.Sequence, "NTE") {
		return built{}, errors.New("the " + support.Structure + " structure has no place for notes")
	}
	notes := []*segment{}
	for i, note := range row.Notes {
		nte := newSegment("NTE")
		w.put(nte, 1, plain(strconv.Itoa(i+1)), false, setTypes)
		w.put(nte, 2, plain("L"), false, nil)
		w.put(nte, 3, plain(note), true, textTypes)
		notes = append(notes, nte)
	}

	switch t.family {
	case "ADT":
		evn := newSegment("EVN")
		w.put(evn, 1, plain(support.Trigger), false, controlTypes)
		w.put(evn, 2, when(at), true, timestampTypes)
		units = append(units, evn)
		if step.Into != "" {
			// A merge: the surviving identity in PID, the one merged away in MRG.
			pid.group = patientID
			mrg := newSegment("MRG")
			mid, mns := identity(step.Subject)
			w.put(mrg, 1, cx(mid, mns, m.patients[step.Subject].IdentifierType), true, identifierTypes)
			b.keys = append(b.keys, BusinessKey{Kind: "prior-patient", Namespace: mns, Value: mid})
			units = append(units, pid, mrg)
			break
		}
		visit := m.visits[step.Subject]
		pv1 := newSegment("PV1")
		w.put(pv1, 1, plain("1"), false, setTypes)
		w.put(pv1, 2, coded(rule, "PV1", 2, visit.Class, "HL70004"), true, classTypes)
		transfer := f.prior != nil
		w.put(pv1, 3, location(f.locations[step.Subject]), transfer, locationTypes)
		if transfer {
			w.put(pv1, 6, location(*f.prior), true, locationTypes)
		}
		vid, vns := identity(step.Subject)
		w.put(pv1, 19, cx(vid, vns, visit.IdentifierType), true, identifierTypes)
		if step.Event == "A03" {
			w.put(pv1, 45, when(at), false, timestampTypes)
		}
		b.keys = append(b.keys, BusinessKey{Kind: "visit", Namespace: vns, Value: vid})
		units = append(units, pid, pv1)
	case "SIU":
		appt := m.appts[appointmentID]
		slot := f.slots[appointmentID]
		start := m.w.BaseTime.Add(slot.start)
		end := start.Add(time.Duration(slot.duration) * time.Minute)
		sch := newSegment("SCH")
		if appt.Placer != nil {
			w.put(sch, 1, ei(appt.Placer.Identifier, appt.Placer.Namespace), false, entityTypes)
		}
		aid, ans := identity(appointmentID)
		w.put(sch, 2, ei(aid, ans), true, entityTypes)
		w.put(sch, 6, code(appt.Reason), false, codeTypes)
		timed := holdsSegment(rule.Sequence, "TQ1")
		if !timed {
			w.put(sch, 11, components("", "", "", stamp(start, m.r.Wire.Precision, offset), stamp(end, m.r.Wire.Precision, offset)), false, []string{"TQ"})
		}
		w.put(sch, 16, person(appt.Contact), false, personTypes)
		w.put(sch, 20, person(appt.EnteredBy), false, personTypes)
		w.put(sch, 25, components(fillerStatus[step.Event], "", "HL70278"), false, codeTypes)
		b.keys = append(b.keys, BusinessKey{Kind: "appointment", Namespace: ans, Value: aid})
		units = append(units, sch)
		if timed {
			tq1 := newSegment("TQ1")
			w.put(tq1, 1, plain("1"), false, setTypes)
			w.put(tq1, 7, when(start), true, timestampTypes)
			w.put(tq1, 8, when(end), false, timestampTypes)
			units = append(units, tq1)
		}
		units = append(units, notes...)
		notes = nil
		changes, err := m.resourceChanges(step, appointmentID, f, change)
		if err != nil {
			return built{}, err
		}
		rgs := newSegment("RGS")
		w.put(rgs, 1, plain("1"), true, setTypes)
		if m.r.Wire.ResourceUpdates == ActionCodeUpdates && updating[step.Event] {
			w.put(rgs, 2, plain("U"), true, controlTypes)
		}
		units = append(units, pid, rgs)
		minutes := plain(strconv.Itoa(slot.duration))
		unitsOf := components("min", "minutes", "ISO+")
		byKind := map[string][]*segment{}
		for _, c := range changes {
			r := c.resource
			var s *segment
			n := strconv.Itoa(len(byKind[r.Kind]) + 1)
			action := func() {
				if c.action != "" {
					w.put(s, 2, plain(c.action), true, controlTypes)
				}
			}
			status := components(c.status, "", "HL70278")
			switch r.Kind {
			case ServiceResource:
				s = newSegment("AIS")
				w.put(s, 1, plain(n), true, setTypes)
				action()
				w.put(s, 3, code(r.Identifier), true, codeTypes)
				w.put(s, 4, when(start), false, timestampTypes)
				w.put(s, 7, minutes, false, numberTypes)
				w.put(s, 8, unitsOf, false, codeTypes)
				w.put(s, 10, status, false, codeTypes)
			case GeneralResource:
				s = newSegment("AIG")
				w.put(s, 1, plain(n), true, setTypes)
				action()
				w.put(s, 3, code(r.Identifier), true, codeTypes)
				w.put(s, 4, code(r.Role), false, codeTypes)
				w.put(s, 8, when(start), false, timestampTypes)
				w.put(s, 11, minutes, false, numberTypes)
				w.put(s, 12, unitsOf, false, codeTypes)
				w.put(s, 14, status, false, codeTypes)
			case LocationResource:
				s = newSegment("AIL")
				w.put(s, 1, plain(n), true, setTypes)
				action()
				w.put(s, 3, components(r.Identifier.Code, "", "", r.Identifier.System), true, locationTypes)
				w.put(s, 4, code(r.Role), false, codeTypes)
				w.put(s, 6, when(start), false, timestampTypes)
				w.put(s, 9, minutes, false, numberTypes)
				w.put(s, 10, unitsOf, false, codeTypes)
				w.put(s, 12, status, false, codeTypes)
			case PersonnelResource:
				s = newSegment("AIP")
				w.put(s, 1, plain(n), true, setTypes)
				action()
				w.put(s, 3, person(Person{ID: r.Identifier.Code, Family: r.Identifier.Text, Authority: r.Identifier.System, IDType: r.IdentifierType, NameType: r.NameType}), true, personTypes)
				w.put(s, 4, code(r.Role), false, codeTypes)
				w.put(s, 6, when(start), false, timestampTypes)
				w.put(s, 9, minutes, false, numberTypes)
				w.put(s, 10, unitsOf, false, codeTypes)
				w.put(s, 12, status, false, codeTypes)
			}
			s.group = r.ID
			byKind[r.Kind] = append(byKind[r.Kind], s)
		}
		for _, kind := range []string{ServiceResource, GeneralResource, LocationResource, PersonnelResource} {
			units = append(units, byKind[kind]...)
		}
	case "ORM", "ORU":
		order := m.placers[step.Subject]
		placerID, placerNS := order.Placer.Identifier, order.Placer.Namespace
		fillerID, fillerNS := order.Filler.Identifier, order.Filler.Namespace
		// An identity mutation of an order changes the order's identity as a
		// whole: its placer and its filler number together.
		if v, ok := change.identifiers[step.Subject]; ok {
			placerID, fillerID = v, v
		}
		if v, ok := change.namespaces[step.Subject]; ok {
			placerNS, fillerNS = v, v
		}
		filler := ei(fillerID, fillerNS)
		if change.unlink {
			filler = &field{state: hl7.Empty}
		}
		orc := newSegment("ORC")
		control := "RE"
		if t.family == "ORM" {
			control = strings.TrimPrefix(string(step.Event), "ORM-")
		}
		w.put(orc, 1, plain(control), true, controlTypes)
		w.put(orc, 2, ei(placerID, placerNS), true, entityTypes)
		w.put(orc, 3, filler, !change.unlink, entityTypes)
		w.put(orc, 9, when(at), false, timestampTypes)
		obr := newSegment("OBR")
		w.put(obr, 1, plain("1"), false, setTypes)
		w.put(obr, 2, ei(placerID, placerNS), false, entityTypes)
		w.put(obr, 3, filler, false, entityTypes)
		w.put(obr, 4, code(m.orders[step.Subject].Service), true, codeTypes)
		b.keys = append(b.keys, BusinessKey{Kind: "placer-order", Namespace: placerNS, Value: placerID})
		if !change.unlink {
			b.keys = append(b.keys, BusinessKey{Kind: "filler-order", Namespace: fillerNS, Value: fillerID})
		}
		if t.family == "ORM" {
			units = append(units, notes...)
			units = append(units, pid, orc, obr)
		} else {
			status := strings.TrimPrefix(string(step.Event), "ORU-")
			w.put(obr, 7, when(at), false, timestampTypes)
			w.put(obr, 22, when(at), false, timestampTypes)
			w.put(obr, 25, plain(status), true, controlTypes)
			units = append(units, pid)
			units = append(units, notes...)
			units = append(units, orc, obr)
			for i, o := range m.results[step.ID].Observations {
				obx := newSegment("OBX")
				w.put(obx, 1, plain(strconv.Itoa(i+1)), false, setTypes)
				w.put(obx, 2, coded(rule, "OBX", 2, "TX", "HL70125"), true, idTypes)
				w.put(obx, 3, components(o.Code, "", m.orders[step.Subject].ObservationSystem), true, codeTypes)
				w.put(obx, 4, plain(o.SubID), true, []string{"ST", "OG"})
				w.put(obx, 5, plain(o.Value), true, textTypes)
				w.put(obx, 11, plain(o.Status), true, controlTypes)
				obx.group = o.Code + "/" + o.SubID
				units = append(units, obx)
			}
		}
		notes = nil
	}
	if w.err != nil {
		return built{}, w.err
	}
	if len(notes) > 0 {
		return built{}, errors.New("the " + support.Structure + " messages the generator writes have no place for notes")
	}
	placed, err := place(rule.Sequence, units)
	if err != nil {
		return built{}, fmt.Errorf("%s in HL7 %s: %w", support.Structure, t.version, err)
	}
	b.segments = placed
	return b, nil
}

// coded writes a table value as the edition types the field: the bare code of
// an ID or IS field, or the code and its HL7 table as coding system in a coded
// composite.
func coded(rule *profileeval.MessageRule, segment string, position int, value, table string) *field {
	if declared, ok := fieldRule(rule, segment, position); ok && slices.Contains(codeTypes, declared.DataType) {
		return components(value, "", table)
	}
	return plain(value)
}

// admits reports whether the edition's normative length for a field admits
// the encoded value. A field with no normative maximum admits any.
func admits(rule *profileeval.MessageRule, segment string, position int, encoded string) bool {
	declared, ok := fieldRule(rule, segment, position)
	if !ok || declared.Length == nil || declared.Length.State != "normative" || declared.Length.Max == 0 {
		return true
	}
	return utf8.RuneCountInString(encoded) <= declared.Length.Max
}

func holdsSegment(nodes []profileeval.Node, id string) bool {
	for _, n := range nodes {
		if holds(n, id) {
			return true
		}
	}
	return false
}
