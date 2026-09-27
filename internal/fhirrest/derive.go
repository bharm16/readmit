package fhirrest

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
)

func derive(ctx context.Context, p *Plan, r *Result, bodies map[int][]byte) {
	r.State = r.ExecutionState
	projectionUnknown := false
	r.Search = Search{Coverage: "not-search", Consistency: "not-declared", Overlaps: []Overlap{}}
	r.Projections = []fhirr4.Dataset{}
	if p.request.Kind == "search" {
		r.Search.Coverage = "complete"
		if r.ExecutionState != "succeeded" {
			r.Search.Coverage = "incomplete"
		}
	}
	type occurrence struct {
		attempt         int
		version, digest string
	}
	seen := map[string]occurrence{}
	matched := map[string]bool{}
	for _, a := range r.Attempts {
		if a.Phase != "interaction" || a.Outcome.State != "succeeded" && a.Outcome.State != "not-modified" {
			continue
		}
		raw := bodies[a.Index]
		if a.Outcome.State == "not-modified" && p.spec.Prior != nil {
			raw = p.spec.Prior.Body
		}
		d, e := decode(p.spec.Base, raw)
		if e != nil {
			continue
		}
		matches := map[string]bool{}
		resources := d.Resources()
		if p.request.Kind == "search" {
			b, e := d.HTTPBundle()
			if e != nil {
				r.Search.Coverage = "unknown"
				continue
			}
			r.Search.Pages++
			self := ""
			for _, link := range b.Links {
				if link.Relation == "self" {
					if self != "" {
						r.Search.Coverage = "unknown"
					}
					self = link.URL
				}
			}
			selfURL, se := destination.ScopedLink(p.spec.Base+"/", self)
			_, scopeErr := fhirrequest.Page(p.spec.Base, p.request.Resource, selfURL)
			if self == "" || se != nil || scopeErr != nil || !sameParameters(p.request.Parameters, selfURL) {
				r.Search.Coverage = "unknown"
			}
			if len(p.request.Parameters["_elements"]) > 0 || (len(p.request.Parameters["_summary"]) > 0 && p.request.Parameters["_summary"][0] != "false") || d.Subsetted() {
				r.Search.Coverage = "unknown"
			}
			for i, entry := range b.Entries {
				switch entry.SearchMode {
				case "match":
					r.Search.MatchOccurrences++
				case "include":
					r.Search.Includes++
				case "outcome":
					r.Search.Outcomes++
				case "":
					r.Search.Coverage = "unknown"
				default:
					r.Search.Coverage = "unknown"
				}
				if entry.SearchMode == "outcome" {
					out, e := decode(p.spec.Base, entry.Resource)
					if e != nil {
						r.Search.Coverage = "unknown"
						continue
					}
					severities, e := out.OutcomeSeverities()
					if e != nil {
						r.Search.Coverage = "unknown"
					}
					for _, s := range severities {
						if s == "fatal" || s == "error" {
							r.Search.Coverage = "unknown"
						}
					}
					continue
				}
				pointer := fmt.Sprintf("/entry/%d/resource", i)
				for _, resource := range resources {
					if resource.Pointer != pointer {
						continue
					}
					if entry.SearchMode == "match" {
						if resource.Type != p.request.Resource {
							r.Search.Coverage = "unknown"
						}
					}
					if resource.LogicalID == "" {
						r.Search.Coverage = "unknown"
						continue
					}
					identity := resource.Base + "/" + resource.Type + "/" + resource.LogicalID
					if entry.SearchMode == "match" && !matched[identity] {
						matched[identity] = true
						r.Search.Matches++
						matches[resource.Occurrence] = true
					}
					digest := dataset.Digest(entry.Resource)
					if first, ok := seen[identity]; ok {
						r.Search.Overlaps = append(r.Search.Overlaps, Overlap{Identity: identity, FirstAttempt: first.attempt, Attempt: a.Index, FirstVersion: first.version, Version: resource.VersionID, Changed: first.version != resource.VersionID || first.digest != digest})
						if first.version != resource.VersionID || first.digest != digest {
							r.Search.Consistency = "changed-between-pages"
							r.Search.Coverage = "unknown"
						}
					} else {
						seen[identity] = occurrence{a.Index, resource.VersionID, digest}
					}
				}
			}
		}
		if p.spec.Projection != nil {
			partialView := len(p.request.Parameters["_elements"]) > 0 || (len(p.request.Parameters["_summary"]) > 0 && p.request.Parameters["_summary"][0] != "false") || d.Subsetted()
			projected, e := d.Project(ctx, dataset.Binding{Run: "fhir-run", Phase: "after", Source: dataset.Digest(raw), Namespace: "fhir"}, *p.spec.Projection)
			if e != nil {
				projectionUnknown = true
				continue
			}
			if p.request.Kind == "search" {
				rows := []dataset.Row{}
				ids := map[string]bool{}
				for _, row := range projected.Rows {
					if matches[row.Provenance.SourceRecord] {
						rows = append(rows, row)
						ids[row.ID] = true
					}
				}
				provenance := []fhirr4.FieldProvenance{}
				for _, field := range projected.Provenance {
					if ids[field.Row] {
						provenance = append(provenance, field)
					}
				}
				projected.Rows = rows
				projected.Provenance = provenance
				if r.Search.Coverage != "complete" {
					projected.Status = "collection-unknown"
				}
			}
			if partialView && projected.Status == "complete" {
				projected.Status = "partial-resource"
			}
			if projected.Status != "complete" {
				projectionUnknown = true
			}
			r.Projections = append(r.Projections, projected)
		}
	}
	if p.request.Kind == "search" && r.Search.Coverage != "complete" {
		for i := range r.Projections {
			if r.Projections[i].Status == "complete" {
				r.Projections[i].Status = "collection-unknown"
			}
		}
	}
	if (r.ExecutionState == "succeeded" || r.ExecutionState == "not-modified") && (projectionUnknown || p.request.Kind == "search" && r.Search.Coverage != "complete") {
		r.State = "unknown"
	}
}
func sameParameters(want map[string][]string, self string) bool {
	u, e := url.Parse(self)
	if e != nil || self == "" {
		return false
	}
	got, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return false
	}
	for k, v := range want {
		copy := append([]string(nil), v...)
		actual := append([]string(nil), got[k]...)
		slices.Sort(copy)
		slices.Sort(actual)
		if !slices.Equal(copy, actual) {
			return false
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok && !strings.HasPrefix(k, "_getpages") && k != "_count" {
			return false
		}
	}
	return true
}
