package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/runcompare"
)

// The portable review of a connected packet. readmit-portable-review/v1 and
// readmit-portable-report/v1 are unchanged and keep their own readers.
const (
	ConnectedReviewSchema = "readmit-portable-review/v2"
	ConnectedReportSchema = "readmit-portable-report/v2"
	connectedReportBytes  = 16 << 20
)

// ConnectedReport is the typed report every rendering is generated from. It
// carries protocol-specific evidence summaries and relative evidence links
// into the nested packet, never raw JSON paragraphs.
type ConnectedReport struct {
	Schema               string                     `json:"schema"`
	PacketIdentity       string                     `json:"packet_identity"`
	ExportPolicy         string                     `json:"export_policy"`
	ContainsSourceValues bool                       `json:"contains_source_values"`
	EvidenceClass        string                     `json:"evidence_class"`
	Equivalence          ConnectedEquivalence       `json:"equivalence"`
	Runs                 []ReportRun                `json:"runs"`
	Comparison           *runcompare.FlowComparison `json:"comparison"`
	Limitations          []string                   `json:"limitations"`
}

// ReportRun is one retained lifecycle: its verified claims and each phase.
type ReportRun struct {
	Section string        `json:"section"`
	Run     ConnectedRun  `json:"run"`
	Phases  []ReportPhase `json:"phases"`
}

type ReportPhase struct {
	ID           string              `json:"id"`
	State        string              `json:"state"`
	Verdict      string              `json:"verdict"`
	Checks       []ReportCheck       `json:"checks"`
	Steps        []ReportStep        `json:"steps"`
	Observations []ReportObservation `json:"observations"`
	Bindings     []ReportBinding     `json:"bindings"`
}

// ReportCheck is one check with the kind of claim it makes, kept apart:
// transport acceptance, response outcome, profile validity or an observed
// workflow assertion. Evidence is a relative, inert reference.
type ReportCheck struct {
	ID       string `json:"id"`
	Claim    string `json:"claim"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence"`
}

// ReportStep is one stimulus attempt. A FHIR step names the reviewed method
// and request template and its response outcome class; a v2 step names its
// delivery. Uncertain is never folded into an outcome.
type ReportStep struct {
	Step      string `json:"step"`
	Protocol  string `json:"protocol"`
	Method    string `json:"method"`
	Request   string `json:"request"`
	Outcome   string `json:"outcome"`
	Response  string `json:"response"`
	Uncertain bool   `json:"uncertain"`
	Evidence  string `json:"evidence"`
}

// ReportObservation is one retained observation the evaluator read, with its
// declared boundary and every record's fields. A field keeps its state:
// present, empty, null and absent stay distinct.
type ReportObservation struct {
	Dataset  string         `json:"dataset"`
	Boundary string         `json:"boundary"`
	Meaning  string         `json:"meaning"`
	Usable   bool           `json:"usable"`
	Columns  []string       `json:"columns"`
	Records  []ReportRecord `json:"records"`
	Evidence string         `json:"evidence"`
}

type ReportRecord struct {
	Row    string        `json:"row"`
	Fields []ReportField `json:"fields"`
	Link   string        `json:"link"`
}

type ReportField struct {
	Column string `json:"column"`
	State  string `json:"state"`
	Text   string `json:"text"`
}

// ReportBinding is one entry of the runtime identity mapping: a declared
// response variable and the server-assigned value an actual response bound.
type ReportBinding struct {
	Variable string `json:"variable"`
	Value    string `json:"value"`
	Evidence string `json:"evidence"`
}

