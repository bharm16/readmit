package report

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"html"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
)

// A reviewed transformed extract of a connected packet carries transformed
// evidence, not only outcomes: FHIR resources and requests, typed records and
// the runtime identity mapping, each element or column kept or pseudonymized
// only where an explicit rule of the reviewed policy says so and excluded
// otherwise. readmit-connected-disclosure-policy/v1 and the value-free
// readmit-connected-extract/v1 are unchanged; each version's reader refuses
// the other.
const (
	TransformPolicySchema    = "readmit-connected-disclosure-policy/v2"
	TransformedExtractSchema = "readmit-connected-extract/v2"
	// EvidenceTransformed is the class of a reviewed transformed extract: a
	// derived representation approved for one disclosure, never an original.
	EvidenceTransformed    = "reviewed-transformed-extract"
	transformedMaxBytes    = 16 << 20
	transformPolicyMaxSize = 256 << 10
)

// transformable are the surfaces a v2 policy may transform. Every other
// surface is opaque in this version (narrative, extensions, attachments,
// non-FHIR bodies, v2 messages, validator diagnostics, setup resources,
// authored definitions and records) and may only be excluded.
var transformable = []string{SurfaceFHIRResource, SurfaceTypedDataset, SurfaceMapping, SurfaceHTTPExchange}

// TransformPolicy is a reviewed field-level transformation. Surfaces maps every
// surface the packet holds to "exclude" or, for a transformable surface,
// "transform". Elements, Columns and Parameters are the only values that
// leave the packet, each kept or pseudonymized as its rule says. Names says
// whether authored identifiers (phase, check, step, dataset, column and
// variable IDs) are shown ("authored") or only positions ("positions").
type TransformPolicy struct {
	Schema     string            `json:"schema"`
	Surfaces   map[string]string `json:"surfaces"`
	Names      string            `json:"names"`
	Elements   []ElementRule     `json:"elements"`
	Columns    []ColumnRule      `json:"columns"`
	Parameters []ParameterRule   `json:"parameters"`
}

// ElementRule selects a FHIR element by resource type ("*" for any) and its
// dotted path from the resource root without indexes, such as
// "identifier.value" or "participant.status".
type ElementRule struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	Action   string `json:"action"`
}

// ColumnRule selects a typed dataset column by dataset and column ID.
type ColumnRule struct {
	Dataset string `json:"dataset"`
	Column  string `json:"column"`
	Action  string `json:"action"`
}

// ParameterRule selects a FHIR request query parameter by name.
type ParameterRule struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

var (
	ruleName     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_\-]{0,63}$`)
	elementPath  = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,63}(\.[a-z][A-Za-z0-9]{0,63}){0,15}$`)
	resourceName = regexp.MustCompile(`^(\*|[A-Z][A-Za-z]{1,63})$`)
	// relativeReference is a FHIR literal reference to a server-assigned
	// logical ID, optionally with a version.
	relativeReference = regexp.MustCompile(`^([A-Z][A-Za-z]{1,63})/([A-Za-z0-9\-.]{1,64})(/_history/[A-Za-z0-9\-.]{1,64})?$`)
)

// DecodeTransformPolicy reads a v2 policy strictly. Transforming an opaque
// surface, admitting credential material, an unknown action or a duplicate
// rule refuses it.
func DecodeTransformPolicy(raw []byte) (TransformPolicy, error) {
	var p TransformPolicy
	refused := errors.New("the disclosure policy is not a readmit-connected-disclosure-policy/v2 this release reads")
	if len(raw) > transformPolicyMaxSize || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != TransformPolicySchema || p.Surfaces == nil || p.Names != "authored" && p.Names != "positions" || len(p.Elements) > 1024 || len(p.Columns) > 1024 || len(p.Parameters) > 256 {
		return p, refused
	}
	for surface, disposition := range p.Surfaces {
		if !slices.Contains(DisclosureSurfaces, surface) || disposition != "exclude" && disposition != "transform" || disposition == "transform" && !slices.Contains(transformable, surface) {
			return p, refused
		}
	}
	seen := map[string]bool{}
	for _, r := range p.Elements {
		k := "e|" + r.Resource + "|" + r.Path
		if !resourceName.MatchString(r.Resource) || !elementPath.MatchString(r.Path) || r.Action != "keep" && r.Action != "pseudonymize" || seen[k] || r.Path == "id" || strings.HasSuffix(r.Path, ".reference") || r.Path == "reference" {
			return p, refused
		}
		seen[k] = true
	}
	for _, r := range p.Columns {
		k := "c|" + r.Dataset + "|" + r.Column
		if !ruleName.MatchString(r.Dataset) || !ruleName.MatchString(r.Column) || !slices.Contains([]string{"keep", "pseudonymize", "redact"}, r.Action) || seen[k] {
			return p, refused
		}
		seen[k] = true
	}
	for _, r := range p.Parameters {
		k := "p|" + r.Name
		if !ruleName.MatchString(strings.TrimPrefix(r.Name, "_")) || r.Action != "keep" && r.Action != "pseudonymize" || seen[k] {
			return p, refused
		}
		seen[k] = true
	}
	return p, nil
}

