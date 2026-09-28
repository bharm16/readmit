package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/exportreview"
)

// A disclosure-reviewed extract of a connected packet is the share-oriented
// export of connected evidence. It is value-free: it holds outcomes, counts,
// declared boundaries and commitments, never a retained byte. Every evidence
// surface the packet holds must be mapped by the approved policy, or the
// extract is refused; credential material is refused whatever the policy says.
const (
	DisclosurePolicySchema = "readmit-connected-disclosure-policy/v1"
	ExtractSchema          = "readmit-connected-extract/v1"
	extractMaxBytes        = 4 << 20
)

// The evidence surfaces a connected packet can hold. A file belongs to one
// surface by where it is retained; FHIR content additionally reports the
// narrative, extension and attachment surfaces it carries.
const (
	SurfaceFHIRResource  = "fhir-resource"
	SurfaceFHIRNarrative = "fhir-narrative"
	SurfaceFHIRExtension = "fhir-extension"
	SurfaceAttachment    = "fhir-attachment"
	SurfaceHTTPExchange  = "http-exchange"
	SurfaceHTTPResponse  = "http-response"
	SurfaceHL7Transport  = "hl7-transport"
	SurfaceTypedDataset  = "typed-dataset"
	SurfaceMapping       = "identity-mapping"
	SurfaceSetup         = "setup-resource"
	SurfaceValidator     = "validator-diagnostics"
	SurfacePlan          = "plan-definition"
	SurfaceLifecycle     = "lifecycle-record"
	SurfaceCompletion    = "completion-record"
	SurfacePacket        = "packet-metadata"
	SurfaceCredential    = "credential-material"
)

// DisclosureSurfaces are the surfaces a disclosure policy may map, in order.
// Credential material is deliberately absent: no policy can admit it.
var DisclosureSurfaces = []string{SurfaceFHIRResource, SurfaceFHIRNarrative, SurfaceFHIRExtension, SurfaceAttachment, SurfaceHTTPExchange, SurfaceHTTPResponse, SurfaceHL7Transport, SurfaceTypedDataset, SurfaceMapping, SurfaceSetup, SurfaceValidator, SurfacePlan, SurfaceLifecycle, SurfaceCompletion, SurfacePacket}

// DisclosurePolicy maps each surface to its disposition. The only disposition
// this version offers is "exclude": the surface's bytes never leave the packet
// and only its file count is reported. A surface the policy does not name
// blocks the extract.
type DisclosurePolicy struct {
	Schema   string            `json:"schema"`
	Surfaces map[string]string `json:"surfaces"`
}

func DecodeDisclosurePolicy(raw []byte) (DisclosurePolicy, error) {
	var p DisclosurePolicy
	refused := errors.New("the disclosure policy is not a readmit-connected-disclosure-policy/v1 this release reads")
	if len(raw) > 16384 || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != DisclosurePolicySchema || p.Surfaces == nil {
		return p, refused
	}
	for surface, disposition := range p.Surfaces {
		if !slices.Contains(DisclosureSurfaces, surface) || disposition != "exclude" {
			return p, refused
		}
	}
	return p, nil
}

// InventoryItem is one surface of the packet: how many files hold it and,
// in the customer-local preview only, where.
type InventoryItem struct {
	Surface     string   `json:"surface"`
	Files       int      `json:"files"`
	Disposition string   `json:"disposition"`
	Locations   []string `json:"-"`
}

var (
	privateKey  = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	bearerToken = regexp.MustCompile(`(?i)bearer[ ]+[A-Za-z0-9\-._~+/]{8,}=*`)
	jwt         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)
	tokenMember = regexp.MustCompile(`"(access_token|refresh_token|id_token|client_assertion)"\s*:\s*"[^"]`)
	clientForm  = regexp.MustCompile(`client_assertion=[A-Za-z0-9]`)
)

// credential reports whether bytes carry a token or private key. Retained
// evidence never should; finding one refuses every share-oriented export.
func credential(data []byte) bool {
	return privateKey.Match(data) || bearerToken.Match(data) || jwt.Match(data) || tokenMember.Match(data) || clientForm.Match(data)
}