// ConnectedReviewManifest binds every rendering to the verified packet.
type ConnectedReviewManifest struct {
	Schema               string           `json:"schema"`
	State                string           `json:"state"`
	PacketIdentity       string           `json:"packet_identity"`
	ExportPolicy         string           `json:"export_policy"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	Files                []bundle.Payload `json:"files"`
}

// ConnectedReview has no execute or mutate method.
type ConnectedReview struct {
	Identity   string
	Manifest   ConnectedReviewManifest
	Report     ConnectedReport
	renderings map[string][]byte
}

func (r *ConnectedReview) Render(format string) ([]byte, error) {
	name, ok := reviewFormats[format]
	if !ok {
		return nil, errors.New("unsupported report format")
	}
	return bytes.Clone(r.renderings[name]), nil
}

var connectedReviewFamily = func() artifactdir.Family {
	f := sealed("manifest.json", "identity.sha256", nil, []string{"packet"}, "report.html", "report.md", "report.json", "junit.xml", "report.pdf")
	f.Layout.MaxFiles, f.Layout.MaxFileBytes, f.Layout.MaxBytes = connectedMaxFiles, connectedMaxFileBytes, connectedMaxBytes
	return f
}()

// ExportConnectedReview copies a verified connected packet byte for byte
// into a new private review and seals five renderings of its typed report.
func ExportConnectedReview(ctx context.Context, source, output string) (*ConnectedReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := artifactpath.Directory(source)
	if err != nil {
		return nil, err
	}
	files, err := readConnected(source, false)
	if err != nil {
		return nil, err
	}
	nested := make(map[string][]byte, len(files))
	for name, data := range files {
		nested["packet/"+name] = data
	}
	if err := connectedReviewBounds(nested); err != nil {
		return nil, err
	}
	review, err := artifactdir.Create(output, connectedReviewFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer review.Close()
	dir := review.Path()
	if err := copyFiles(review, nested, "", ""); err != nil {
		return nil, err
	}
	packet, err := OpenConnected(ctx, filepath.Join(dir, "packet"))
	if err != nil {
		return nil, err
	}
	doc, err := BuildConnectedReport(packet)
	if err != nil {
		return nil, err
	}
	renders, err := renderConnected(doc)
	if err != nil {
		return nil, err
	}
	for name, data := range renders {
		nested[name] = data
	}
	manifest := connectedReviewManifest(packet, nested)
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	nested["manifest.json"] = raw
	if err := connectedReviewBounds(nested); err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(renders)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := review.WriteFile(name, renders[name]); err != nil {
			return nil, err
		}
	}
	if err := review.WriteFile("manifest.json", raw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := review.Seal(nil); err != nil {
		return nil, err
	}
	return OpenConnectedReview(ctx, dir)
}

// OpenConnectedReview verifies the nested packet and regenerates every
// rendering from it, so a resealed invented verdict or injected markup fails.
func OpenConnectedReview(ctx context.Context, dir string) (*ConnectedReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	files, err := readConnected(dir, false)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed or unsupported connected portable review")
	raw := files["manifest.json"]
	var stored ConnectedReviewManifest
	if len(raw) > 64<<20 || string(files["identity.sha256"]) != digest(raw)+"\n" || json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil || stored.Schema != ConnectedReviewSchema {
		return nil, invalid
	}
	packet, err := OpenConnected(ctx, filepath.Join(dir, "packet"))
	if err != nil {
		return nil, err
	}
	canonical, err := encode(connectedReviewManifest(packet, files))
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	doc, err := BuildConnectedReport(packet)
	if err != nil {
		return nil, err
	}
	renders, err := renderConnected(doc)
	if err != nil {
		return nil, err
	}
	for name, data := range renders {
		if !bytes.Equal(files[name], data) {
			return nil, invalid
		}
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" || strings.HasPrefix(name, "packet/") {
			continue
		}
		if _, ok := renders[name]; !ok {
			return nil, invalid
		}
	}
	return &ConnectedReview{Identity: digest(raw), Manifest: stored, Report: doc, renderings: renders}, nil
}

func connectedReviewManifest(p *ConnectedPacket, files map[string][]byte) ConnectedReviewManifest {
	return ConnectedReviewManifest{Schema: ConnectedReviewSchema, State: "complete", PacketIdentity: p.Identity, ExportPolicy: p.Manifest.ExportPolicy, ContainsSourceValues: p.Manifest.ContainsSourceValues, Files: index(files)}
}

func connectedReviewBounds(files map[string][]byte) error {
	total := 0
	for name, data := range files {
		total += len(data)
		if len(files) > connectedMaxFiles || len(data) > connectedMaxFileBytes || total > connectedMaxBytes || len(name) > connectedMaxPath || strings.Count(name, "/") > connectedMaxDepth {
			return errors.New("connected portable review exceeds file, path or byte limits")
		}
	}
	return nil
}

// BuildConnectedReport derives the typed report from a verified packet only.
// It is deterministic: this release's reanalysis is not part of it.
func BuildConnectedReport(p *ConnectedPacket) (ConnectedReport, error) {
	doc := ConnectedReport{Schema: ConnectedReportSchema, PacketIdentity: p.Identity, ExportPolicy: p.Manifest.ExportPolicy, ContainsSourceValues: p.Manifest.ContainsSourceValues, EvidenceClass: p.Manifest.EvidenceClass, Equivalence: p.Manifest.Equivalence, Runs: []ReportRun{}, Comparison: p.Comparison, Limitations: []string{
		"Customer-local sensitive evidence: observed resources, request templates, bound server identities and historical configuration are shown as retained. This is not a disclosure-reviewed extract or a de-identification.",
		"Transport acceptance, response outcome, profile validity and observed workflow assertions are separate claims; none implies another.",
		"Each observation speaks only for its declared boundary. A missing or unusable observation is never an empty one.",
		"Verdicts are re-derived offline from retained evidence and pinned definitions; nothing was re-fetched, rerun or regenerated. Hashes establish integrity, not source authenticity, target software identity or approval.",
	}}
	if p.Manifest.Baseline == nil {
		doc.Limitations = append(doc.Limitations, "No observed baseline was supplied; this single-run report proves no before/after improvement or regression.")
	}
	runs := map[string]*ConnectedRun{"current": &p.Manifest.Current, "baseline": p.Manifest.Baseline, "replay": p.Manifest.Replay}
	for _, section := range connectedSections {
		run := runs[section]
		if run == nil {
			continue
		}
		e := p.evidence[section]
		rr := ReportRun{Section: section, Run: *run, Phases: []ReportPhase{}}
		for _, phase := range e.Result.Phases {
			rr.Phases = append(rr.Phases, reportPhase(section, e, phase))
		}
		doc.Runs = append(doc.Runs, rr)
	}
	raw, err := encode(doc)
	if err != nil {
		return doc, err
	}
	if len(raw) > connectedReportBytes {
		return doc, errors.New("connected report exceeds 16 MiB; select a smaller retained packet")
	}
	return doc, nil
}

func reportPhase(section string, e connectedrun.FlowEvidence, phase connectedrun.FlowPhaseResult) ReportPhase {
	base := section + "/phases/" + phase.ID + "/"
	pe := e.Phases[phase.ID]
	fhir := e.Result.Schema == connectedrun.FlowSchemaV4
	rp := ReportPhase{ID: phase.ID, State: phase.State, Verdict: string(phase.Verdict), Checks: []ReportCheck{}, Steps: []ReportStep{}, Observations: []ReportObservation{}, Bindings: []ReportBinding{}}
	var declared struct {
		responses   map[string]string
		validations map[string]string
	}
	declared.responses, declared.validations = map[string]string{}, map[string]string{}
	for _, fp := range e.Plan.Document().Test.Phases {
		if fp.ID == phase.ID {
			for _, r := range fp.Responses {
				declared.responses[r.ID] = r.Step
			}
			for _, v := range fp.Validations {
				declared.validations[v.ID] = v.Step
			}
		}
	}
	for _, c := range phase.Checks {
		kind, id, _ := strings.Cut(c.ID, ":")
		evidence := base + "evaluation"
		if fhir {
			evidence = base + "evaluation.json"
		}
		switch kind {
		case "wire":
			evidence = base + "transport/run"
		case "response":
			evidence = base + "steps/" + declared.responses[id]
		case "validation":
			evidence = base + "validations/" + id
		}
		if phase.State == "not-attempted" || phase.State == "blocked" || phase.State == "skipped" {
			evidence = "not retained: the phase was " + phase.State
		}
		rp.Checks = append(rp.Checks, ReportCheck{ID: c.ID, Claim: checkClaim(c.ID), Outcome: string(c.Outcome), Evidence: evidence})
	}
	records := map[string]connectedrun.FHIRStepRecord{}
	for _, s := range pe.Steps {
		records[s.Step] = s
	}
	for _, s := range phase.Steps {
		step := ReportStep{Step: s.Step, Protocol: "hl7-v2", Outcome: s.Outcome, Uncertain: s.Uncertain, Evidence: base + "transport/run"}
		if s.Kind == "fhir-interaction" {
			step.Protocol, step.Evidence = "fhir-r4", base+"steps/"+s.Step
			if plan := e.Plan.Phase(phase.ID); plan != nil {
				if call, err := plan.FHIRCallShape(s.Step); err == nil {
					step.Method, step.Request = call.Method, call.URL
				}
			}
			step.Response = records[s.Step].Outcome
		}
		if s.Outcome == "not-attempted" {
			step.Evidence = "not retained: never attempted"
		}
		rp.Steps = append(rp.Steps, step)
	}
	claims := map[string]connectedrun.FlowClaim{}
	for _, q := range e.Result.Qualification {
		if q.Phase == phase.ID {
			claims[q.Dataset] = q
		}
	}
	for _, id := range slices.Sorted(maps.Keys(pe.Tables)) {
		t := pe.Tables[id]
		o := ReportObservation{Dataset: id, Boundary: pe.Boundaries[id], Meaning: "Typed application or engine-output state as the declared source returned it.", Usable: t.Usable && (pe.Intervals[id].Schema == "" || pe.Intervals[id].Sufficient()), Columns: []string{}, Records: []ReportRecord{}, Evidence: base + pe.Observations[id]}
		if q, ok := claims[id]; ok {
			o.Boundary, o.Meaning = q.Boundary+" ("+pe.Boundaries[id]+")", q.Meaning
		}
		for _, c := range t.Columns {
			o.Columns = append(o.Columns, c.Name)
		}
		for _, row := range t.Rows {
			rec := ReportRecord{Row: row.ID, Fields: []ReportField{}, Link: o.Evidence + "#" + row.ID}
			for i, c := range t.Columns {
				f := ReportField{Column: c.Name, State: "absent"}
				if i < len(row.Values) {
					f.State, f.Text = row.Values[i].State, fieldText(row.Values[i])
				}
				rec.Fields = append(rec.Fields, f)
			}
			o.Records = append(o.Records, rec)
		}
		rp.Observations = append(rp.Observations, o)
	}
	for _, name := range slices.Sorted(maps.Keys(pe.Bound)) {
		rp.Bindings = append(rp.Bindings, ReportBinding{Variable: name, Value: pe.Bound[name], Evidence: base + "steps/" + bindingStep(e, phase.ID, name)})
	}
	return rp
}

// fieldText is one typed value as text; a repeated value lists its items.
func fieldText(v dataset.Value) string {
	if len(v.Items) == 0 {
		if v.CodeSystem != "" {
			return v.CodeSystem + "|" + v.Text
		}
		return v.Text
	}
	items := []string{}
	for _, item := range v.Items {
		items = append(items, item.State+":"+fieldText(item))
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func bindingStep(e connectedrun.FlowEvidence, phase, variable string) string {
	plan := e.Plan.Phase(phase)
	if plan == nil {
		return ""
	}
	for _, s := range plan.Document().Test.Steps {
		if s.Interaction == nil {
			continue
		}
		for _, b := range s.Interaction.Bind {
			if b.Variable == variable {
				return s.ID
			}
		}
	}
	return ""
}

// inert quotes evidence as printable ASCII. Every character Markdown or HTML
// could interpret, every control and every non-ASCII rune is a Go escape, so
// no evidence becomes markup and strconv.Unquote returns the exact bytes.
func inert(s string) string {
	var out strings.Builder
	out.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&out, "\\x%02x", s[i])
		case r < 0x80 && (r < 0x20 || r == 0x7f || strings.ContainsRune(`"\|<>&[]`+"`"+`*_#!(){}~:`, r)):
			fmt.Fprintf(&out, "\\x%02x", r)
		case r < 0x80:
			out.WriteRune(r)
		case r <= 0xffff:
			fmt.Fprintf(&out, "\\u%04x", r)
		default:
			fmt.Fprintf(&out, "\\U%08x", r)
		}
		i += size
	}
	out.WriteByte('"')
	return out.String()
}