// DisclosurePolicyVersion reads which policy version a file holds, so the
// caller can choose the value-free or the transformed extract.
func DisclosurePolicyVersion(path string) (string, error) {
	raw, err := disclosurePolicyFileV2.Read(path)
	if err != nil {
		return "", err
	}
	var head struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Schema != DisclosurePolicySchema && head.Schema != TransformPolicySchema {
		return "", errors.New("the disclosure policy is not a readmit-connected-disclosure-policy this release reads")
	}
	return head.Schema, nil
}

var disclosurePolicyFileV2 = artifactdir.Document{
	MaxBytes: transformPolicyMaxSize,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("the disclosure policy must be a bounded regular file"),
		Read:      errors.New("the disclosure policy must be a bounded regular file"),
	},
}

// PseudonymKey is the customer-local secret every pseudonym of an extract is
// keyed with. It is never exported, never written into an extract and never
// derivable from one; without it a pseudonym cannot be recomputed from a
// guessed value. The same key yields the same pseudonyms, which lets a
// previewed extract be approved and later published byte for byte.
type PseudonymKey struct{ key []byte }

func (PseudonymKey) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("pseudonym key (private)")) }

// WritePseudonymKey creates a new private key file: 32 random bytes as 64
// lowercase hex characters, readable by the owner only.
func WritePseudonymKey(path string) error {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return errors.New("cannot generate a pseudonym key")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create the pseudonym key; the file must be new")
	}
	_, err = f.WriteString(hex.EncodeToString(key) + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return errors.New("cannot write the pseudonym key")
	}
	return nil
}

// ReadPseudonymKey reads a key file: a regular file readable by its owner
// only, holding exactly 64 lowercase hex characters.
func ReadPseudonymKey(path string) (PseudonymKey, error) {
	refused := errors.New("the pseudonym key must be a private regular file of 64 hex characters; create one with readmit report connected pseudonym-key")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128 || info.Mode().Perm()&0o077 != 0 {
		return PseudonymKey{}, refused
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return PseudonymKey{}, refused
	}
	text := strings.TrimSuffix(string(raw), "\n")
	key, err := hex.DecodeString(text)
	if err != nil || len(key) != 32 || text != strings.ToLower(text) {
		return PseudonymKey{}, refused
	}
	return PseudonymKey{key: key}, nil
}

// TransformedExtract is the reviewed transformed document. Every transformed
// item states that it is derived and names its original by position only.
type TransformedExtract struct {
	Schema         string               `json:"schema"`
	PacketIdentity string               `json:"packet_identity"`
	PolicyIdentity string               `json:"policy_identity"`
	EvidenceClass  string               `json:"evidence_class"`
	Derivation     string               `json:"derivation"`
	Equivalence    ConnectedEquivalence `json:"equivalence"`
	Runs           []ExtractRun         `json:"runs"`
	Comparison     *ExtractComparison   `json:"comparison"`
	Evidence       []TransformedRun     `json:"evidence"`
	Inventory      []InventoryItem      `json:"inventory"`
	Residual       exportreview.Scan    `json:"residual"`
	Scope          string               `json:"scope"`
}

type TransformedRun struct {
	Section string             `json:"section"`
	Phases  []TransformedPhase `json:"phases"`
}

type TransformedPhase struct {
	Position     int                      `json:"position"`
	ID           string                   `json:"id"`
	Exchanges    []TransformedExchange    `json:"exchanges"`
	Observations []TransformedObservation `json:"observations"`
	Bindings     []TransformedBinding     `json:"bindings"`
}

// TransformedExchange is one FHIR request and its response. Method, path and
// status are present only when the HTTP exchange surface is transformed; the
// bodies only when FHIR resources are.
type TransformedExchange struct {
	Position   int      `json:"position"`
	Step       string   `json:"step"`
	Attempt    int      `json:"attempt"`
	Derivation string   `json:"derivation"`
	Original   string   `json:"original"`
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Status     int      `json:"status"`
	Outcome    string   `json:"outcome"`
	Request    any      `json:"request"`
	Response   any      `json:"response"`
	Excluded   []string `json:"excluded"`
}

type TransformedObservation struct {
	Position   int                 `json:"position"`
	Dataset    string              `json:"dataset"`
	Derivation string              `json:"derivation"`
	Original   string              `json:"original"`
	Boundary   string              `json:"boundary"`
	Usable     bool                `json:"usable"`
	Columns    []TransformedColumn `json:"columns"`
	Records    [][]ReportField     `json:"records"`
	Resources  []any               `json:"resources"`
	Excluded   []string            `json:"excluded"`
}

type TransformedColumn struct {
	Position int    `json:"position"`
	Name     string `json:"name"`
	Action   string `json:"action"`
}

type TransformedBinding struct {
	Position   int    `json:"position"`
	Variable   string `json:"variable"`
	Derivation string `json:"derivation"`
	Value      string `json:"value"`
}