// Inventory classifies every file of a verified packet into its
// evidence surfaces.
func (p *ConnectedPacket) Inventory(ctx context.Context, dir string) ([]InventoryItem, error) {
	files, err := readConnected(dir, false)
	if err != nil {
		return nil, err
	}
	if changes := indexChanges(p.Manifest.Files, files); len(changes) > 0 {
		return nil, &ChangedEvidenceError{Changes: changes}
	}
	found := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, surface := range surfacesOf(name, files[name]) {
			found[surface] = append(found[surface], name)
		}
	}
	items := []InventoryItem{}
	for _, surface := range append(slices.Clone(DisclosureSurfaces), SurfaceCredential) {
		if len(found[surface]) > 0 {
			items = append(items, InventoryItem{Surface: surface, Files: len(found[surface]), Disposition: "unmapped", Locations: found[surface]})
		}
	}
	return items, nil
}

func surfacesOf(name string, data []byte) []string {
	surfaces := []string{}
	if credential(data) {
		surfaces = append(surfaces, SurfaceCredential)
	}
	section, rel, found := strings.Cut(name, "/")
	if !found || !slices.Contains(connectedSections, section) {
		return append(surfaces, SurfacePacket)
	}
	fhir := fhirSurfaces(data)
	switch {
	case strings.HasPrefix(rel, "isolation/") || strings.HasPrefix(rel, "transitions/") || strings.HasPrefix(rel, "continuation/") || strings.HasPrefix(rel, "previous/") || strings.HasPrefix(rel, "preflight/"):
		return append(surfaces, SurfaceSetup)
	case strings.HasPrefix(rel, "plan/"):
		return append(append(surfaces, SurfacePlan), fhir...)
	case !strings.HasPrefix(rel, "phases/"):
		return append(surfaces, SurfaceLifecycle)
	}
	_, rest, _ := strings.Cut(strings.TrimPrefix(rel, "phases/"), "/")
	http := strings.Contains(rest, "/http/")
	switch {
	case strings.HasPrefix(rest, "plan/"):
		return append(append(surfaces, SurfacePlan), fhir...)
	case strings.HasPrefix(rest, "validations/"):
		return append(append(surfaces, SurfaceValidator), fhir...)
	case strings.HasPrefix(rest, "transport/"):
		return append(surfaces, SurfaceHL7Transport)
	case rest == "manifest.json" || rest == "started.json":
		return append(surfaces, SurfaceMapping)
	case strings.HasSuffix(rest, ".bin") && (http || strings.HasPrefix(rest, "steps/") || strings.HasPrefix(rest, "preflight/")):
		if len(fhir) > 0 || isFHIR(data) {
			return append(append(surfaces, SurfaceFHIRResource), fhir...)
		}
		return append(surfaces, SurfaceHTTPResponse)
	case http || strings.HasPrefix(rest, "steps/") || strings.HasPrefix(rest, "preflight/"):
		return append(surfaces, SurfaceHTTPExchange)
	case strings.HasPrefix(rest, "observations/") || strings.HasPrefix(rest, "evaluation/datasets/") || strings.Contains(rest, "/samples/") || strings.HasPrefix(rest, "intervals/") && strings.Contains(rest, "/dataset"):
		return append(surfaces, SurfaceTypedDataset)
	case strings.HasPrefix(rest, "intervals/"):
		return append(surfaces, SurfaceCompletion)
	}
	return append(surfaces, SurfaceLifecycle)
}

func isFHIR(data []byte) bool {
	var head struct {
		ResourceType string `json:"resourceType"`
	}
	return json.Unmarshal(data, &head) == nil && head.ResourceType != ""
}

