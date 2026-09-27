package connectedrun

import (
	"bytes"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"path/filepath"
)

const ConfigSchemaV2 = "readmit-connected-run-config/v2"
const SchemaV2 = "readmit-connected-run/v2"

type ConfigV2 struct {
	Schema     string                     `json:"schema"`
	Definition Config                     `json:"definition"`
	Barriers   map[string]SourceSelection `json:"barriers"`
}

func prepareInterval(planPath, configPath string, plan *connectedtest.Plan, raw []byte) (*Prepared, error) {
	return prepareIntervalMode(planPath, configPath, plan, raw, false)
}
func prepareIntervalMode(planPath, configPath string, plan *connectedtest.Plan, raw []byte, sequence bool) (*Prepared, error) {
	var c ConfigV2
	schema := connectedtest.PlanSchemaV3
	if sequence {
		schema = connectedtest.PhasePlanSchema
	}
	if plan.Document().Schema != schema || json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != ConfigSchemaV2 || c.Definition.Schema != ConfigSchema {
		return nil, invalid
	}
	config := c.Definition
	root, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	anchor := func(s string) string {
		if s == "" {
			return ""
		}
		return artifactpath.JoinReference(root, s)
	}
	config.Send.Path = anchor(config.Send.Path)
	selection := connectedtransport.Selection{Case: anchor(config.Case), Target: anchor(config.Target), Policy: anchor(config.Policy), Credential: anchor(config.Credential)}
	var transport *connectedtransport.Prepared
	if sequence {
		transport, err = connectedtransport.PrepareSequence(plan, selection)
	} else {
		transport, err = connectedtransport.Prepare(plan, selection)
	}
	if err != nil {
		return nil, err
	}
	policy, err := (artifactdir.Document{MaxBytes: sendpolicy.MaxPolicyBytes}).Read(anchor(config.Policy))
	if err != nil {
		return nil, err
	}
	p := &Prepared{sequence: sequence, intervals: true, plan: plan, transport: transport, send: config.Send, planPath: planPath, configPath: configPath, configRaw: bytes.Clone(raw)}
	doc := plan.Document()
	files := plan.Files()
	barriers := 0
	if len(config.Sources) != len(doc.Test.Datasets) {
		return nil, invalid
	}
	for _, d := range doc.Test.Datasets {
		selected, ok := config.Sources[d.ID]
		if !ok || d.Projection == nil || d.Completion.Policy == nil {
			return nil, invalid
		}
		projection, err := dataset.DecodeProjection(files["dependencies/"+d.Projection.SHA256])
		if err != nil {
			return nil, err
		}
		definition, err := observeinterval.Decode(files["dependencies/"+d.Completion.Policy.SHA256])
		if err != nil || !definition.Enabled {
			return nil, invalid
		}
		sourceRaw, err := (artifactdir.Document{MaxBytes: 3 << 20}).Read(anchor(selected.Path))
		if err != nil {
			return nil, err
		}
		var head struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(sourceRaw, &head) != nil {
			return nil, invalid
		}
		var item sourcePlan
		if head.Schema == observeinterval.CaptureSourceSchema {
			if d.Phase != "after" || projection.Format != "hl7" || definition.Mode != "stream" || dataset.Digest(sourceRaw) != d.Source || selected.Grant == nil || selected.CredentialGeneration != "" {
				return nil, invalid
			}
			captureSource, err := observeinterval.DecodeCapture(sourceRaw)
			if err != nil || captureSource.MaxMessages > definition.MaxRecords || captureSource.MaxBytes > definition.MaxBytes {
				return nil, invalid
			}
			capture, err := observeinterval.PrepareCapture(sourceRaw, policy, networkaction.Binding{Plan: plan.Identity(), Project: doc.Environment.Project, Environment: doc.Environment.ID, Revision: doc.Environment.Revision, Endpoint: d.ID})
			if err != nil {
				return nil, err
			}
			item = sourcePlan{definition: d, projection: projection, capture: capture, captureRaw: sourceRaw, grant: *selected.Grant}
			item.grant.Path = anchor(item.grant.Path)
		} else {
			if definition.Mode != "snapshots" {
				return nil, invalid
			}
			item, err = prepareSource(plan, d, projection, selected, anchor, policy)
			if err != nil || item.source.Capture != nil {
				return nil, invalid
			}
		}
		item.interval = &definition
		if definition.Barrier != nil {
			if d.Phase != "after" {
				return nil, invalid
			}
			barriers++
			chosen, ok := c.Barriers[d.ID]
			if !ok {
				return nil, invalid
			}
			bd := connectedtest.Dataset{ID: d.ID + "-barrier", Source: definition.Barrier.Source, Namespace: "processing-barrier", Phase: "after", Kind: "typed-rows"}
			prepared, err := prepareSource(plan, bd, definition.Barrier.Projection, chosen, anchor, policy)
			if err != nil || prepared.source.Capture != nil {
				return nil, invalid
			}
			item.barrier = &prepared
		}
		p.sources = append(p.sources, item)
	}
	if barriers != len(c.Barriers) {
		return nil, invalid
	}
	return p, nil
}