const (
	transformedDerivation = "Derived by the reviewed policy from customer-local original evidence: every value shown was kept or pseudonymized by an explicit rule and everything else was excluded. It is not an original observation, response or finding; the originals stay customer-local in the packet named by packet_identity."
	transformedScope      = "Reviewed transformed extract of customer-local evidence. Pseudonyms are keyed by a customer-local secret that is not part of this extract; equal original values have equal pseudonyms, so references and identifiers stay consistent. The residual scan checks every original value the policy did not keep; it is one check of known values, not an identification assessment or a legal de-identification determination. It is not an equivalent reproducer: no external replay of this extract exists, and passing disclosure review establishes no behavioral equivalence."
	derivedItem           = "transformed"
)

// TransformedCandidate is a prepared transformed extract with its renderings.
// Publish regenerates it and writes only the exact approved bytes.
type TransformedCandidate struct {
	packet, policy string
	key            PseudonymKey
	raw            []byte
	renders        map[string][]byte
	Extract        TransformedExtract
	Blocked        []string
}

func (c *TransformedCandidate) Identity() string { return digest(c.raw) }

// Render returns one derived rendering: json, markdown or html.
func (c *TransformedCandidate) Render(format string) ([]byte, error) {
	name, ok := map[string]string{"json": "extract.json", "markdown": "report.md", "html": "report.html"}[format]
	if !ok {
		return nil, errors.New("unsupported extract format")
	}
	if name == "extract.json" {
		return bytes.Clone(c.raw), nil
	}
	return bytes.Clone(c.renders[name]), nil
}

// transformer applies one policy with one key and records every original
// value it read and every one it emitted unchanged, for the residual scan.
type transformer struct {
	policy    TransformPolicy
	key       []byte
	bases     []string
	elements  map[string]string
	columns   map[string]string
	params    map[string]string
	originals map[string]bool
	kept      map[string]bool
}

func (t *transformer) surface(s string) bool { return t.policy.Surfaces[s] == "transform" }

// pseudonym is the keyed, deterministic replacement of one value.
func (t *transformer) pseudonym(v string) string {
	mac := hmac.New(sha256.New, t.key)
	mac.Write([]byte("readmit-pseudonym/v1\x00" + v))
	return "p-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(mac.Sum(nil)[:15]))
}

func (t *transformer) read(v string) {
	if len(v) >= 4 && strings.IndexFunc(v, unicode.IsLetter) >= 0 && !hexDigest(v) {
		t.originals[v] = true
	}
}

func (t *transformer) keep(v string) string { t.kept[v] = true; return v }

// reference pseudonymizes the logical ID of a relative FHIR reference, so
// every retained reference to a resource names its pseudonymized ID. An
// absolute reference to a declared server is made relative first; any other
// reference is not transformable and is excluded.
func (t *transformer) reference(v string) (string, bool) {
	t.read(v)
	for _, base := range t.bases {
		if rest, ok := strings.CutPrefix(v, strings.TrimSuffix(base, "/")+"/"); ok {
			v = rest
			break
		}
	}
	m := relativeReference.FindStringSubmatch(v)
	if m == nil || !fhirr4.KnownResourceType(m[1]) {
		return "", false
	}
	t.read(m[2])
	return t.keep(m[1]) + "/" + t.pseudonym(m[2]), true
}

// value applies a pseudonymize action to a typed record value: a relative
// reference to a FHIR resource keeps its type, anything else is replaced
// whole. Element values and bound IDs are always replaced whole.
func (t *transformer) value(v string) string {
	if r, ok := t.reference(v); ok {
		return r
	}
	return t.pseudonym(v)
}

// resource transforms one FHIR resource. A resource carrying a modifier
// extension cannot be shown without it and still mean the same; it and Binary
// content are excluded whole.
func (t *transformer) resource(res map[string]any, excluded *[]string) map[string]any {
	typ, _ := res["resourceType"].(string)
	t.read(typ)
	if typ == "" || !resourceName.MatchString(typ) || typ == "*" {
		*excluded = append(*excluded, "a document that is not a FHIR resource")
		return nil
	}
	if typ == "Binary" {
		t.readAll(res)
		*excluded = append(*excluded, "Binary content (opaque)")
		return nil
	}
	if typ == "Bundle" {
		return t.bundle(res, excluded)
	}
	if hasMember(res, "modifierExtension") {
		t.readAll(res)
		*excluded = append(*excluded, typ+" carrying a modifier extension (cannot be shown without it)")
		return nil
	}
	out := map[string]any{"resourceType": t.keep(typ)}
	if id, ok := res["id"].(string); ok {
		t.read(id)
		out["id"] = t.pseudonym(id)
	}
	t.walk(typ, "", res, out, excluded)
	return out
}

// bundle keeps a bundle's type and entries' transformed resources; links and
// full URLs carry server addresses and are excluded.
func (t *transformer) bundle(res map[string]any, excluded *[]string) map[string]any {
	out := map[string]any{"resourceType": t.keep("Bundle"), "entry": []any{}}
	if kind, ok := res["type"].(string); ok {
		out["type"] = t.keep(kind)
	}
	if total, ok := res["total"].(float64); ok {
		out["total"] = total
	}
	entries, _ := res["entry"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if url, ok := entry["fullUrl"].(string); ok {
			t.reference(url)
		}
		if r, ok := entry["resource"].(map[string]any); ok {
			if transformed := t.resource(r, excluded); transformed != nil {
				out["entry"] = append(out["entry"].([]any), map[string]any{"resource": transformed})
			}
		}
	}
	return out
}