// fhirSurfaces reports the narrative, extension and attachment surfaces a
// FHIR JSON document carries anywhere in its tree, including bundle entries.
func fhirSurfaces(data []byte) []string {
	if !isFHIR(data) {
		return nil
	}
	var tree any
	if json.Unmarshal(data, &tree) != nil {
		return nil
	}
	found := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if text, ok := v["text"].(map[string]any); ok && text["div"] != nil {
				found[SurfaceFHIRNarrative] = true
			}
			if v["extension"] != nil || v["modifierExtension"] != nil {
				found[SurfaceFHIRExtension] = true
			}
			if _, typed := v["contentType"]; typed && (v["data"] != nil || v["url"] != nil) || v["resourceType"] == "Binary" {
				found[SurfaceAttachment] = true
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(tree)
	out := []string{}
	for _, s := range []string{SurfaceFHIRNarrative, SurfaceFHIRExtension, SurfaceAttachment} {
		if found[s] {
			out = append(out, s)
		}
	}
	return out
}

// Extract is the value-free share-oriented document. Phases and checks are
// named by position and claim, never by authored text; record keys by
// position, never by value or digest.
type Extract struct {
	Schema         string               `json:"schema"`
	PacketIdentity string               `json:"packet_identity"`
	PolicyIdentity string               `json:"policy_identity"`
	EvidenceClass  string               `json:"evidence_class"`
	Equivalence    ConnectedEquivalence `json:"equivalence"`
	Runs           []ExtractRun         `json:"runs"`
	Comparison     *ExtractComparison   `json:"comparison"`
	Inventory      []InventoryItem      `json:"inventory"`
	Residual       exportreview.Scan    `json:"residual"`
	Scope          string               `json:"scope"`
}

type ExtractRun struct {
	Section        string         `json:"section"`
	Identity       string         `json:"identity"`
	Schema         string         `json:"schema"`
	Plan           string         `json:"plan"`
	State          string         `json:"state"`
	Verdict        string         `json:"verdict"`
	Setup          string         `json:"setup"`
	Cleanup        string         `json:"cleanup"`
	Boundary       string         `json:"boundary"`
	Classification string         `json:"classification"`
	Boundaries     []string       `json:"observation_boundaries"`
	Phases         []ExtractPhase `json:"phases"`
}

type ExtractPhase struct {
	Position int            `json:"position"`
	State    string         `json:"state"`
	Verdict  string         `json:"verdict"`
	Checks   []ExtractCheck `json:"checks"`
}

type ExtractCheck struct {
	Position int    `json:"position"`
	Claim    string `json:"claim"`
	Outcome  string `json:"outcome"`
}

type ExtractComparison struct {
	Dimensions  []string `json:"dimensions"`
	Attribution string   `json:"attribution"`
	Behavior    []string `json:"behavior"`
	Records     []string `json:"records"`
}

const extractScope = "Value-free extract of customer-local evidence: outcomes, counts, declared boundaries and commitments only; every retained byte stays customer-local. The residual scan is one check of known values, not an identification assessment or a legal de-identification determination. It is not an equivalent reproducer: no external replay of this extract exists."

// ExtractCandidate is a prepared extract. Publish regenerates it and writes
// only when the regenerated bytes are exactly the approved ones.
type ExtractCandidate struct {
	packet, policy string
	raw            []byte
	Extract        Extract
	Blocked        []string
}

func (c *ExtractCandidate) Identity() string { return digest(c.raw) }

// PrepareExtract verifies the packet, inventories every surface, applies the
// policy and builds the value-free extract. Blocked lists every reason the
// extract cannot be published: an unmapped surface, credential material or a
// known value found in the extract.
func PrepareExtract(ctx context.Context, packetPath, policyPath string) (*ExtractCandidate, error) {
	raw, err := disclosurePolicyFile.Read(policyPath)
	if err != nil {
		return nil, err
	}
	policy, err := DecodeDisclosurePolicy(raw)
	if err != nil {
		return nil, err
	}
	packet, err := OpenConnected(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(packetPath)
	if err != nil {
		return nil, err
	}
	inventory, err := packet.Inventory(ctx, dir)
	if err != nil {
		return nil, err
	}
	c := &ExtractCandidate{packet: packetPath, policy: policyPath, Blocked: []string{}}
	for i, item := range inventory {
		switch {
		case item.Surface == SurfaceCredential:
			c.Blocked = append(c.Blocked, "credential material (a token or private key) is retained in "+strconv.Itoa(item.Files)+" files; no policy can admit it and no share-oriented export is made")
		case policy.Surfaces[item.Surface] == "":
			c.Blocked = append(c.Blocked, "the policy does not map the "+item.Surface+" surface held in "+strconv.Itoa(item.Files)+" files")
		default:
			inventory[i].Disposition = policy.Surfaces[item.Surface]
		}
	}
	x := Extract{Schema: ExtractSchema, PacketIdentity: packet.Identity, PolicyIdentity: digest(raw), EvidenceClass: EvidenceExtract, Equivalence: ConnectedEquivalence{State: EquivalenceUnverified, Reason: "No external replay of this extract is retained, so it is not described as an equivalent reproducer. Its disclosure review is unaffected."}, Runs: []ExtractRun{}, Inventory: inventory, Scope: extractScope}
	x.Runs = valueFreeRuns(packet)
	x.Comparison = valueFreeComparison(packet)
	// The extract is scanned for every value the packet observed or bound, so
	// a value that reached it through any path blocks it.
	x.Residual = exportreview.Scan{Status: "passed", Locations: []string{}}
	body, err := encode(x)
	if err != nil {
		return nil, err
	}
	x.Residual = exportreview.Residual(map[string][]byte{"extract.json": body}, knownValues(packet))
	if x.Residual.Status != "passed" {
		c.Blocked = append(c.Blocked, "a value the packet retained appears in the extract")
	}
	c.Extract = x
	if c.raw, err = encode(x); err != nil {
		return nil, err
	}
	if len(c.raw) > extractMaxBytes {
		return nil, errors.New("the extract exceeds 4 MiB; select a smaller packet")
	}
	return c, nil
}

// valueFreeRuns summarizes every retained lifecycle by position and claim:
// states, verdicts, declared boundaries and check outcomes, never authored
// text or a value.
func valueFreeRuns(packet *ConnectedPacket) []ExtractRun {
	out := []ExtractRun{}
	runs := map[string]*ConnectedRun{"current": &packet.Manifest.Current, "baseline": packet.Manifest.Baseline, "replay": packet.Manifest.Replay}
	for _, section := range connectedSections {
		r := runs[section]
		if r == nil {
			continue
		}
		run := ExtractRun{Section: section, Identity: r.Identity, Schema: r.Schema, Plan: r.Plan, State: r.State, Verdict: r.Verdict, Setup: r.Setup, Cleanup: r.Cleanup, Boundary: r.Boundary, Classification: r.Environment.Classification, Boundaries: []string{}, Phases: []ExtractPhase{}}
		for _, q := range r.Qualification {
			run.Boundaries = append(run.Boundaries, q.Boundary)
		}
		for i, phase := range r.Phases {
			p := ExtractPhase{Position: i + 1, State: phase.State, Verdict: phase.Verdict, Checks: []ExtractCheck{}}
			for j, check := range phase.Checks {
				p.Checks = append(p.Checks, ExtractCheck{Position: j + 1, Claim: checkClaim(check.ID), Outcome: string(check.Outcome)})
			}
			run.Phases = append(run.Phases, p)
		}
		out = append(out, run)
	}
	return out
}

// valueFreeComparison is the packet's baseline comparison by dimension state,
// behavior and record counts only.
func valueFreeComparison(packet *ConnectedPacket) *ExtractComparison {
	cmp := packet.Comparison
	if cmp == nil {
		return nil
	}
	ec := &ExtractComparison{Dimensions: []string{}, Attribution: cmp.Attribution.Outcome, Behavior: []string{}, Records: []string{}}
	for _, d := range cmp.Dimensions {
		ec.Dimensions = append(ec.Dimensions, d.Dimension+": "+d.State)
	}
	for _, check := range cmp.Checks {
		ec.Behavior = append(ec.Behavior, check.Baseline+" -> "+check.Current+"; definition "+check.Definition+"; behavior "+check.Behavior)
	}
	for _, r := range cmp.Records {
		if r.State != "compared" {
			ec.Records = append(ec.Records, r.State)
			continue
		}
		for _, k := range r.Keys {
			ec.Records = append(ec.Records, strconv.Itoa(k.Baseline)+" -> "+strconv.Itoa(k.Current)+"; "+k.State+"; "+strconv.Itoa(len(k.Values))+" field columns; "+strconv.Itoa(len(k.Identities))+" server-assigned columns")
		}
	}
	return ec
}

// knownValues are the values the packet's lifecycles observed, bound and
// declared: every retained record field, every server-assigned identity, the
// server bases, environment names and target revision. Short and purely
// numeric values would match the extract's own counts and digests, so a
// known value needs four characters and a letter.
func knownValues(p *ConnectedPacket) [][]byte {
	seen := map[string]bool{}
	add := func(v string) {
		if len(v) >= 4 && strings.IndexFunc(v, unicode.IsLetter) >= 0 && !hexDigest(v) {
			seen[v] = true
		}
	}
	for _, e := range p.evidence {
		env := e.Plan.Document().Test.Environment
		for _, v := range []string{env.Project, env.ID, env.Name, env.Endpoint, env.TargetRevision.Value} {
			add(v)
		}
		for _, s := range e.Plan.Document().Test.Servers {
			add(s.Base)
		}
		for _, phase := range e.Phases {
			for _, t := range phase.Tables {
				for _, row := range t.Rows {
					for _, v := range row.Values {
						add(v.Text)
						for _, item := range v.Items {
							add(item.Text)
						}
					}
				}
			}
			for _, v := range phase.Bound {
				add(v)
			}
			for _, v := range phase.Inputs {
				add(v)
			}
		}
	}
	terms := [][]byte{}
	for _, v := range slices.Sorted(maps.Keys(seen)) {
		terms = append(terms, []byte(v))
	}
	return terms
}

func hexDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	return strings.IndexFunc(v, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) }) < 0
}