// block is one part of the rendered document: a heading, a paragraph or a
// table. Every renderer draws the same blocks.
type block struct {
	heading int
	text    string
	head    []string
	rows    [][]string
}

func connectedBlocks(doc ConnectedReport) []block {
	b := []block{{heading: 1, text: "Readmit connected lifecycle review"}}
	p := func(text string) { b = append(b, block{text: text}) }
	p("Packet " + doc.PacketIdentity + " - " + doc.EvidenceClass + " - export policy " + doc.ExportPolicy + ". Read-only: no execute, transmit, reset or evidence change.")
	p("Regression equivalence: " + doc.Equivalence.State + ". " + doc.Equivalence.Reason)
	for _, run := range doc.Runs {
		r := run.Run
		b = append(b, block{heading: 2, text: "Run: " + run.Section})
		b = append(b, block{head: []string{"Fact", "Value"}, rows: [][]string{
			{"Result", r.Verdict + " (" + r.State + ")"}, {"Setup / cleanup", r.Setup + " / " + r.Cleanup}, {"Lifecycle", r.Identity + " " + r.Schema},
			{"Plan / test", r.Plan + " / " + inert(r.Test)}, {"Instance", inert(r.Instance)}, {"Engine at execution", inert(r.Engine)}, {"Boundary", r.Boundary},
			{"Environment", inert(r.Environment.Project + "/" + r.Environment.ID + " revision " + r.Environment.Revision + " (" + r.Environment.Classification + ")")},
			{"Target configuration / address policy", r.Environment.TargetIdentity + " / " + r.Environment.AddressPolicyIdentity},
			{"Target revision", inert(r.Environment.TargetRevision.Value) + " (" + r.Environment.TargetRevision.Provenance + ")"},
		}})
		for _, s := range r.Environment.Servers {
			p("FHIR server " + inert(s.ID) + ": " + inert(s.Base) + ", reviewed capability " + s.Capability)
		}
		for _, phase := range run.Phases {
			b = append(b, block{heading: 3, text: "Phase " + phase.ID + ": " + phase.State + ", " + phase.Verdict})
			checks := block{head: []string{"Check", "Claim", "Outcome", "Evidence"}}
			// Failed, undecided and skipped checks come before passed ones.
			ordered := slices.Clone(phase.Checks)
			slices.SortStableFunc(ordered, func(x, y ReportCheck) int {
				rank := func(o string) int {
					if o == string(assertion.OutcomePassed) {
						return 1
					}
					return 0
				}
				return rank(x.Outcome) - rank(y.Outcome)
			})
			for _, c := range ordered {
				checks.rows = append(checks.rows, []string{c.ID, c.Claim, c.Outcome, inert(c.Evidence)})
			}
			b = append(b, checks)
			if len(phase.Steps) > 0 {
				steps := block{head: []string{"Step", "Protocol", "Request", "Outcome", "Response", "Uncertain", "Evidence"}}
				for _, s := range phase.Steps {
					request, response := "-", "-"
					if s.Method != "" {
						request = inert(s.Method + " " + s.Request)
					}
					if s.Response != "" {
						response = s.Response
					}
					steps.rows = append(steps.rows, []string{s.Step, s.Protocol, request, s.Outcome, response, strconv.FormatBool(s.Uncertain), inert(s.Evidence)})
				}
				b = append(b, steps)
			}
			for _, o := range phase.Observations {
				p("Observation " + o.Dataset + " - boundary " + o.Boundary + ". " + o.Meaning + " Usable: " + strconv.FormatBool(o.Usable) + "; records: " + strconv.Itoa(len(o.Records)) + ". Evidence: " + inert(o.Evidence))
				table := block{head: append([]string{"Record"}, o.Columns...)}
				for _, rec := range o.Records {
					row := []string{inert(rec.Link)}
					for _, f := range rec.Fields {
						cell := f.State
						if f.State == "present" || f.Text != "" {
							cell = inert(f.Text)
						}
						row = append(row, cell)
					}
					table.rows = append(table.rows, row)
				}
				b = append(b, table)
			}
			if len(phase.Bindings) > 0 {
				bindings := block{head: []string{"Response variable", "Server-assigned value", "Evidence"}}
				for _, bd := range phase.Bindings {
					bindings.rows = append(bindings.rows, []string{bd.Variable, inert(bd.Value), inert(bd.Evidence)})
				}
				b = append(b, bindings)
			}
		}
	}
	if c := doc.Comparison; c != nil {
		b = append(b, block{heading: 2, text: "Baseline to current"})
		dims := block{head: []string{"Dimension", "State", "Changed declarations"}}
		for _, d := range c.Dimensions {
			dims.rows = append(dims.rows, []string{d.Dimension, d.State, inert(strings.Join(d.Changed, ", "))})
		}
		b = append(b, dims)
		p("Attribution: " + c.Attribution.Outcome + ". " + c.Attribution.Reason)
		checks := block{head: []string{"Phase", "Check", "Baseline", "Current", "Definition", "Behavior"}}
		for _, x := range c.Checks {
			checks.rows = append(checks.rows, []string{x.Phase, x.Check, x.Baseline, x.Current, x.Definition, x.Behavior})
		}
		b = append(b, checks)
		records := block{head: []string{"Phase", "Dataset", "Key", "Baseline", "Current", "State", "Changed fields", "Server-assigned"}}
		for _, r := range c.Records {
			if r.State != "compared" {
				records.rows = append(records.rows, []string{r.Phase, r.Dataset, "-", "-", "-", r.State + ": " + r.Reason, "", ""})
				continue
			}
			for _, k := range r.Keys {
				records.rows = append(records.rows, []string{r.Phase, r.Dataset, k.Key[:16], strconv.Itoa(k.Baseline), strconv.Itoa(k.Current), k.State, strings.Join(k.Values, ", "), strings.Join(k.Identities, ", ")})
			}
		}
		b = append(b, records)
		p(c.Scope)
	}
	b = append(b, block{heading: 2, text: "Limitations"})
	for _, l := range doc.Limitations {
		p(l)
	}
	return b
}