func hasMember(v any, name string) bool {
	switch v := v.(type) {
	case map[string]any:
		if _, ok := v[name]; ok {
			return true
		}
		for _, child := range v {
			if hasMember(child, name) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasMember(child, name) {
				return true
			}
		}
	}
	return false
}

// walk copies into out only what a rule keeps or pseudonymizes. Narrative,
// extensions and attachments are opaque here and never copied; references
// are always pseudonymized consistently; contained resources are transformed
// as resources.
func (t *transformer) walk(typ, prefix string, in, out map[string]any, excluded *[]string) {
	for _, k := range slices.Sorted(maps.Keys(in)) {
		v := in[k]
		if prefix == "" && (k == "resourceType" || k == "id") {
			continue
		}
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch {
		case k == "extension":
			t.readAll(v)
			continue
		case prefix == "" && k == "text":
			if narrative, ok := v.(map[string]any); ok && narrative["div"] != nil {
				t.readAll(v)
				continue
			}
		case k == "contained":
			items, _ := v.([]any)
			kept := []any{}
			for _, item := range items {
				if r, ok := item.(map[string]any); ok {
					if transformed := t.resource(r, excluded); transformed != nil {
						kept = append(kept, transformed)
					}
				}
			}
			if len(kept) > 0 {
				out[k] = kept
			}
			continue
		}
		if copied, ok := t.element(typ, path, k, v, excluded); ok {
			out[k] = copied
		}
	}
}

func (t *transformer) element(typ, path, name string, v any, excluded *[]string) (any, bool) {
	switch v := v.(type) {
	case map[string]any:
		if _, typed := v["contentType"]; typed && (v["data"] != nil || v["url"] != nil) {
			t.readAll(v)
			return nil, false
		}
		child := map[string]any{}
		t.walk(typ, path, v, child, excluded)
		return child, len(child) > 0
	case []any:
		items := []any{}
		for _, item := range v {
			if copied, ok := t.element(typ, path, name, item, excluded); ok {
				items = append(items, copied)
			}
		}
		return items, len(items) > 0
	case string:
		t.read(v)
		if name == "reference" {
			return t.reference(v)
		}
		switch t.rule(typ, path) {
		case "keep":
			return t.keep(v), true
		case "pseudonymize":
			return t.pseudonym(v), true
		}
	case float64, bool:
		if t.rule(typ, path) == "keep" {
			return v, true
		}
	}
	return nil, false
}

func (t *transformer) rule(typ, path string) string {
	if a, ok := t.elements[typ+"|"+path]; ok {
		return a
	}
	return t.elements["*|"+path]
}

// readAll records every string below v as an original value.
func (t *transformer) readAll(v any) {
	switch v := v.(type) {
	case map[string]any:
		for _, child := range v {
			t.readAll(child)
		}
	case []any:
		for _, child := range v {
			t.readAll(child)
		}
	case string:
		t.opaque([]byte(v))
	}
}

// document transforms one retained FHIR JSON body; any other body is opaque.
func (t *transformer) document(raw []byte, excluded *[]string) any {
	var doc map[string]any
	if len(raw) == 0 {
		return nil
	}
	if json.Unmarshal(raw, &doc) != nil || doc["resourceType"] == nil {
		t.opaque(raw)
		*excluded = append(*excluded, "a non-FHIR body (opaque)")
		return nil
	}
	transformed := t.resource(doc, excluded)
	if transformed == nil {
		return nil
	}
	return transformed
}

// opaque records an opaque body, and every identifier-like token in it (six
// or more characters with a letter and a digit), as original values, so none
// of them can reach the extract through another path unnoticed. An opaque
// body itself is never copied.
func (t *transformer) opaque(raw []byte) {
	if len(raw) <= 64<<10 {
		t.read(string(raw))
	}
	for _, word := range strings.FieldsFunc(string(raw), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.'
	}) {
		if len(word) >= 6 && strings.IndexFunc(word, unicode.IsDigit) >= 0 {
			t.read(word)
		}
	}
}

