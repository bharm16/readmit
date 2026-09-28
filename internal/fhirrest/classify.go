package fhirrest

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
)

type Outcome struct {
	State              string    `json:"state"`
	Payload            string    `json:"payload"`
	ConditionalMatches string    `json:"conditional_matches,omitzero"`
	LogicalID          string    `json:"logical_id,omitzero"`
	Version            string    `json:"version,omitzero"`
	Entries            []Outcome `json:"entries"`
}

func classify(base string, request fhirrequest.Request, status int, headers map[string]string, body []byte, prior *Prior) Outcome {
	o := Outcome{State: "protocol-invalid", Payload: "empty", Entries: []Outcome{}}
	var doc *fhirr4.Document
	if len(body) > 0 {
		o.Payload = "non-fhir"
		if fhirrequest.MediaType(headers["Content-Type"]) == "application/fhir+json" {
			var e error
			doc, e = fhirr4.Decode(context.Background(), body, fhirr4.Context{Version: fhirr4.Version, Base: base, MediaType: "application/fhir+json"})
			if e == nil && len(doc.Resources()) > 0 {
				o.Payload = doc.Resources()[0].Type
				invalid, unsupported := false, false
				for _, finding := range doc.Findings() {
					invalid = invalid || finding.State == "invalid"
					unsupported = unsupported || finding.State == "unsupported"
				}
				if invalid {
					o.Payload = "invalid-fhir"
					doc = nil
				} else if unsupported || !fhirr4.SupportedHTTPResource(o.Payload) {
					o.Payload = "unsupported-fhir"
					doc = nil
				}
			} else {
				doc = nil
				o.Payload = "invalid-fhir"
			}
		}
	}
	switch status {
	case 401:
		o.State = "unauthorized"
		return o
	case 403:
		o.State = "forbidden"
		return o
	case 404, 410:
		o.State = "not-found"
		return o
	case 409, 412:
		o.State = "conflict"
		if status == 412 && (request.Kind == "conditional-create" || request.Kind == "conditional-update") {
			o.ConditionalMatches = "multiple"
		}
		return o
	case 429:
		o.State = "throttled"
		return o
	case 500, 502, 503, 504:
		o.State = "unavailable"
		return o
	case 202:
		o.State = "pending"
		return o
	case 304:
		if request.Kind == "read" && prior != nil && len(body) == 0 && (headers["ETag"] == "" || headers["ETag"] == prior.ETag) {
			o.State = "not-modified"
			o.Payload = "pinned-prior"
		}
		return o
	}
	if status >= 400 && status < 500 {
		o.State = "rejected"
		if request.Kind == "transaction" {
			o.State = "rejected-transaction"
		}
		return o
	}
	if status >= 200 && status < 300 && o.Payload == "unsupported-fhir" {
		o.State = "unsupported"
		return o
	}
	if status < 200 || status >= 300 {
		return o
	}
	if request.Kind == "transaction" || request.Kind == "batch" {
		if status != 200 || doc == nil {
			return o
		}
		bundle, e := doc.HTTPBundle()
		if e != nil || bundle.Type != request.Kind+"-response" || len(bundle.Entries) != len(request.Entries) {
			return o
		}
		o.State = "succeeded"
		for i, entry := range bundle.Entries {
			parts := strings.SplitN(entry.Response.Status, " ", 2)
			if len(parts[0]) != 3 {
				o.State = "protocol-invalid"
				return o
			}
			code, e := strconv.Atoi(parts[0])
			if e != nil {
				o.State = "protocol-invalid"
				return o
			}
			h := map[string]string{"Content-Type": "application/fhir+json", "Location": entry.Response.Location, "ETag": entry.Response.ETag}
			payload := entry.Resource
			if len(payload) == 0 {
				payload = entry.Response.Outcome
			}
			child := classify(base, request.Entries[i], code, h, payload, nil)
			o.Entries = append(o.Entries, child)
			if child.State != "succeeded" && child.State != "not-modified" {
				if request.Kind == "transaction" {
					o.State = "protocol-invalid"
				} else {
					o.State = "partial-failure"
				}
			}
		}
		return o
	}
	switch request.Kind {
	case "capabilities":
		if status != 200 || doc == nil || o.Payload != "CapabilityStatement" {
			return o
		}
	case "search":
		if status != 200 || doc == nil {
			return o
		}
		bundle, e := doc.HTTPBundle()
		if e != nil || bundle.Type != "searchset" {
			return o
		}
		types := map[string]string{}
		for _, resource := range doc.Resources() {
			types[resource.Pointer] = resource.Type
		}
		for i, entry := range bundle.Entries {
			typ, exists := types[entryPointer(i)]
			if len(entry.Resource) == 0 || !exists || entry.SearchMode == "outcome" && typ != "OperationOutcome" {
				return o
			}
		}
	case "read", "vread":
		if status != 200 || doc == nil || o.Payload != request.Resource {
			return o
		}
	case "create":
		if status != 201 {
			return o
		}
	case "conditional-create":
		if status != 200 && status != 201 {
			return o
		}
		if status == 201 {
			o.ConditionalMatches = "zero"
		} else {
			o.ConditionalMatches = "one"
		}
	case "update", "patch", "conditional-update":
		if status != 200 && status != 201 {
			return o
		}
		if request.Kind == "conditional-update" {
			if status == 201 {
				o.ConditionalMatches = "zero"
			} else {
				o.ConditionalMatches = "one"
			}
		}
	case "delete":
		if status != 200 && status != 204 {
			return o
		}
	default:
		return o
	}
	if len(body) > 0 && doc == nil {
		return o
	}
	if doc != nil && request.Kind != "search" && request.Kind != "capabilities" {
		if o.Payload == "OperationOutcome" {
			severities, _ := doc.OutcomeSeverities()
			for _, severity := range severities {
				if severity == "error" || severity == "fatal" {
					return o
				}
			}
		} else {
			resources := doc.Resources()
			resource := resources[0]
			if resource.Type != request.Resource || request.ID != "" && request.ID != resource.LogicalID || request.Version != "" && request.Version != resource.VersionID {
				return o
			}
			o.LogicalID = resource.LogicalID
			o.Version = resource.VersionID
		}
	}
	if location := headers["Location"]; location != "" {
		b, _ := url.Parse(base + "/")
		u, e := url.Parse(location)
		if e != nil || u.User != nil || u.Fragment != "" {
			return o
		}
		u = b.ResolveReference(u)
		scope, _ := url.Parse(base)
		if u.Scheme != scope.Scheme || u.Host != scope.Host || !strings.HasPrefix(u.Path, scope.Path+"/") || u.RawQuery != "" || u.RawPath != "" {
			return o
		}
		parts := strings.Split(strings.TrimPrefix(u.Path, scope.Path+"/"), "/")
		if len(parts) != 2 && len(parts) != 4 || parts[0] != request.Resource || !fhirrequest.ValidID(parts[1]) {
			return o
		}
		if request.ID != "" && request.ID != parts[1] || o.LogicalID != "" && o.LogicalID != parts[1] {
			return o
		}
		o.LogicalID = parts[1]
		if len(parts) == 4 {
			if parts[2] != "_history" || !fhirrequest.ValidID(parts[3]) || request.Version != "" && request.Version != parts[3] || o.Version != "" && o.Version != parts[3] {
				return o
			}
			o.Version = parts[3]
		}
	}
	if tag := headers["ETag"]; tag != "" {
		if len(tag) < 5 || !strings.HasPrefix(tag, `W/"`) || !strings.HasSuffix(tag, `"`) {
			return o
		}
		v := tag[3 : len(tag)-1]
		if !fhirrequest.ValidID(v) || o.Version != "" && o.Version != v {
			return o
		}
		o.Version = v
	}
	if status == 201 && request.Kind == "create" && headers["Location"] == "" {
		return o
	}
	o.State = "succeeded"
	return o
}