func renderConnected(doc ConnectedReport) (map[string][]byte, error) {
	document, err := encode(doc)
	if err != nil {
		return nil, err
	}
	blocks := connectedBlocks(doc)
	var md, page strings.Builder
	lines := []string{}
	page.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; sandbox\"><meta name=\"referrer\" content=\"no-referrer\"><title>Readmit connected lifecycle review</title><style>body{font:14px/20px system-ui,sans-serif;max-width:760px;margin:24px auto;padding:0 16px}table{border-collapse:collapse;margin:8px 0}th,td{border:1px solid #999;padding:2px 6px;text-align:left;vertical-align:top;overflow-wrap:anywhere}</style></head><body>\n")
	for _, bl := range blocks {
		switch {
		case bl.heading > 0:
			fmt.Fprintf(&md, "%s %s\n\n", strings.Repeat("#", bl.heading), bl.text)
			fmt.Fprintf(&page, "<h%d>%s</h%d>\n", bl.heading, html.EscapeString(bl.text), bl.heading)
			lines = append(lines, "", strings.ToUpper(bl.text))
		case bl.head != nil:
			md.WriteString("| " + strings.Join(bl.head, " | ") + " |\n|" + strings.Repeat(" --- |", len(bl.head)) + "\n")
			page.WriteString("<table><thead><tr>")
			for _, h := range bl.head {
				page.WriteString("<th scope=\"col\">" + html.EscapeString(h) + "</th>")
			}
			page.WriteString("</tr></thead><tbody>\n")
			lines = append(lines, strings.Join(bl.head, " | "))
			for _, row := range bl.rows {
				md.WriteString("| " + strings.Join(row, " | ") + " |\n")
				page.WriteString("<tr>")
				for _, cell := range row {
					page.WriteString("<td>" + html.EscapeString(cell) + "</td>")
				}
				page.WriteString("</tr>\n")
				lines = append(lines, "  "+strings.Join(row, " | "))
			}
			md.WriteString("\n")
			page.WriteString("</tbody></table>\n")
		default:
			md.WriteString(bl.text + "\n\n")
			page.WriteString("<p>" + html.EscapeString(bl.text) + "</p>\n")
			lines = append(lines, bl.text)
		}
	}
	page.WriteString("</body></html>\n")
	suite := junitReview{Name: "connected-lifecycle", Content: md.String()}
	for _, run := range doc.Runs {
		if run.Run.State != "complete" {
			suite.Cases = append(suite.Cases, junitCase{Name: run.Section + " lifecycle", Error: &junitFailure{Message: "Lifecycle " + run.Run.State + "; setup " + run.Run.Setup + ", cleanup " + run.Run.Cleanup + ". No successful regression proof."}})
			suite.Errors++
		}
		for _, phase := range run.Phases {
			for _, c := range phase.Checks {
				entry := junitCase{Name: run.Section + " " + phase.ID + " " + c.ID}
				switch c.Outcome {
				case string(assertion.OutcomePassed):
				case string(assertion.OutcomeFailed):
					entry.Failure = &junitFailure{Message: c.Claim + " failed; see the report and " + c.Evidence}
					suite.Failures++
				default:
					entry.Error = &junitFailure{Message: c.Claim + " " + c.Outcome + "; no verdict"}
					suite.Errors++
				}
				suite.Cases = append(suite.Cases, entry)
			}
		}
	}
	suite.Tests = len(suite.Cases)
	junit, err := xml.Marshal(suite)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"report.html": []byte(page.String()), "report.md": []byte(md.String()), "report.json": document, "junit.xml": append([]byte(xml.Header), junit...), "report.pdf": pdfReport(lines)}, nil
}

// RenderConnectedReport renders a verified packet through the same typed
// report and inert renderers as ExportConnectedReview, without writing a review.
func RenderConnectedReport(p *ConnectedPacket, format string) ([]byte, error) {
	doc, err := BuildConnectedReport(p)
	if err != nil {
		return nil, err
	}
	files, err := renderConnected(doc)
	if err != nil {
		return nil, err
	}
	name, ok := reviewFormats[format]
	if !ok {
		return nil, errors.New("unsupported connected report format")
	}
	return bytes.Clone(files[name]), nil
}