// path is a request URL relative to its declared server, with every logical ID
// in the path pseudonymized and each query parameter kept, pseudonymized or
// excluded by its rule. A token's system is kept and its code pseudonymized,
// so it matches the pseudonymized identifier values.
func (t *transformer) path(raw string) (string, bool) {
	t.read(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	rel := ""
	for _, base := range t.bases {
		b, err := url.Parse(strings.TrimSuffix(base, "/"))
		if err == nil && b.Scheme == u.Scheme && b.Host == u.Host && (u.Path == b.Path || strings.HasPrefix(u.Path, b.Path+"/")) {
			rel = strings.TrimPrefix(strings.TrimPrefix(u.Path, b.Path), "/")
			break
		}
	}
	if rel == "" {
		return "", false
	}
	segments := strings.Split(rel, "/")
	for i, s := range segments {
		t.read(s)
		switch {
		case i == 0 || strings.HasPrefix(s, "_") || strings.HasPrefix(s, "$"):
			segments[i] = t.keep(s)
		default:
			segments[i] = t.pseudonym(s)
		}
	}
	out := strings.Join(segments, "/")
	query := u.Query()
	names := slices.Sorted(maps.Keys(query))
	parts := []string{}
	for _, name := range names {
		for _, v := range query[name] {
			t.read(v)
			action := t.params[name]
			switch action {
			case "keep":
				parts = append(parts, t.keep(name)+"="+t.keep(v))
			case "pseudonymize":
				system, code, token := strings.Cut(v, "|")
				if token {
					t.read(system)
					t.read(code)
					parts = append(parts, t.keep(name)+"="+t.keep(system)+"|"+t.pseudonym(code))
				} else {
					parts = append(parts, t.keep(name)+"="+t.pseudonym(v))
				}
			default:
				parts = append(parts, t.keep(name)+"=[excluded]")
			}
		}
	}
	if len(parts) > 0 {
		out += "?" + strings.Join(parts, "&")
	}
	return out, true
}

// PrepareTransformedExtract verifies the packet, inventories every surface,
// applies the reviewed v2 policy with the customer-local key and builds the
// transformed extract and its renderings, all from derived material only.
// Blocked lists every reason it cannot be published.
func PrepareTransformedExtract(ctx context.Context, packetPath, policyPath string, key PseudonymKey) (*TransformedCandidate, error) {
	if len(key.key) != 32 {
		return nil, errors.New("select the customer-local pseudonym key")
	}
	raw, err := disclosurePolicyFileV2.Read(policyPath)
	if err != nil {
		return nil, err
	}
	policy, err := DecodeTransformPolicy(raw)
	if err != nil {
		return nil, err
	}
	opened, err := openDisclosedPacket(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	c := &TransformedCandidate{packet: packetPath, policy: policyPath, key: key, Blocked: applyDisclosurePolicy(opened.inventory, policy.Surfaces)}
	t := &transformer{policy: policy, key: key.key, elements: map[string]string{}, columns: map[string]string{}, params: map[string]string{}, originals: map[string]bool{}, kept: map[string]bool{}}
	for _, r := range policy.Elements {
		t.elements[r.Resource+"|"+r.Path] = r.Action
	}
	for _, r := range policy.Columns {
		t.columns[r.Dataset+"|"+r.Column] = r.Action
	}
	for _, r := range policy.Parameters {
		t.params[r.Name] = r.Action
	}
	for _, e := range opened.packet.evidence {
		for _, s := range e.Plan.Document().Test.Servers {
			t.bases = append(t.bases, s.Base)
		}
	}
	slices.Sort(t.bases)
	t.bases = slices.Compact(t.bases)
	x := TransformedExtract{Schema: TransformedExtractSchema, PacketIdentity: opened.packet.Identity, PolicyIdentity: digest(raw), EvidenceClass: EvidenceTransformed, Derivation: transformedDerivation,
		Equivalence: ConnectedEquivalence{State: EquivalenceUnverified, Reason: "No external replay of this transformed extract is retained, so it is not described as an equivalent reproducer, and a passing disclosure review establishes no behavioral equivalence. Its disclosure review is unaffected."},
		Runs:        valueFreeRuns(opened.packet), Comparison: valueFreeComparison(opened.packet), Evidence: []TransformedRun{}, Inventory: opened.inventory, Scope: transformedScope}
	for _, section := range connectedSections {
		e, ok := opened.packet.evidence[section]
		if !ok {
			continue
		}
		run, err := t.run(ctx, filepath.Join(opened.dir, section), e)
		if err != nil {
			return nil, err
		}
		run.Section = section
		x.Evidence = append(x.Evidence, run)
	}
	// Every value the packet observed, bound or declared, and every original
	// value read while transforming, is a term unless a rule kept it.
	for _, v := range knownValues(opened.packet) {
		t.originals[string(v)] = true
	}
	terms := [][]byte{}
	for _, v := range slices.Sorted(maps.Keys(t.originals)) {
		if !t.kept[v] {
			terms = append(terms, []byte(v))
		}
	}
	x.Residual = exportreview.Scan{Status: "passed", Locations: []string{}}
	body, err := encode(x)
	if err != nil {
		return nil, err
	}
	renders := renderTransformed(x)
	scanned := map[string][]byte{"extract.json": body, "extract strings": decodedStrings(body)}
	for name, data := range renders {
		scanned[name] = data
	}
	c.Blocked, x.Residual = scanResidual(c.Blocked, scanned, terms, "an original value the policy did not keep appears in the extract or its renderings")
	c.Extract = x
	if c.raw, err = encode(x); err != nil {
		return nil, err
	}
	if len(c.raw) > transformedMaxBytes {
		return nil, errors.New("the transformed extract exceeds 16 MiB; select a smaller packet or fewer rules")
	}
	// The renderings never show the residual scan, so the scanned ones are
	// the published ones.
	c.renders = renders
	return c, nil
}

// decodedStrings lists every string value of a JSON document, decoded, so a
// value the encoder escaped differently is still scanned.
func decodedStrings(body []byte) []byte {
	var tree any
	if json.Unmarshal(body, &tree) != nil {
		return nil
	}
	var out bytes.Buffer
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				out.WriteString(k + "\n")
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case string:
			out.WriteString(v + "\n")
		}
	}
	walk(tree)
	return out.Bytes()
}

