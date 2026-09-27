package dataset

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/importer"
)

var decimal = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,4})?$`)

func projectValue(c Column, v importer.ProjectedValue) Value {
	out := Value{State: v.State, Type: c.Type}
	if v.Kind == "array" {
		if !c.Repeated {
			out.State = "invalid"
			return out
		}
		out.Items = []Value{}
		element := c
		element.Repeated = false
		for _, item := range v.Items {
			out.Items = append(out.Items, projectValue(element, item))
		}
		return out
	}
	if c.Repeated && v.State == "present" {
		out.State = "invalid"
		return out
	}

	if v.State != "present" {
		return out
	}
	if len(v.Text) > 65536 || !utf8.ValidString(v.Text) {
		out.State = "unreadable"
		return out
	}
	out.Text = v.Text
	switch c.Type {
	case "text":
		if v.Kind != "string" {
			out.State = "invalid"
		}
	case "decimal":
		if v.Kind != "string" && v.Kind != "number" || len(v.Text) > 256 || !decimal.MatchString(v.Text) {
			out.State = "invalid"
			break
		}
		mantissa := v.Text
		if at := strings.IndexAny(mantissa, "eE"); at >= 0 {
			exp, _ := strconv.Atoi(mantissa[at+1:])
			if exp > 1000 || exp < -1000 {
				out.State = "invalid"
			}
			mantissa = mantissa[:at]
		}
		scale := 0
		if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
			scale = len(mantissa) - dot - 1
		}
		out.Precision = strconv.Itoa(scale)
	case "boolean":
		if v.Text != "true" && v.Text != "false" {
			out.State = "invalid"
		}
	case "code":
		if v.Kind != "string" {
			out.State = "invalid"
		}
		out.CodeSystem = c.CodeSystem
	case "date":
		if _, err := time.Parse("2006-01-02", v.Text); err != nil {
			out.State = "invalid"
		}
		out.Precision = "date"
		out.Timezone = "absent"
	case "datetime":
		out.Precision, out.Timezone = temporal(v.Text)
		if out.Precision == "" {
			out.State = "invalid"
		}
	}
	return out
}
func temporal(s string) (string, string) {
	for _, candidate := range []struct{ layout, precision, zone string }{
		{time.RFC3339Nano, "second", "explicit"}, {"2006-01-02T15:04:05.999999999", "second", "absent"}, {"2006-01-02T15:04", "minute", "absent"}, {"2006-01-02", "date", "absent"},
		{"20060102150405-0700", "second", "explicit"}, {"20060102150405", "second", "absent"}, {"200601021504", "minute", "absent"}, {"20060102", "date", "absent"},
	} {
		if t, err := time.Parse(candidate.layout, s); err == nil {
			precision := candidate.precision
			if dot := strings.IndexAny(s, ".,"); dot >= 0 {
				end := dot + 1
				for end < len(s) && s[end] >= '0' && s[end] <= '9' {
					end++
				}
				precision = "fraction-" + strconv.Itoa(end-dot-1)
			}
			zone := candidate.zone
			if zone == "explicit" {
				zone = t.Format("-07:00")
			}
			return precision, zone
		}
	}
	return "", ""
}
func invalidValue(v Value) bool {
	if v.State == "invalid" || v.State == "unreadable" {
		return true
	}
	for _, item := range v.Items {
		if invalidValue(item) {
			return true
		}
	}
	return false
}

// ValidExpected checks the same typed representation a projection can produce.
func ValidExpected(v Value) bool {
	if !contains([]string{"present", "empty", "null", "absent"}, v.State) || !contains([]string{"text", "decimal", "boolean", "date", "datetime", "code"}, v.Type) {
		return false
	}
	if v.Items != nil {
		if v.State != "present" || v.Text != "" || v.Precision != "" || v.Timezone != "" || v.CodeSystem != "" || len(v.Items) > 1024 {
			return false
		}
		for _, item := range v.Items {
			if item.Items != nil || item.Type != v.Type || !ValidExpected(item) {
				return false
			}
		}
		return true
	}
	if v.State != "present" {
		return v.Text == "" && v.Precision == "" && v.Timezone == "" && v.CodeSystem == ""
	}
	kind := "string"
	if v.Type == "boolean" {
		kind = "boolean"
	}
	if v.Type == "decimal" {
		kind = "number"
	}
	got := projectValue(Column{Type: v.Type, CodeSystem: v.CodeSystem}, importer.ProjectedValue{State: "present", Kind: kind, Text: v.Text})
	return got.State == "present" && Equal(got, v) && (v.Type != "code" || v.CodeSystem != "")
}

func Compatible(c Column, v Value) bool {
	if !ValidExpected(v) || c.Type != v.Type {
		return false
	}
	if v.State == "present" && (v.Items != nil) != c.Repeated {
		return false
	}
	if v.Items != nil {
		element := c
		element.Repeated = false
		for _, item := range v.Items {
			if !Compatible(element, item) {
				return false
			}
		}
	}
	return v.State != "present" || c.Type != "code" || c.Repeated || c.CodeSystem == v.CodeSystem
}
