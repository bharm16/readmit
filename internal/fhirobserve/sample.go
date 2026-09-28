package fhirobserve

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const SampleSchema = "readmit-fhir-observation-sample/v1"

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR observation sample", Nested: []string{"http"}, RequiredFiles: []string{"sample.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "sample.json" || n == "identity.sha256"
}, MaxFiles: 1024, MaxFileBytes: 32 << 20, MaxBytes: 260 << 20}, Seal: artifactdir.DirectoryHash(SampleSchema)}

var relative = regexp.MustCompile(`^([A-Z][A-Za-z]{0,63})/([A-Za-z0-9\-.]{1,64})(/_history/[A-Za-z0-9\-.]{1,64})?$`)

// TypedRows is the derived typed content. Only complete rows can support a
// check; every other status is unusable evidence, never an empty result.
type TypedRows struct {
	Status string        `json:"status"`
	Rows   []dataset.Row `json:"rows"`
}
type Record struct {
	Schema      string          `json:"schema"`
	Observation string          `json:"observation"`
	Binding     dataset.Binding `json:"binding"`
	HTTP        string          `json:"http_identity"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt time.Time       `json:"completed_at"`
	Bytes       int             `json:"bytes"`
	Coverage    string          `json:"coverage"`
	Table       TypedRows       `json:"table"`
}

// Sample is one verified retained search acquisition and its derived table.
type Sample struct {
	record      Record
	observation Observation
	files       map[string][]byte
	identity    string
}

func (s *Sample) Identity() string          { return s.identity }
func (s *Sample) Usable() bool              { return s != nil && s.record.Table.Status == "complete" }
func (s *Sample) Binding() dataset.Binding  { return s.record.Binding }
func (s *Sample) Started() time.Time        { return s.record.StartedAt }
func (s *Sample) Completed() time.Time      { return s.record.CompletedAt }
func (s *Sample) Records() int              { return len(s.record.Table.Rows) }
func (s *Sample) Size() int                 { return s.record.Bytes }
func (s *Sample) Status() string            { return s.record.Table.Status }
func (s *Sample) Record() Record            { var r Record; _ = json.Unmarshal(canonical(s.record), &r); return r }
func (Sample) Format(w fmt.State, _ rune)   { _, _ = fmt.Fprint(w, "FHIR observation sample (private)") }
func (Sample) MarshalJSON() ([]byte, error) { return nil, invalid }
func (s *Sample) Write(ctx context.Context, output string) error {
	files := maps.Clone(s.files)
	delete(files, "identity.sha256")
	_, err := artifactdir.Write(ctx, output, family, artifactdir.Durable, files)
	return err
}

// Table adapts the sample for the shared typed evaluator.
func (s *Sample) Table() assertion.Table {
	return assertion.Table{Identity: s.identity, Binding: s.record.Binding, Projection: s.observation.ProjectionIdentity(), ProjectionID: s.observation.ID, Columns: s.observation.TypedColumns(), Usable: s.Usable(), Rows: s.record.Table.Rows}
}

// Expect is the exact request a caller prepared; a sample read under another
// base, address or operation is refused rather than reinterpreted.
type Expect struct {
	Base, URL string
	Binding   dataset.Binding
}

// Acquire executes the prepared search once through the shared executor and
// seals its retained evidence with the derived table.
func Acquire(ctx context.Context, o Observation, expect Expect, plan *fhirrest.Plan, authority networkaction.Authority, provider networkaction.RuntimeProvider, resolve sendpolicy.Resolver, output string) (*Sample, error) {
	if o.Validate() != nil || plan == nil || !matchesPlan(plan.Declaration(), expect, o) || dataset.ValidateBinding(expect.Binding) != nil || expect.Binding.Source != o.Identity() {
		return nil, invalid
	}
	w, err := artifactdir.Create(output, family, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	if _, err = plan.Execute(ctx, authority, provider, filepath.Join(w.Path(), "http"), resolve); err != nil {
		return nil, err
	}
	evidence, err := fhirrest.OpenEvidence(context.WithoutCancel(ctx), filepath.Join(w.Path(), "http"))
	if err != nil {
		return nil, err
	}
	record := derive(context.WithoutCancel(ctx), o, expect, evidence)
	if w.WriteFile("sample.json", canonical(record)) != nil {
		return nil, invalid
	}
	if _, err = w.Seal(nil); err != nil {
		return nil, err
	}
	return Open(context.WithoutCancel(ctx), w.Path(), o, expect)
}

// Open re-derives the table from the retained response bytes. It never reads
// the original server, resolves a credential or repeats an acquisition.
func Open(ctx context.Context, directory string, o Observation, expect Expect) (*Sample, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return nil, err
	}
	identity := strings.TrimSpace(string(files["identity.sha256"]))
	if identity != artifactdir.Identity(SampleSchema, files) || o.Validate() != nil {
		return nil, invalid
	}
	evidence, err := fhirrest.OpenEvidence(ctx, filepath.Join(directory, "http"))
	if err != nil || !artifactdir.MatchesSubtree(files, "http", fhirrest.ResultSchema, evidence.Identity()) {
		return nil, invalid
	}
	var plan fhirrest.Spec
	if json.Unmarshal(files["http/plan.json"], &plan) != nil || !matchesPlan(plan, expect, o) {
		return nil, invalid
	}
	var declared Record
	if json.Unmarshal(files["sample.json"], &declared, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	// An interval reader supplies no binding: the retained one is used and the
	// reader checks its scope; it must still name this exact observation.
	if expect.Binding == (dataset.Binding{}) {
		if dataset.ValidateBinding(declared.Binding) != nil || declared.Binding.Source != o.Identity() {
			return nil, invalid
		}
		expect.Binding = declared.Binding
	}
	rebuilt := derive(ctx, o, expect, evidence)
	if string(canonical(rebuilt)) != string(canonical(declared)) {
		return nil, invalid
	}
	return &Sample{record: rebuilt, observation: o, files: files, identity: identity}, nil
}

func matchesPlan(s fhirrest.Spec, expect Expect, o Observation) bool {
	h := s.HTTP.HTTP
	return s.Base == expect.Base && h.URL == expect.URL && h.Method == "GET" && h.Operation == sendpolicy.FHIRSearch && len(h.Body) == 0 && s.Projection == nil && s.Prior == nil && s.Budget == o.Budget && s.Retry == o.Retry && strings.HasPrefix(expect.URL, expect.Base+"/"+o.Resource)
}

func derive(ctx context.Context, o Observation, expect Expect, evidence *fhirrest.Evidence) Record {
	result := evidence.Result()
	r := Record{Schema: SampleSchema, Observation: o.Identity(), Binding: expect.Binding, HTTP: evidence.Identity(), Coverage: result.Search.Coverage, Table: TypedRows{Status: "complete", Rows: []dataset.Row{}}}
	for i, a := range result.Attempts {
		if i == 0 || a.Started.Before(r.StartedAt) {
			r.StartedAt = a.Started
		}
		if a.Completed.After(r.CompletedAt) {
			r.CompletedAt = a.Completed
		}
		if body, ok := evidence.ResponseBytes(i); ok {
			r.Bytes += len(body)
		}
	}
	fail := func(status string) Record {
		r.Table = TypedRows{Status: status, Rows: []dataset.Row{}}
		return r
	}
	if result.ExecutionState != "succeeded" || result.State != "succeeded" {
		return fail("acquisition-" + result.State)
	}
	if result.Search.Coverage != "complete" {
		return fail("coverage-" + result.Search.Coverage)
	}
	fhirColumns := 0
	for _, c := range o.Columns {
		if c.Value.Kind != "identity" {
			fhirColumns++
		}
	}
	seen := map[string]bool{}
	values := 0
	for i, a := range result.Attempts {
		if a.Phase != "interaction" || a.Outcome.State != "succeeded" {
			continue
		}
		body, _ := evidence.ResponseBytes(i)
		d, err := fhirr4.Decode(ctx, body, fhirr4.Context{Version: fhirr4.Version, Base: expect.Base, MediaType: "application/fhir+json"})
		if err != nil {
			return fail("protocol-invalid")
		}
		bundle, err := d.HTTPBundle()
		if err != nil {
			return fail("protocol-invalid")
		}
		resources := map[string]fhirr4.Resource{}
		matched := map[string]bool{}
		for _, resource := range d.Resources() {
			resources[resource.Occurrence] = resource
		}
		for _, m := range fhirrest.CorrelateEntries(bundle, d.Resources()) {
			if m.Mode != "match" || !m.Found || m.Type != o.Resource || m.LogicalID == "" {
				continue
			}
			// Repeated logical resources across pages are one entity, as
			// fhirrest's own search accounting states; versions are compared there.
			if !seen[m.Identity] {
				seen[m.Identity] = true
				matched[m.Occurrence] = true
			}
		}
		projected, err := d.Project(ctx, dataset.Binding{Run: expect.Binding.Run, Phase: expect.Binding.Phase, Namespace: expect.Binding.Namespace, Source: expect.Binding.Source}, o.projection())
		if err != nil {
			return fail("projection-invalid")
		}
		if projected.Status != "complete" {
			return fail("projection-" + projected.Status)
		}
		for _, row := range projected.Rows {
			if !matched[row.Provenance.SourceRecord] {
				continue
			}
			if len(row.Values) != fhirColumns || len(r.Table.Rows) >= o.MaxRows {
				return fail("row-limit")
			}
			resource := resources[row.Provenance.SourceRecord]
			out := dataset.Row{ID: fmt.Sprintf("row-%06d", len(r.Table.Rows)+1), Values: []dataset.Value{}, Provenance: row.Provenance}
			out.Provenance.Record = len(r.Table.Rows) + 1
			next := 0
			for _, c := range o.Columns {
				var v dataset.Value
				switch c.Value.Kind {
				case "identity":
					v = dataset.Value{State: "present", Type: "text", Text: resource.Type + "/" + resource.LogicalID}
				case "reference":
					v = reference(row.Values[next], expect.Base)
					next++
				default:
					v = row.Values[next]
					next++
				}
				v, ok := typed(c, v)
				if !ok {
					return fail("type-mismatch")
				}
				if c.Required && v.State != "present" {
					return fail("required-absent")
				}
				values++
				out.Values = append(out.Values, v)
			}
			if values > o.MaxValues {
				return fail("value-limit")
			}
			r.Table.Rows = append(r.Table.Rows, out)
		}
	}
	return r
}

// reference keeps logical identity only when it is unambiguous under the one
// declared base. Contained, URN, logical-identifier and foreign references are
// unsupported values rather than guesses.
func reference(v dataset.Value, base string) dataset.Value {
	if v.State != "present" {
		return dataset.Value{State: v.State, Type: "text"}
	}
	text := strings.TrimPrefix(v.Text, base+"/")
	m := relative.FindStringSubmatch(text)
	if m == nil || strings.Contains(text, "://") {
		return dataset.Value{State: "unsupported", Type: "text"}
	}
	return dataset.Value{State: "present", Type: "text", Text: m[1] + "/" + m[2]}
}

// typed applies the declared column type. A FHIR code carries its declared
// binding system; any other disagreement is a mismatch, never a conversion.
func typed(c Column, v dataset.Value) (dataset.Value, bool) {
	scalar := func(item dataset.Value) (dataset.Value, bool) {
		if item.State != "present" {
			if item.State == "invalid" || item.State == "unsupported" || item.State == "ambiguous" {
				return item, false
			}
			return dataset.Value{State: item.State, Type: c.Type}, true
		}
		if item.Type != c.Type || item.Items != nil {
			return item, false
		}
		if c.Type == "code" {
			if item.CodeSystem != "" && item.CodeSystem != c.CodeSystem {
				return item, false
			}
			item.CodeSystem = c.CodeSystem
		}
		return item, true
	}
	if !c.Repeated {
		return scalar(v)
	}
	if v.State != "present" {
		return scalar(dataset.Value{State: v.State, Type: c.Type})
	}
	out := dataset.Value{State: "present", Type: c.Type, Items: []dataset.Value{}}
	for _, item := range v.Items {
		typedItem, ok := scalar(item)
		if !ok {
			return v, false
		}
		out.Items = append(out.Items, typedItem)
	}
	return out, true
}