func (t *transformer) name(id string) string {
	if t.policy.Names == "authored" {
		return id
	}
	return ""
}

// run transforms one lifecycle's evidence phase by phase.
func (t *transformer) run(ctx context.Context, dir string, e connectedrun.FlowEvidence) (TransformedRun, error) {
	run := TransformedRun{Phases: []TransformedPhase{}}
	for i, phase := range e.Result.Phases {
		pe, ok := e.Phases[phase.ID]
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return run, err
		}
		tp := TransformedPhase{Position: i + 1, ID: t.name(phase.ID), Exchanges: []TransformedExchange{}, Observations: []TransformedObservation{}, Bindings: []TransformedBinding{}}
		base := filepath.Join(dir, filepath.FromSlash(connectedrun.PhaseDir(phase.ID)))
		for j, s := range pe.Steps {
			evidence, err := fhirrest.OpenEvidence(ctx, filepath.Join(dir, filepath.FromSlash(connectedrun.StepDir(phase.ID, s.Step))))
			if err != nil {
				continue
			}
			result := evidence.Result()
			for k, a := range result.Attempts {
				if a.Phase == "capabilities" {
					continue
				}
				x := TransformedExchange{Position: j + 1, Step: t.name(s.Step), Attempt: k + 1, Derivation: derivedItem, Original: fmt.Sprintf("phase %d step %d attempt %d", i+1, j+1, k+1), Outcome: a.Outcome.State, Excluded: []string{}}
				if t.surface(SurfaceHTTPExchange) {
					x.Method, x.Status = a.Request.HTTP.Method, a.Receipt.HTTPStatus
					if p, ok := t.path(a.Request.HTTP.URL); ok {
						x.Path = p
					} else {
						x.Excluded = append(x.Excluded, "a request URL outside the declared servers")
					}
				} else {
					t.read(a.Request.HTTP.URL)
				}
				response, _ := evidence.ResponseBytes(k)
				if t.surface(SurfaceFHIRResource) {
					x.Request = t.document(a.Request.HTTP.Body, &x.Excluded)
					x.Response = t.document(response, &x.Excluded)
				} else {
					t.opaque(a.Request.HTTP.Body)
					t.opaque(response)
				}
				tp.Exchanges = append(tp.Exchanges, x)
			}
		}
		for j, id := range slices.Sorted(maps.Keys(pe.Tables)) {
			tp.Observations = append(tp.Observations, t.observation(ctx, j+1, id, pe, filepath.Join(base, filepath.FromSlash(pe.Observations[id]))))
		}
		for j, name := range slices.Sorted(maps.Keys(pe.Bound)) {
			t.read(pe.Bound[name])
			if t.surface(SurfaceMapping) {
				tp.Bindings = append(tp.Bindings, TransformedBinding{Position: j + 1, Variable: t.name(name), Derivation: derivedItem, Value: t.pseudonym(pe.Bound[name])})
			}
		}
		run.Phases = append(run.Phases, tp)
	}
	return run, nil
}

// observation transforms one retained observation: its typed records column
// by column and, for a FHIR observation, the resources its final sample
// returned.
func (t *transformer) observation(ctx context.Context, position int, id string, pe connectedrun.PhaseEvidence, dir string) TransformedObservation {
	table := pe.Tables[id]
	o := TransformedObservation{Position: position, Dataset: t.name(id), Derivation: derivedItem, Original: "observation " + strconv.Itoa(position), Boundary: pe.Boundaries[id], Usable: table.Usable, Columns: []TransformedColumn{}, Records: [][]ReportField{}, Resources: []any{}, Excluded: []string{}}
	actions := []string{}
	for i, c := range table.Columns {
		action := t.columns[id+"|"+c.Name]
		if action == "" || !t.surface(SurfaceTypedDataset) {
			action = "exclude"
		}
		actions = append(actions, action)
		o.Columns = append(o.Columns, TransformedColumn{Position: i + 1, Name: t.name(c.Name), Action: action})
	}
	for _, row := range table.Rows {
		fields := []ReportField{}
		for i := range table.Columns {
			v := dataset.Value{State: "absent"}
			if i < len(row.Values) {
				v = row.Values[i]
			}
			t.readValue(v)
			switch actions[i] {
			case "exclude":
				continue
			case "redact":
				fields = append(fields, ReportField{Column: t.name(table.Columns[i].Name), State: v.State})
			default:
				fields = append(fields, ReportField{Column: t.name(table.Columns[i].Name), State: v.State, Text: t.field(v, actions[i])})
			}
		}
		if t.surface(SurfaceTypedDataset) {
			o.Records = append(o.Records, fields)
		}
	}
	if evidence, err := fhirrest.OpenEvidence(ctx, filepath.Join(dir, "http")); err == nil {
		for k, a := range evidence.Result().Attempts {
			if a.Phase == "capabilities" {
				continue
			}
			response, _ := evidence.ResponseBytes(k)
			if !t.surface(SurfaceFHIRResource) {
				t.opaque(response)
				continue
			}
			if doc, ok := t.document(response, &o.Excluded).(map[string]any); ok {
				if entries, ok := doc["entry"].([]any); ok && doc["resourceType"] == "Bundle" {
					for _, entry := range entries {
						o.Resources = append(o.Resources, entry.(map[string]any)["resource"])
					}
				} else {
					o.Resources = append(o.Resources, doc)
				}
			}
		}
	}
	return o
}

