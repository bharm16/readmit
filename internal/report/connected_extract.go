package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedrun"
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

// surfacesOf maps one packet file to its disclosure surfaces: the credential
// scan and the FHIR content it carries, over the retained location the
// lifecycle's own classification answers.
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
	switch loc := connectedrun.Locate(rel); loc.Area {
	case connectedrun.AreaRuntimeInputs:
		// Original/derived bytes and transformation manifests may contain the
		// same HL7 values; none may escape the existing transport disclosure rule.
		return append(surfaces, SurfaceHL7Transport)
	case connectedrun.AreaSetup:
		return append(surfaces, SurfaceSetup)
	case connectedrun.AreaPlan, connectedrun.AreaPlanDeps:
		return append(append(surfaces, SurfacePlan), fhir...)
	case connectedrun.AreaPhase:
		switch loc.Sub {
		case connectedrun.PhasePlan:
			return append(append(surfaces, SurfacePlan), fhir...)
		case connectedrun.PhaseValidations:
			return append(append(surfaces, SurfaceValidator), fhir...)
		case connectedrun.PhaseTransport:
			return append(surfaces, SurfaceHL7Transport)
		case connectedrun.PhaseManifest:
			return append(surfaces, SurfaceMapping)
		}
		if loc.Binary && (loc.HTTP || loc.Sub == connectedrun.PhaseSteps || loc.Sub == connectedrun.PhasePreflight) {
			if len(fhir) > 0 || isFHIR(data) {
				return append(append(surfaces, SurfaceFHIRResource), fhir...)
			}
			return append(surfaces, SurfaceHTTPResponse)
		}
		if loc.HTTP || loc.Sub == connectedrun.PhaseSteps || loc.Sub == connectedrun.PhasePreflight {
			return append(surfaces, SurfaceHTTPExchange)
		}
		if loc.Sub == connectedrun.PhaseObservations || loc.Sub == connectedrun.PhaseEvaluationDatasets || loc.Samples ||
			loc.Sub == connectedrun.PhaseIntervals && loc.Dataset {
			return append(surfaces, SurfaceTypedDataset)
		}
		if loc.Sub == connectedrun.PhaseIntervals {
			return append(surfaces, SurfaceCompletion)
		}
		return append(surfaces, SurfaceLifecycle)
	default:
		return append(surfaces, SurfaceLifecycle)
	}
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
	policyRaw      []byte
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
	candidate, err := PrepareExtractPolicy(ctx, packetPath, raw)
	if candidate != nil {
		candidate.policy = policyPath
		candidate.policyRaw = nil
	}
	return candidate, err
}

// PrepareExtractPolicy is the same disclosure owner over an exact in-memory
// policy snapshot. No projection or consent is restored from the window.
func PrepareExtractPolicy(ctx context.Context, packetPath string, raw []byte) (*ExtractCandidate, error) {
	policy, err := DecodeDisclosurePolicy(raw)
	if err != nil {
		return nil, err
	}
	opened, err := openDisclosedPacket(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	c := &ExtractCandidate{packet: packetPath, policyRaw: bytes.Clone(raw), Blocked: applyDisclosurePolicy(opened.inventory, policy.Surfaces)}
	x := Extract{Schema: ExtractSchema, PacketIdentity: opened.packet.Identity, PolicyIdentity: digest(raw), EvidenceClass: EvidenceExtract, Equivalence: ConnectedEquivalence{State: EquivalenceUnverified, Reason: "No external replay of this extract is retained, so it is not described as an equivalent reproducer. Its disclosure review is unaffected."}, Runs: []ExtractRun{}, Inventory: opened.inventory, Scope: extractScope}
	x.Runs = valueFreeRuns(opened.packet)
	x.Comparison = valueFreeComparison(opened.packet)
	// The extract is scanned for every value the packet observed or bound, so
	// a value that reached it through any path blocks it.
	x.Residual = exportreview.Scan{Status: "passed", Locations: []string{}}
	body, err := encode(x)
	if err != nil {
		return nil, err
	}
	c.Blocked, x.Residual = scanResidual(c.Blocked, map[string][]byte{"extract.json": body}, knownValues(opened.packet), "a value the packet retained appears in the extract")
	c.Extract = x
	if c.raw, err = encode(x); err != nil {
		return nil, err
	}
	if len(c.raw) > extractMaxBytes {
		return nil, errors.New("the extract exceeds 4 MiB; select a smaller packet")
	}
	return c, nil
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
	return publishDisclosed(ctx, disclosedPublish{
		packet: c.packet, policy: c.policy, blocked: c.Blocked, identity: c.Identity(),
		changed: "evidence or policy", family: extractFamily, approval: approval, output: output,
		repare: func(ctx context.Context) ([]disclosedFile, []string, string, error) {
			var fresh *ExtractCandidate
			var err error
			if c.policyRaw != nil {
				fresh, err = PrepareExtractPolicy(ctx, c.packet, c.policyRaw)
			} else {
				fresh, err = PrepareExtract(ctx, c.packet, c.policy)
			}
			if err != nil {
				return nil, nil, "", err
			}
			return []disclosedFile{{name: "extract.json", data: fresh.raw}}, fresh.Blocked, fresh.Identity(), nil
		},
	})
}

// OpenExtract verifies a published extract: its seal and its exact canonical,
// value-free document. It cannot be re-derived without the packet, which
// stays customer-local.
func OpenExtract(dir string) (Extract, error) {
	var x Extract
	invalid := errors.New("invalid, incomplete or changed connected extract")
	_, raw, err := verifyDisclosedSeal(dir, artifactdir.Layout{AllowFile: extractFamily.Layout.AllowFile, MaxFiles: 2, MaxFileBytes: extractMaxBytes, MaxBytes: extractMaxBytes + 128}, invalid)
	if err != nil {
		return x, err
	}
	if json.Unmarshal(raw, &x, json.RejectUnknownMembers(true)) != nil || x.Schema != ExtractSchema || x.EvidenceClass != EvidenceExtract || x.Equivalence.State != EquivalenceUnverified || x.Scope != extractScope || x.Residual.Status != "passed" {
		return Extract{}, invalid
	}
	if !inventoryAdmitted(x.Inventory, func(item InventoryItem) bool { return item.Disposition == "exclude" }) {
		return Extract{}, invalid
	}
	canonical, err := encode(x)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Extract{}, invalid
	}
	return x, nil
}

// Bytes returns the exact detached value-free document reviewed for publication.
func (c *ExtractCandidate) Bytes() []byte { return bytes.Clone(c.raw) }
