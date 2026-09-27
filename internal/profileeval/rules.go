package profileeval

import (
	"github.com/bharm16/readmit/internal/localprofile"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var number = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)
var digits = regexp.MustCompile(`^[0-9]+$`)

func datatype(kind, value string) string {
	switch kind {
	case "ST", "TX", "FT", "ID", "IS":
		return "pass"
	case "SI":
		if len(value) <= 4 && digits.MatchString(value) {
			return "pass"
		}
		return "fail"
	case "NM":
		if number.MatchString(value) {
			return "pass"
		}
		return "fail"
	case "DT", "DTM", "TM":
		if _, _, ok := dateParts(value, kind); ok {
			return "pass"
		}
		return "fail"
	// Composite content needs its own versioned component metadata. Checking
	// only its first component would mislabel partial support as conformance.
	default:
		return "unsupported"
	}
}
func dateParts(value, kind string) (string, bool, bool) {
	zone := false
	if len(value) >= 5 {
		at := len(value) - 5
		if value[at] == '+' || value[at] == '-' {
			z := value[at+1:]
			if !digits.MatchString(z) {
				return "", false, false
			}
			h, _ := strconv.Atoi(z[:2])
			m, _ := strconv.Atoi(z[2:])
			if h > 23 || m > 59 {
				return "", false, false
			}
			zone = true
			value = value[:at]
		}
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return "", zone, false
	}
	core := parts[0]
	fraction := len(parts) == 2
	if !digits.MatchString(core) {
		return "", zone, false
	}
	lengths := []int{4, 6, 8, 10, 12, 14}
	if kind == "DT" {
		lengths = []int{4, 6, 8}
		if zone || fraction {
			return "", zone, false
		}
	}
	if kind == "TM" {
		lengths = []int{2, 4, 6}
	}
	if !slices.Contains(lengths, len(core)) {
		return "", zone, false
	}
	if fraction && (len(parts[1]) < 1 || len(parts[1]) > 4 || !digits.MatchString(parts[1]) || len(core) != 14 && kind != "TM" || kind == "TM" && len(core) != 6) {
		return "", zone, false
	}
	layout := "20060102150405"
	if kind == "TM" {
		layout = "150405"
	}
	if _, err := time.Parse(layout[:len(core)], core); err != nil {
		return "", zone, false
	}
	precision := map[int]string{4: "year", 6: "month", 8: "day", 10: "hour", 12: "minute", 14: "second"}[len(core)]
	if kind == "TM" {
		precision = map[int]string{2: "hour", 4: "minute", 6: "second"}[len(core)]
	}
	if fraction {
		precision = "fraction"
	}
	return precision, zone, true
}
func dateMatches(value, kind string, r localprofile.DateHandling) bool {
	if kind == "TS" {
		kind = "DTM"
	}
	precision, zone, valid := dateParts(value, kind)
	return valid && precision == string(r.Precision) && (r.TimeZone != localprofile.TimeZoneRequired || zone) && (r.TimeZone != localprofile.TimeZoneForbidden || !zone)
}
