package receiver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

type request struct {
	doc                     *hl7.Document
	controlID               string
	trigger                 string
	patient, placer, filler observation.Identifier
	start                   string
}

func parseRequest(raw []byte) (*request, error) {
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		return nil, errors.New("message is not supported HL7 syntax")
	}
	m := doc.Messages[0]
	control := m.Segments[0].Field(10)
	value := string(doc.Bytes(control.Span))
	if control.State != hl7.Present || !utf8.ValidString(value) || len(value) > 1024 {
		return nil, errors.New("MSH-10 must be a present UTF-8 control ID of at most 1024 bytes")
	}
	r := &request{doc: doc, controlID: value}
	// This fixture has one explicit wire profile. Syntax inspection and capture
	// remain delimiter-agnostic; no unsupported receiver semantics are inferred.
	if m.Delimiters != (hl7.Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}) {
		return nil, errors.New("receiver profile requires standard MSH-1 and MSH-2 delimiters")
	}
	header := m.Segments[0]
	if populated(header.Field(15)) || populated(header.Field(16)) {
		return r, fmt.Errorf("enhanced acknowledgement mode unsupported: MSH-15=%s, MSH-16=%s", ackModeValue(doc.Bytes(header.Field(15).Span)), ackModeValue(doc.Bytes(header.Field(16).Span)))
	}
	typeName, err := selected(doc, "MSH-9.1", true)
	if err != nil {
		return r, err
	}
	r.trigger, err = selected(doc, "MSH-9.2", true)
	if err != nil {
		return r, err
	}
	version, err := selected(doc, "MSH-12.1", true)
	if err != nil {
		return r, err
	}
	if typeName != "SIU" || r.trigger != "S12" && r.trigger != "S13" || version != "2.5.1" {
		return r, errors.New("readmit-siu-v1 requires HL7 2.5.1 SIU S12 or S13")
	}
	for _, segmentID := range []string{"SCH", "PID"} {
		count := 0
		for _, segment := range m.Segments {
			if segment.ID == segmentID {
				count++
			}
		}
		if count != 1 {
			return r, fmt.Errorf("readmit-siu-v1 requires exactly one %s segment", segmentID)
		}
	}
	r.patient, err = identifier(doc, "PID-3.1", "PID-3.4.1", "PID-3.4.2", "PID-3.4.3")
	if err != nil {
		return r, err
	}
	r.placer, err = identifier(doc, "SCH-1.1", "SCH-1.2", "SCH-1.3", "SCH-1.4")
	if err != nil {
		return r, err
	}
	r.filler, err = identifier(doc, "SCH-2.1", "SCH-2.2", "SCH-2.3", "SCH-2.4")
	if err != nil {
		return r, err
	}
	r.start, err = selected(doc, "SCH-11.4", true)
	if err != nil {
		return r, err
	}
	layout := "20060102150405"
	if len(r.start) == 19 {
		layout += "-0700"
	}
	parsed, err := time.Parse(layout, r.start)
	if err != nil || parsed.Format(layout) != r.start {
		return r, errors.New("SCH-11.4 requires a valid whole-second YYYYMMDDhhmmss time with optional numeric offset")
	}
	return r, nil
}

func populated(field hl7.Field) bool { return field.State != hl7.Omitted && field.State != hl7.Empty }

func ackModeValue(value []byte) string {
	if len(value) > 128 {
		return strconv.QuoteToASCII(string(value[:128])) + " (truncated)"
	}
	return strconv.QuoteToASCII(string(value))
}

func selected(doc *hl7.Document, path string, required bool) (string, error) {
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		return "", err
	}
	value, err := doc.Select(0, selector)
	if err != nil {
		return "", err
	}
	if value.State != hl7.Present {
		if !required && value.State != hl7.Null {
			return "", nil
		}
		return "", fmt.Errorf("%s must have a supported present value", path)
	}
	decoded, err := hl7.Decode(doc.Bytes(value.Span), doc.Messages[0].Delimiters)
	if err != nil || !utf8.Valid(decoded) || len(decoded) > 1024 || len(decoded) == 0 {
		return "", fmt.Errorf("%s must contain supported UTF-8 text of at most 1024 bytes", path)
	}
	return string(decoded), nil
}

func identifier(doc *hl7.Document, paths ...string) (observation.Identifier, error) {
	var identifier observation.Identifier
	fields := []*string{&identifier.Value, &identifier.Namespace, &identifier.UniversalID, &identifier.UniversalIDType}
	for i, path := range paths {
		value, err := selected(doc, path, i == 0)
		if err != nil {
			return identifier, err
		}
		*fields[i] = value
	}
	if (identifier.UniversalID == "") != (identifier.UniversalIDType == "") {
		return identifier, errors.New("identifier universal ID and type must be supplied together")
	}
	return identifier, nil
}

func (r *Receiver) apply(request *request) error {
	if request.trigger == "S13" {
		match := -1
		for i, record := range r.snapshot.Records {
			if record.FillerID != request.filler {
				continue
			}
			if match != -1 {
				return errors.New("reschedule filler identifier is ambiguous")
			}
			match = i
		}
		if match == -1 {
			return errors.New("reschedule has no existing appointment")
		}
		existing := &r.snapshot.Records[match]
		if existing.PatientID != request.patient || existing.PlacerID != request.placer {
			return errors.New("reschedule patient or placer identifier disagrees with appointment")
		}
		if r.config.Mode == observation.Fixed {
			existing.AppointmentStart = request.start
			return nil
		}
	}
	r.snapshot.Records = append(r.snapshot.Records, observation.Record{
		RecordID:  fmt.Sprintf("r%06d", len(r.snapshot.Records)+1),
		PatientID: request.patient, PlacerID: request.placer, FillerID: request.filler, AppointmentStart: request.start,
	})
	return nil
}

func acknowledgement(request *request, code, reason string, sequence int) []byte {
	trigger := request.trigger
	if trigger != "S12" && trigger != "S13" {
		trigger = ""
	}
	header := fmt.Sprintf("MSH|^~\\&|READMIT|FIXTURE|||%s||ACK^%s|READMITACK%06d|P|2.5.1\r", time.Now().UTC().Format("20060102150405-0700"), trigger, sequence)
	text := header + "MSA|" + code + "|" + request.controlID
	if reason != "" {
		// Errors returned on the wire may name explicit MSH-15/16 values. They
		// never go to terminal diagnostics and cannot introduce HL7 fields.
		replacer := strings.NewReplacer("\\", `\E\`, "|", `\F\`, "^", `\S\`, "~", `\R\`, "&", `\T\`)
		text += "|" + replacer.Replace(reason)
	}
	return mllp.Frame([]byte(text + "\r"))
}
