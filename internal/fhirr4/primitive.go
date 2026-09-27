package fhirr4

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bharm16/readmit/internal/dataset"
)

var decimalLexeme = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
var oidLexeme = regexp.MustCompile(`^urn:oid:[0-2](\.(0|[1-9][0-9]*))+$`)
var uuidLexeme = regexp.MustCompile(`^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var clockLexeme = regexp.MustCompile(`^([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})?$`)

func scalar(typ string, n *node) dataset.Value {
	v := dataset.Value{State: "present", Type: "text"}
	switch typ {
	case "decimal", "integer", "positiveInt", "unsignedInt":
		v.Type = "decimal"
	case "boolean":
		v.Type = "boolean"
	case "date":
		v.Type = "date"
	case "dateTime", "instant", "time":
		v.Type = "datetime"
	case "code":
		v.Type = "code"
	}
	if n == nil {
		v.State = "absent"
		return v
	}
	v.Text = n.text
	if n.kind == 'n' {
		v.State = "invalid"
		v.Text = ""
		return v
	}
	number := typ == "integer" || typ == "positiveInt" || typ == "unsignedInt" || typ == "decimal"
	if number {
		v.Type = "decimal"
		if n.kind != '0' {
			v.State = "invalid"
			return v
		}
	} else if typ == "boolean" {
		v.Type = "boolean"
		if n.kind != 't' && n.kind != 'f' {
			v.State = "invalid"
		}
		return v
	} else if n.kind != '"' || n.text == "" {
		v.State = "invalid"
		return v
	}
	switch typ {
	case "decimal":
		if len(n.text) > 256 {
			v.State = "unsupported"
			return v
		}
		if !decimalLexeme.MatchString(n.text) {
			v.State = "invalid"
			return v
		}
		mantissa := strings.Split(strings.Split(n.text, "e")[0], "E")[0]
		precision := 0
		if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
			precision = len(mantissa) - dot - 1
		}
		v.Precision = strconv.Itoa(precision)
	case "integer", "positiveInt", "unsignedInt":
		value, err := strconv.ParseInt(n.text, 10, 32)
		if err != nil || strings.ContainsAny(n.text, ".eE") || typ == "positiveInt" && value < 1 || typ == "unsignedInt" && value < 0 {
			v.State = "invalid"
		}
		v.Precision = "0"
	case "date", "dateTime", "instant", "time":
		v.Type = "datetime"
		if typ == "date" {
			v.Type = "date"
		}
		v.Precision, v.Timezone = temporal(typ, n.text)
		if v.Precision == "" {
			v.State = "invalid"
		}
	case "id":
		if !identifier.MatchString(n.text) {
			v.State = "invalid"
		}
	case "code":
		v.Type = "code"
		if strings.TrimSpace(n.text) != n.text || strings.ContainsAny(n.text, "\r\n\t") || strings.Contains(n.text, "  ") {
			v.State = "invalid"
		}
	case "uri", "url", "canonical", "oid", "uuid":
		value := n.text
		if typ == "canonical" {
			c, err := ParseCanonical(value)
			if err != nil {
				v.State = "invalid"
				return v
			}
			value = c.URL
		}
		u, err := url.Parse(value)
		if err != nil || strings.IndexFunc(value, unicode.IsSpace) >= 0 || u.User != nil {
			v.State = "invalid"
		}
		if typ == "oid" && !oidLexeme.MatchString(value) || typ == "uuid" && !uuidLexeme.MatchString(value) {
			v.State = "invalid"
		}
	case "base64Binary":
		if _, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, n.text)); err != nil {
			v.State = "invalid"
		}
	}
	return v
}
func temporal(typ, value string) (string, string) {
	if typ == "time" {
		return clock(value, false)
	}
	at := strings.IndexByte(value, 'T')
	date := value
	if at >= 0 {
		date = value[:at]
	}
	precision := ""
	switch len(date) {
	case 4:
		precision = "year"
	case 7:
		precision = "month"
	case 10:
		precision = "date"
	default:
		return "", ""
	}
	parts := strings.Split(date, "-")
	for _, part := range parts {
		if !asciiDigits(part) {
			return "", ""
		}
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil || year < 1 || year > 9999 || len(parts[0]) != 4 {
		return "", ""
	}
	month, day := 1, 1
	if len(parts) > 1 {
		if len(parts[1]) != 2 {
			return "", ""
		}
		month, err = strconv.Atoi(parts[1])
		if err != nil || month < 1 || month > 12 {
			return "", ""
		}
	}
	if len(parts) > 2 {
		if len(parts[2]) != 2 {
			return "", ""
		}
		day, err = strconv.Atoi(parts[2])
		if err != nil || day < 1 || day > 31 {
			return "", ""
		}
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if t.Year() != year || int(t.Month()) != month || t.Day() != day {
			return "", ""
		}
	}
	if at < 0 {
		if typ == "instant" {
			return "", ""
		}
		return precision, "absent"
	}
	if typ == "date" || precision != "date" {
		return "", ""
	}
	return clock(value[at+1:], true)
}
func clock(value string, zoneRequired bool) (string, string) {
	if len(value) > 128 {
		return "", ""
	}
	m := clockLexeme.FindStringSubmatch(value)
	if m == nil {
		return "", ""
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	second, _ := strconv.Atoi(m[3])
	if hour > 23 || minute > 59 || second > 60 {
		return "", ""
	}
	zone := m[5]
	if zoneRequired && zone == "" || !zoneRequired && zone != "" {
		return "", ""
	}
	if len(zone) > 1 {
		hours, _ := strconv.Atoi(zone[1:3])
		minutes, _ := strconv.Atoi(zone[4:])
		if hours > 14 || minutes > 59 || hours == 14 && minutes != 0 {
			return "", ""
		}
	}
	if zone == "" {
		zone = "absent"
	}
	precision := "second"
	if m[4] != "" {
		precision = "fraction-" + strconv.Itoa(len(m[4])-1)
	}
	return precision, zone
}
func codeSystem(typ, key string, parent *node) string {
	if typ == "Coding" || typ == "Quantity" || typ == "Age" || typ == "Count" || typ == "Distance" || typ == "Duration" {
		return parent.field("system").string()
	}
	if f, ok := definitions[typ][key]; ok && f.system != "" {
		return f.system
	}
	systems := map[string]string{"Appointment.status": "appointmentstatus", "Participant.status": "participationstatus", "Participant.required": "participantrequired", "Observation.status": "observation-status", "DiagnosticReport.status": "diagnostic-report-status", "ServiceRequest.status": "request-status", "ServiceRequest.intent": "request-intent", "Encounter.status": "encounter-status", "Patient.gender": "administrative-gender", "Practitioner.gender": "administrative-gender", "Location.status": "location-status", "Identifier.use": "identifier-use", "Bundle.type": "bundle-type", "BundleRequest.method": "http-verb", "OutcomeIssue.severity": "issue-severity", "OutcomeIssue.code": "issue-type"}
	if system := systems[typ+"."+key]; system != "" {
		return "http://hl7.org/fhir/" + system
	}
	return ""
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
