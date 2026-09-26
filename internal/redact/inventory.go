package redact

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// DecodeInventory is the same strict reader used by Create and document authoring.
func DecodeInventory(raw []byte) (Inventory, error) {
	var inventory Inventory
	var required struct {
		Artifacts      *[]OriginalArtifact `json:"artifacts"`
		ResidualValues *[]string           `json:"residual_values"`
	}
	if len(raw) > maxConfigBytes || json.Unmarshal(raw, &required) != nil || required.Artifacts == nil || required.ResidualValues == nil || json.Unmarshal(raw, &inventory, json.RejectUnknownMembers(true)) != nil || inventory.Schema != InventorySchema || !inventory.Complete || len(inventory.Artifacts) > 32 || len(inventory.ResidualValues) > 4096 {
		return inventory, errors.New("inventory requires explicit complete scope and supported bounded entries")
	}
	for _, value := range inventory.ResidualValues {
		if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
			return inventory, errors.New("invalid known residual value")
		}
	}
	return inventory, nil
}

// verifyArtifacts is the disk-reading half of the inventory review: it
// resolves and verifies each inventoried artifact against the reviewed case,
// and reads back the in-memory facts derive locates findings about. It
// writes nothing.
func verifyArtifacts(inventory Inventory, base, sourceIdentity string) ([]artifactFact, error) {
	facts := make([]artifactFact, 0, len(inventory.Artifacts))
	for _, artifact := range inventory.Artifacts {
		if artifact.Path == "" {
			return nil, errors.New("inventory artifact requires a path")
		}
		// Resolve filesystem traversal before any lexical path cleaning. Seal
		// the same location that is validated, reviewed, protected and reopened.
		path, err := artifactpath.Resolve(artifactpath.JoinReference(base, artifact.Path))
		if err != nil {
			return nil, err
		}
		identity, err := artifactIdentity(artifact.Kind, path, sourceIdentity)
		if err != nil {
			return nil, err
		}
		fact := artifactFact{reference: sourceReference{Kind: artifact.Kind, Path: path, Identity: identity}}
		switch artifact.Kind {
		case "run":
			if fact.run, err = replay.Open(path); err != nil {
				return nil, err
			}
		case "result":
			if fact.result, err = testrunner.Open(path); err != nil {
				return nil, err
			}
		case "diagnosis-json":
			raw, err := readLocal(path, maxReviewBytes)
			if err != nil {
				return nil, err
			}
			var report diagnose.Report
			if json.Unmarshal(raw, &report, json.RejectUnknownMembers(true)) != nil {
				return nil, errors.New("unsupported diagnosis artifact")
			}
			fact.report = &report
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

// reviewFacts is the pure half of the inventory review: verified facts in,
// located findings and residual terms out. It pairs each fact with its
// private source linkage, so the state derive returns pins every original
// artifact it located findings about.
func (t *transformer) reviewFacts(inventory Inventory, facts []artifactFact) error {
	for _, value := range inventory.ResidualValues {
		t.addTerm([]byte(value))
	}
	for i, fact := range facts {
		t.local.Sources = append(t.local.Sources, fact.reference)
		location := fmt.Sprintf("original-artifacts/a%04d", i+1)
		if err := t.finding(location+"/filename", "other-unique-identifiers", "source-filename", Filenames, t.has(Filenames)); err != nil {
			return err
		}
		switch fact.reference.Kind {
		case "run":
			if err := t.reviewRun(fact.run, location); err != nil {
				return err
			}
		case "result":
			if err := t.finding(location+"/result-spec-and-observations", "other-unique-identifiers", "original-result-excluded-and-rerun", Rerun, t.has(Rerun)); err != nil {
				return err
			}
			if fact.result.Run != nil {
				if err := t.reviewRun(fact.result.Run, location+"/run"); err != nil {
					return err
				}
			}
		case "diagnosis-json":
			for j := range fact.report.Findings {
				if err := t.finding(fmt.Sprintf("%s/findings/%d/text", location, j+1), "other-unique-identifiers", "original-diagnosis-text-excluded-and-regenerated", Diagnosis, t.has(Diagnosis)); err != nil {
					return err
				}
			}
			if err := t.finding(location+"/metadata-and-other-text", "other-unique-identifiers", "original-diagnosis-excluded-and-regenerated", Diagnosis, t.has(Diagnosis)); err != nil {
				return err
			}
		case "diagnosis-markdown":
			if err := t.finding(location+"/text", "other-unique-identifiers", "original-diagnosis-text-excluded-and-regenerated", Diagnosis, t.has(Diagnosis)); err != nil {
				return err
			}
		}
	}
	// The regenerated diagnosis is required even if there was no original report.
	return t.finding("packet/diagnosis", "other-unique-identifiers", "diagnosis-regeneration-required", Diagnosis, t.has(Diagnosis))
}

func (t *transformer) reviewRun(run *replay.Run, location string) error {
	if err := t.finding(location+"/manifest-and-events", "other-unique-identifiers", "original-run-excluded-and-rerun", Rerun, t.has(Rerun)); err != nil {
		return err
	}
	for i, change := range run.Manifest.Changes {
		for _, value := range []struct {
			name string
			raw  []byte
		}{{"old", change.Old}, {"new", change.New}} {
			t.addTerm(value.raw)
			if err := t.finding(fmt.Sprintf("%s/manifest/changes/%d/%s", location, i+1, value.name), "other-unique-identifiers", "original-replay-transformation-excluded", Rerun, t.has(Rerun)); err != nil {
				return err
			}
		}
	}
	for _, event := range run.Events {
		if err := t.finding(location+"/"+event.OutboundOccurrence+"/received-ack-and-err", "other-unique-identifiers", "original-ack-text-excluded-and-rerun", Rerun, t.has(Rerun)); err != nil {
			return err
		}
	}
	return nil
}

func artifactIdentity(kind, path, sourceIdentity string) (string, error) {
	switch kind {
	case "run":
		artifact, err := replay.Open(path)
		if err != nil {
			return "", err
		}
		if artifact.Manifest.SourceBundleIdentity != sourceIdentity {
			return "", errors.New("inventory run belongs to another case")
		}
		return artifact.Identity, nil
	case "result":
		artifact, err := testrunner.Open(path)
		if err != nil {
			return "", err
		}
		if artifact.Result.InputBundleIdentity != sourceIdentity {
			return "", errors.New("inventory result belongs to another case")
		}
		return artifact.Identity, nil
	case "diagnosis-json":
		raw, err := readLocal(path, maxReviewBytes)
		if err != nil {
			return "", err
		}
		var report diagnose.Report
		if json.Unmarshal(raw, &report, json.RejectUnknownMembers(true)) != nil || report.Schema != diagnose.Schema || report.CaseIdentity != sourceIdentity || len(report.Findings) > maxFindings || len(report.Unsupported) > maxFindings {
			return "", errors.New("unsupported or unrelated diagnosis JSON")
		}
		return digest(raw), nil
	case "diagnosis-markdown":
		raw, err := readLocal(path, maxReviewBytes)
		if err != nil || !utf8.Valid(raw) {
			return "", errors.New("diagnosis Markdown must be bounded UTF-8 text")
		}
		return digest(raw), nil
	default:
		return "", errors.New("unsupported inventory artifact kind; no artifact was silently omitted")
	}
}
