package desktop

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

// HL7ReferenceSelection is explicit inspection context, never a modification of
// the stored evaluator or a historical run pin. Every chosen byte identity is
// checked again on later reads.
type HL7ReferenceSelection struct {
	Profile               string `json:"profile,omitzero"`
	Pack                  string `json:"pack,omitzero"`
	Documentation         string `json:"documentation,omitzero"`
	ProfileIdentity       string `json:"profile_identity,omitzero"`
	PackIdentity          string `json:"pack_identity,omitzero"`
	DocumentationIdentity string `json:"documentation_identity,omitzero"`
}
type HL7LocalDocumentation struct {
	Identity string `json:"identity"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}
type HL7ReferenceOverlay struct {
	Applicability      string                    `json:"applicability,omitzero"`
	Status             string                    `json:"status"`
	Reason             string                    `json:"reason"`
	Selection          HL7ReferenceSelection     `json:"selection"`
	Profile            *profileeval.Pin          `json:"profile,omitzero"`
	Pack               *profileeval.Pin          `json:"pack,omitzero"`
	Base               *localprofile.Base        `json:"base,omitzero"`
	Field              *localprofile.Field       `json:"field,omitzero"`
	SegmentCardinality *localprofile.Cardinality `json:"segment_cardinality,omitzero"`
	SegmentDescription string                    `json:"segment_description,omitzero"`
	Documentation      *HL7LocalDocumentation    `json:"documentation,omitzero"`
}
type HL7ReferenceSelectionResult struct {
	State   State                `json:"state"`
	Reason  string               `json:"reason,omitzero"`
	Overlay *HL7ReferenceOverlay `json:"overlay,omitzero"`
}

func (r *HL7ReferenceSelectionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

func referenceDigest(raw []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) }
func selectionBytes(path, expect string, limit int) ([]byte, string, string) {
	if !filepath.IsAbs(path) {
		return nil, "", "Choose an absolute local reference path."
	}
	raw, err := (artifactdir.Document{MaxBytes: limit}).Read(path)
	if err != nil {
		return nil, "", "The selected reference is unavailable, linked, or exceeds its size bound."
	}
	identity := referenceDigest(raw)
	if expect != "" && expect != identity {
		return nil, "", "The selected reference changed; select its new identity explicitly."
	}
	return raw, identity, ""
}
func readReferenceOverlay(selection HL7ReferenceSelection, node hl7.Node, version, family string) *HL7ReferenceOverlay {
	overlay := &HL7ReferenceOverlay{Status: "not_selected", Selection: selection}
	fail := func(reason string) *HL7ReferenceOverlay {
		overlay.Status = "not_available"
		overlay.Reason = reason
		overlay.Field = nil
		overlay.SegmentCardinality = nil
		return overlay
	}
	var profile *profileeval.ProfileV2
	if selection.Profile != "" {
		raw, identity, reason := selectionBytes(selection.Profile, selection.ProfileIdentity, localprofile.MaxProfileBytes)
		if reason != "" {
			return fail(reason)
		}
		decoded, err := profileeval.DecodeProfile(raw)
		if err != nil {
			return fail("The selected local profile is unsupported or invalid.")
		}
		profile = &decoded
		overlay.Selection.ProfileIdentity = identity
		overlay.Profile = &profileeval.Pin{Schema: decoded.Schema, ID: decoded.Definition.Identity.ID, Version: decoded.Definition.Identity.Version, SHA256: identity[len("sha256:"):]}
		base := decoded.Definition.Base
		overlay.Base = &base
		overlay.Status = "profile_selected"
		overlay.Applicability = "unspecified"
		sourceContext := node.Kind != ""
		if family != "" && base.Family != family {
			overlay.Applicability = "incompatible"
			return fail("This profile's message family does not match the selected message.")
		}
		if version != "" && base.HL7Version != version {
			overlay.Applicability = "incompatible"
			return fail("This profile's edition does not match the selected message.")
		}
		applicable := !sourceContext || version != "" && family != ""
		if sourceContext {
			overlay.Applicability = "matched"
			if version == "" {
				overlay.Applicability = "unknown_edition"
			} else if family == "" {
				overlay.Applicability = "unknown_family"
			}
		}
		for _, segment := range decoded.Definition.Segments {
			if applicable && segment.ID == node.Segment {
				overlay.SegmentDescription = segment.Description
				overlay.SegmentCardinality = segment.Cardinality
				for _, field := range segment.Fields {
					if field.Position == node.Field {
						copy := field
						overlay.Field = &copy
					}
				}
			}
		}
	}
	if selection.Pack != "" {
		raw, identity, reason := selectionBytes(selection.Pack, selection.PackIdentity, 4<<20)
		if reason != "" {
			return fail(reason)
		}
		decoded, err := profileeval.DecodePack(raw)
		if err != nil {
			return fail("The selected profile pack is unsupported or invalid.")
		}
		overlay.Selection.PackIdentity = identity
		overlay.Pack = &profileeval.Pin{Schema: decoded.Schema, ID: decoded.Metadata.Identity.ID, Version: decoded.Metadata.Identity.Version, SHA256: identity[len("sha256:"):]}
		if profile != nil && decoded.Metadata.Identity != profile.Definition.Base.Pack {
			return fail("The selected pack does not match the local profile's exact declared base pin.")
		}
		if profile != nil {
			overlay.Status = "profile_and_pack_verified"
		}
	} else if profile != nil {
		overlay.Reason = "Authored local constraints are shown; the exact base pack has not been selected or verified. No evaluation pin changes."
	}
	if overlay.Applicability == "unknown_edition" || overlay.Applicability == "unknown_family" {
		overlay.Reason = "The original message's edition or family is unknown. Selected profile/pack identities are readable, but field applicability is not inferred."
	}
	if selection.Documentation != "" {
		raw, identity, reason := selectionBytes(selection.Documentation, selection.DocumentationIdentity, 64<<10)
		if reason != "" {
			return fail(reason)
		}
		if !utf8.Valid(raw) {
			return fail("Local reference documentation must be UTF-8 plain text.")
		}
		overlay.Selection.DocumentationIdentity = identity
		overlay.Documentation = &HL7LocalDocumentation{Identity: identity, Title: filepath.Base(selection.Documentation), Text: string(raw)}
		if profile == nil && selection.Pack == "" {
			overlay.Status = "documentation_selected"
		}
	}
	return overlay
}
func (a *App) ReadHL7ReferenceSelection(selection HL7ReferenceSelection) HL7ReferenceSelectionResult {
	return runRead(a, false, func(context.Context) HL7ReferenceSelectionResult {
		overlay := readReferenceOverlay(selection, hl7.Node{}, "", "")
		if overlay.Status == "not_available" {
			return HL7ReferenceSelectionResult{State: Failed, Reason: overlay.Reason, Overlay: overlay}
		}
		return HL7ReferenceSelectionResult{State: Completed, Overlay: overlay}
	})
}