func (t *transformer) readValue(v dataset.Value) {
	t.read(v.Text)
	t.read(v.CodeSystem)
	for _, item := range v.Items {
		t.readValue(item)
	}
}

// field is one typed value kept or pseudonymized; a code keeps its system
// only when kept.
func (t *transformer) field(v dataset.Value, action string) string {
	if len(v.Items) > 0 {
		items := []string{}
		for _, item := range v.Items {
			items = append(items, item.State+":"+t.field(item, action))
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	if v.State != "present" {
		return ""
	}
	if action == "keep" {
		if v.CodeSystem != "" {
			return t.keep(v.CodeSystem) + "|" + t.keep(v.Text)
		}
		return t.keep(v.Text)
	}
	return t.value(v.Text)
}

var transformedFamily = artifactdir.Family{
	Layout: artifactdir.Layout{AllowFile: func(name string) bool {
		return slices.Contains([]string{"extract.json", "report.md", "report.html", "identity.sha256"}, name)
	}},
	Seal:   artifactdir.ManifestHash("", "extract.json", "identity.sha256"),
	Errors: outputErrors,
}

// Publish writes the transformed extract only when nothing blocks it and a
// fresh preparation with the same key yields exactly the approved bytes.
func (c *TransformedCandidate) Publish(ctx context.Context, approval, output string) error {
	return publishDisclosed(ctx, disclosedPublish{
		packet: c.packet, policy: c.policy, blocked: c.Blocked, identity: c.Identity(),
		changed: "evidence, policy or key", family: transformedFamily, approval: approval, output: output,
		repare: func(ctx context.Context) ([]disclosedFile, []string, string, error) {
			fresh, err := PrepareTransformedExtract(ctx, c.packet, c.policy, c.key)
			if err != nil {
				return nil, nil, "", err
			}
			return []disclosedFile{{name: "extract.json", data: fresh.raw}, {name: "report.md", data: fresh.renders["report.md"]}, {name: "report.html", data: fresh.renders["report.html"]}}, fresh.Blocked, fresh.Identity(), nil
		},
	})
}

// OpenTransformedExtract verifies a published transformed extract: its seal,
// its canonical document, its derived class and unverified equivalence, and
// renderings regenerated from the document alone. It cannot be re-derived
// without the packet and key, which stay customer-local.
func OpenTransformedExtract(dir string) (TransformedExtract, error) {
	var x TransformedExtract
	invalid := errors.New("invalid, incomplete or changed transformed connected extract")
	files, raw, err := verifyDisclosedSeal(dir, artifactdir.Layout{AllowFile: transformedFamily.Layout.AllowFile, RequiredFiles: []string{"extract.json", "report.md", "report.html", "identity.sha256"}, MaxFiles: 4, MaxFileBytes: transformedMaxBytes * 4, MaxBytes: transformedMaxBytes * 8}, invalid)
	if err != nil {
		return x, err
	}
	if json.Unmarshal(raw, &x, json.RejectUnknownMembers(true)) != nil || x.Schema != TransformedExtractSchema || x.EvidenceClass != EvidenceTransformed || x.Derivation != transformedDerivation || x.Equivalence.State != EquivalenceUnverified || x.Scope != transformedScope || x.Residual.Status != "passed" {
		return TransformedExtract{}, invalid
	}
	if !inventoryAdmitted(x.Inventory, func(item InventoryItem) bool {
		return item.Disposition == "exclude" || item.Disposition == "transform" && slices.Contains(transformable, item.Surface)
	}) {
		return TransformedExtract{}, invalid
	}
	canonical, err := encode(x)
	if err != nil || !bytes.Equal(canonical, raw) {
		return TransformedExtract{}, invalid
	}
	for name, data := range renderTransformed(x) {
		if !bytes.Equal(files[name], data) {
			return TransformedExtract{}, invalid
		}
	}
	return x, nil
}

// renderTransformed draws Markdown and HTML from the derived document only;
// no original byte is reachable from here.
func renderTransformed(x TransformedExtract) map[string][]byte {
	b := []block{{heading: 1, text: "Readmit reviewed transformed extract"}}
	p := func(text string) { b = append(b, block{text: text}) }
	p("Packet " + x.PacketIdentity + " - " + x.EvidenceClass + " - policy " + x.PolicyIdentity + ".")
	p(x.Derivation)
	p("Regression equivalence: " + x.Equivalence.State + ". " + x.Equivalence.Reason)
	for _, run := range x.Runs {
		b = append(b, block{heading: 2, text: "Run: " + run.Section})
		p("Result " + run.Verdict + " (" + run.State + "); setup " + run.Setup + ", cleanup " + run.Cleanup + "; boundary " + run.Boundary + ".")
		checks := block{head: []string{"Phase", "Check", "Claim", "Outcome"}}
		for _, phase := range run.Phases {
			for _, c := range phase.Checks {
				checks.rows = append(checks.rows, []string{strconv.Itoa(phase.Position), strconv.Itoa(c.Position), c.Claim, c.Outcome})
			}
		}
		b = append(b, checks)
	}
	for _, run := range x.Evidence {
		for _, phase := range run.Phases {
			b = append(b, block{heading: 2, text: "Transformed evidence: " + run.Section + " phase " + strconv.Itoa(phase.Position) + " " + inert(phase.ID)})
			if len(phase.Exchanges) > 0 {
				ex := block{head: []string{"Step", "Attempt", "Request", "Status", "Outcome", "Request body", "Response body", "Excluded"}}
				for _, e := range phase.Exchanges {
					ex.rows = append(ex.rows, []string{strconv.Itoa(e.Position) + " " + inert(e.Step), strconv.Itoa(e.Attempt), inert(e.Method + " " + e.Path), strconv.Itoa(e.Status), e.Outcome, inert(compactJSON(e.Request)), inert(compactJSON(e.Response)), inert(strings.Join(e.Excluded, "; "))})
				}
				b = append(b, ex)
			}
			for _, o := range phase.Observations {
				p("Observation " + strconv.Itoa(o.Position) + " " + inert(o.Dataset) + " - boundary " + o.Boundary + "; usable " + strconv.FormatBool(o.Usable) + "; " + strconv.Itoa(len(o.Records)) + " records; " + strconv.Itoa(len(o.Resources)) + " resources. " + derivedItem + ".")
				head := []string{"Record"}
				for _, c := range o.Columns {
					if c.Action != "exclude" {
						head = append(head, strconv.Itoa(c.Position)+" "+inert(c.Name)+" ("+c.Action+")")
					}
				}
				table := block{head: head}
				for i, rec := range o.Records {
					row := []string{strconv.Itoa(i + 1)}
					for _, f := range rec {
						cell := f.State
						if f.Text != "" {
							cell = inert(f.Text)
						}
						row = append(row, cell)
					}
					table.rows = append(table.rows, row)
				}
				b = append(b, table)
				for i, r := range o.Resources {
					p("Resource " + strconv.Itoa(i+1) + ": " + inert(compactJSON(r)))
				}
			}
			if len(phase.Bindings) > 0 {
				bindings := block{head: []string{"Response variable", "Pseudonymized server-assigned value"}}
				for _, bd := range phase.Bindings {
					bindings.rows = append(bindings.rows, []string{strconv.Itoa(bd.Position) + " " + inert(bd.Variable), inert(bd.Value)})
				}
				b = append(b, bindings)
			}
		}
	}
	b = append(b, block{heading: 2, text: "Scope"})
	p(x.Scope)
	var md, page strings.Builder
	page.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; sandbox\"><meta name=\"referrer\" content=\"no-referrer\"><title>Readmit reviewed transformed extract</title><style>body{font:14px/20px system-ui,sans-serif;max-width:960px;margin:24px auto;padding:0 16px}table{border-collapse:collapse;margin:8px 0}th,td{border:1px solid #999;padding:2px 6px;text-align:left;vertical-align:top;overflow-wrap:anywhere}</style></head><body>\n")
	for _, bl := range b {
		switch {
		case bl.heading > 0:
			fmt.Fprintf(&md, "%s %s\n\n", strings.Repeat("#", bl.heading), bl.text)
			fmt.Fprintf(&page, "<h%d>%s</h%d>\n", bl.heading, html.EscapeString(bl.text), bl.heading)
		case bl.head != nil:
			md.WriteString("| " + strings.Join(bl.head, " | ") + " |\n|" + strings.Repeat(" --- |", len(bl.head)) + "\n")
			page.WriteString("<table><thead><tr>")
			for _, h := range bl.head {
				page.WriteString("<th scope=\"col\">" + html.EscapeString(h) + "</th>")
			}
			page.WriteString("</tr></thead><tbody>\n")
			for _, row := range bl.rows {
				md.WriteString("| " + strings.Join(row, " | ") + " |\n")
				page.WriteString("<tr>")
				for _, cell := range row {
					page.WriteString("<td>" + html.EscapeString(cell) + "</td>")
				}
				page.WriteString("</tr>\n")
			}
			md.WriteString("\n")
			page.WriteString("</tbody></table>\n")
		default:
			md.WriteString(bl.text + "\n\n")
			page.WriteString("<p>" + html.EscapeString(bl.text) + "</p>\n")
		}
	}
	page.WriteString("</body></html>\n")
	return map[string][]byte{"report.md": []byte(md.String()), "report.html": []byte(page.String())}
}

func compactJSON(v any) string {
	if v == nil {
		return "-"
	}
	raw, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return "-"
	}
	return string(raw)
}
