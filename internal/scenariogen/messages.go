package scenariogen

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/scenario"
)

type Arrival struct {
	Step      string `json:"step"`
	After     string `json:"after"`
	Duplicate bool   `json:"duplicate"`
}

type message struct {
	step      string
	at        time.Duration
	segments  [][]string
	charset   string
	duplicate bool
}

type stream struct {
	data     []byte
	arrivals []Arrival
}

func generate(p Plan, d template, r Row, v Variant) (stream, error) {
	random := rand.New(rand.NewPCG(p.Seed, 0x726561646d697463))
	messages := make([]message, 0, len(d.Steps)*2)
	for _, step := range d.Steps {
		offset, _ := time.ParseDuration(step.After)
		declared := d.BaseTime.Add(offset).UTC()
		m := message{step: step.ID, at: offset, charset: r.Encoding}
		for _, mutation := range v.Mutations {
			if mutation.Step != step.ID {
				continue
			}
			switch mutation.Op {
			case "timezone":
				loc, _ := zone(mutation.Offset)
				declared = declared.In(loc)
			case "encoding":
				m.charset = mutation.Encoding
			case "delay":
				extra, _ := delay(mutation.After)
				m.at += extra
			case "duplicate":
				m.duplicate = true
			}
		}
		if declared.Year() < 1 || declared.Year() > 9999 || d.BaseTime.Add(m.at).Year() > 9999 {
			return stream{}, errors.New("variant timestamp exceeds supported years")
		}
		m.segments = segments(d, step, r, declared, fmt.Sprintf("SYNTH-%016X", random.Uint64()))
		for _, mutation := range v.Mutations {
			if mutation.Step != step.ID || mutation.Op != "field" {
				continue
			}
			seg, field := "PID", 5
			if mutation.Field == "NTE-3" {
				seg, field = "NTE", 3
			}
			for i, s := range m.segments {
				if s[0] != seg {
					continue
				}
				switch mutation.State {
				case "absent":
					m.segments[i] = s[:field]
				case "empty":
					s[field] = ""
				case "null":
					s[field] = `""`
				}
			}
		}
		messages = append(messages, m)
	}
	// Intended arrivals alone decide stream order, with authored step order as
	// the explicit tie break. A retransmission is adjacent and byte-identical.
	sort.SliceStable(messages, func(i, j int) bool { return messages[i].at < messages[j].at })
	out := stream{arrivals: []Arrival{}}
	for _, m := range messages {
		data, err := serialize(m)
		if err != nil {
			return stream{}, err
		}
		out.data = append(out.data, data...)
		out.arrivals = append(out.arrivals, Arrival{Step: m.step, After: m.at.String()})
		if m.duplicate {
			out.data = append(out.data, data...)
			out.arrivals = append(out.arrivals, Arrival{Step: m.step, After: m.at.String(), Duplicate: true})
		}
	}
	return out, nil
}

// segments implements finite readmit-authored HL7 2.5.1 fixture mappings, not
// clinical conformance. Scenario initial state is never invented as messages.
func segments(d template, step scenario.Step, r Row, at time.Time, control string) [][]string {
	subjects := map[string]scenario.Subject{}
	for _, s := range d.Subjects {
		subjects[s.ID] = s
	}
	subject := subjects[step.Subject]
	patient := subject
	if subject.Patient != "" {
		patient = subjects[subject.Patient]
	}
	if step.Into != "" {
		patient = subjects[step.Into]
	}
	family, event := "ADT", string(step.Event)
	switch d.Profile {
	case scenario.SIULifecycle:
		family = "SIU"
	case scenario.ORMLifecycle:
		family, event = "ORM", "O01"
	case scenario.ORULifecycle:
		family, event = "ORU", "R01"
	}
	msh := []string{"MSH", "^~\\&", "READMIT", "SYNTHETIC", "RECEIVER", "READMIT", at.Format(hl7.TimestampLayout), "", family + "^" + event, control, "T", "2.5.1", "", "", "", "", "", ""}
	result := [][]string{msh}
	if family == "ADT" {
		result = append(result, []string{"EVN", event, at.Format(hl7.TimestampLayout)})
	}
	result = append(result, []string{"PID", "1", "", patient.Identifier + "^^^" + patient.Namespace, "", r.PatientName})
	switch family {
	case "ADT":
		if step.Into != "" {
			result = append(result, []string{"MRG", subject.Identifier + "^^^" + subject.Namespace})
		} else {
			pv := make([]string, 20)
			pv[0], pv[1], pv[2], pv[19] = "PV1", "1", "I", subject.Identifier+"^^^"+subject.Namespace
			result = append(result, pv)
		}
	case "SIU":
		result = append(result, []string{"SCH", subject.Identifier + "^" + subject.Namespace, subject.Identifier + "^" + subject.Namespace})
	case "ORM", "ORU":
		var order scenario.Order
		for _, o := range d.Orders {
			if o.Subject == subject.ID {
				order = o
			}
		}
		placer, filler := order.Placer.Identifier+"^"+order.Placer.Namespace, order.Filler.Identifier+"^"+order.Filler.Namespace
		control := "RE"
		if family == "ORM" {
			control = strings.TrimPrefix(string(step.Event), "ORM-")
		}
		result = append(result, []string{"ORC", control, placer, filler})
		obr := make([]string, 26)
		obr[0], obr[1], obr[2], obr[3], obr[4] = "OBR", "1", placer, filler, "SYNTH-TEXT"
		if family == "ORU" {
			obr[25] = strings.TrimPrefix(string(step.Event), "ORU-")
		}
		result = append(result, obr)
		for _, report := range d.Results {
			if report.Step != step.ID {
				continue
			}
			for i, o := range report.Observations {
				obx := make([]string, 12)
				obx[0], obx[1], obx[2], obx[3], obx[4], obx[5], obx[11] = "OBX", fmt.Sprint(i+1), "TX", o.Code, o.SubID, o.Value, o.Status
				result = append(result, obx)
			}
		}
	}
	for i, n := range r.Notes {
		result = append(result, []string{"NTE", fmt.Sprint(i + 1), "L", n})
	}
	return result
}

func serialize(m message) ([]byte, error) {
	charset := "UNICODE UTF-8"
	if m.charset == "iso-8859-1" {
		charset = "8859/1"
	}
	m.segments[0][17] = charset
	framed := mllp.Frame(hl7.Encode(m.segments))
	if m.charset == "utf-8" {
		return framed, nil
	}
	out := make([]byte, 0, len(framed))
	for _, r := range string(framed) {
		if r > 255 {
			return nil, errors.New("row text cannot be represented in ISO-8859-1")
		}
		out = append(out, byte(r))
	}
	return out, nil
}