var disclosurePolicyFile = artifactdir.Document{
	MaxBytes: 16384,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("the disclosure policy must be a bounded regular file"),
		Read:      errors.New("the disclosure policy must be a bounded regular file"),
	},
}

var extractFamily = artifactdir.Family{
	Layout: artifactdir.Layout{AllowFile: func(name string) bool { return name == "extract.json" || name == "identity.sha256" }},
	Seal:   artifactdir.ManifestHash("", "extract.json", "identity.sha256"),
	Errors: outputErrors,
}

// Publish writes the extract only when nothing blocks it and a fresh
// preparation yields exactly the approved bytes. It transmits nothing.
func (c *ExtractCandidate) Publish(ctx context.Context, approval, output string) error {
	if len(c.Blocked) > 0 {
		return errors.New("the extract is blocked: " + strings.Join(c.Blocked, "; "))
	}
	fresh, err := PrepareExtract(ctx, c.packet, c.policy)
	if err != nil {
		return err
	}
	if approval != c.Identity() || fresh.Identity() != c.Identity() || len(fresh.Blocked) > 0 {
		return errors.New("the extract requires approval of its exact current identity; changed evidence or policy requires review again")
	}
	protected := []os.FileInfo{}
	for _, path := range []string{c.packet, c.policy} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		protected = append(protected, info)
	}
	path, err := artifactpath.Destination(output, protected...)
	if err != nil {
		return err
	}
	w, err := artifactdir.Create(path, extractFamily, artifactdir.Durable)
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.WriteFile("extract.json", fresh.raw); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = w.Seal(nil)
	return err
}

// OpenExtract verifies a published extract: its seal and its exact canonical,
// value-free document. It cannot be re-derived without the packet, which
// stays customer-local.
func OpenExtract(dir string) (Extract, error) {
	var x Extract
	invalid := errors.New("invalid, incomplete or changed connected extract")
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return x, err
	}
	files, err := artifactdir.Read(dir, artifactdir.Layout{AllowFile: extractFamily.Layout.AllowFile, MaxFiles: 2, MaxFileBytes: extractMaxBytes, MaxBytes: extractMaxBytes + 128})
	if err != nil {
		return x, invalid
	}
	raw := files["extract.json"]
	if string(files["identity.sha256"]) != digest(raw)+"\n" || json.Unmarshal(raw, &x, json.RejectUnknownMembers(true)) != nil || x.Schema != ExtractSchema || x.EvidenceClass != EvidenceExtract || x.Equivalence.State != EquivalenceUnverified || x.Scope != extractScope || x.Residual.Status != "passed" {
		return Extract{}, invalid
	}
	for _, item := range x.Inventory {
		if item.Surface == SurfaceCredential || item.Disposition != "exclude" {
			return Extract{}, invalid
		}
	}
	canonical, err := encode(x)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Extract{}, invalid
	}
	return x, nil
}
