package connectedtest

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ExecuteLegacy is a deliberately narrow execution adapter: only the existing
// fixture ACK boundary, with exact prepared bytes and target configuration.
// Connected external orchestration belongs to IG06. This function does not
// convert a horizon dataset into claims about downstream processing completion.
func ExecuteLegacy(ctx context.Context, p *Plan, specPath, instance, output string) (*Result, error) {
	if p == nil || !identifier.MatchString(instance) {
		return nil, invalid
	}
	legacy, err := testrunner.Prepare(specPath)
	if err != nil {
		return nil, err
	}
	if err := matchLegacy(p, legacy); err != nil {
		return nil, err
	}
	output, err = legacy.DurableDestination(output)
	if err != nil {
		return nil, err
	}
	writer, err := artifactdir.Create(output, resultFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	// Reserve before effects and retain the complete prepared plan first. No
	// completion marker is written if execution or retaining its result fails.
	for _, dir := range []string{"plan", "plan/dependencies", "plan/inputs"} {
		if err := writer.Mkdir(dir); err != nil {
			return nil, err
		}
	}
	for n, b := range p.Files() {
		if err := writer.WriteFile("plan/"+n, b); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(p.document.Test.Limits.DeadlineMS, p.document.Test.Datasets[0].Completion.HorizonMS))*time.Millisecond)
	defer cancel()
	artifact, err := testrunner.Execute(ctx, legacy, filepath.Join(writer.Path(), "legacy"))
	if err != nil {
		return nil, err
	}
	execution, evidence, err := legacyEvidence(p, artifact, instance, engine.Version())
	if err != nil {
		return nil, err
	}
	// Finish retained evidence even after cancellation of network work. This
	// bounded local evaluation does not resume any external effect.
	if ctx.Err() != nil && execution.State != "complete" {
		execution.State = "cancelled"
	}
	finishCtx := context.WithoutCancel(ctx)
	result, err := Evaluate(finishCtx, p, execution, evidence)
	if err != nil {
		return nil, err
	}
	if err := writer.Mkdir("observations"); err != nil {
		return nil, err
	}
	for n, b := range result.files {
		if len(n) > 5 && n[:5] == "plan/" {
			continue
		}
		if err := writer.WriteFile(n, b); err != nil {
			return nil, err
		}
	}
	if _, err := writer.Seal(nil); err != nil {
		return nil, err
	}
	return OpenResult(finishCtx, writer.Path())
}
func matchLegacy(p *Plan, l *testrunner.Plan) error {
	d := p.document.Test
	if l.Boundary() != testrunner.ACKBoundary || d.Setup.Kind != "operator-declared" || d.Setup.Plan != nil || d.Setup.Cleanup != nil || len(d.Profiles) > 0 || len(d.Environment.Grants) > 0 || len(d.Datasets) != 1 || d.Datasets[0].Source != "legacy-ack" || d.Datasets[0].Completion.Kind != "ack-responses" || d.Datasets[0].ID != d.Bindings.Observed || d.Datasets[0].Kind != "v2-messages" || d.Bindings.Before != "" || d.Bindings.After != "" {
		return errors.New("legacy adapter accepts only an operator-declared ACK fixture")
	}
	if l.Target().Identity() != d.Environment.TargetIdentity || d.Environment.TLS.Mode != "plain" || l.Target().Transport != "plain" {
		return errors.New("legacy target differs from prepared environment")
	}
	actual := l.Environment()
	if actual.Name != d.Environment.Name || string(actual.Classification) != d.Environment.Classification {
		return errors.New("legacy environment differs from prepared environment")
	}
	if d.Environment.AddressPolicyIdentity != Digest([]byte("readmit-legacy-loopback-policy/v1")) {
		return errors.New("legacy adapter requires its exact loopback policy identity")
	}
	pinned := l.PinnedInputs()
	spec, err := testrunner.DecodeSpec(pinned.Spec)
	if err != nil {
		return err
	}
	if spec.Setup.ResetInstructions != d.Setup.Instructions {
		return errors.New("legacy setup declaration differs")
	}
	completion := d.Datasets[0].Completion
	if len(d.Steps) > completion.MaxRecords || len(d.Steps)*(l.Target().MaxACKBytes+3) > completion.MaxBytes {
		return errors.New("legacy adapter exceeds prepared observation capacity")
	}
	if len(pinned.Mappings) != len(d.Steps) {
		return errors.New("legacy stimulus count differs")
	}
	for i, id := range p.document.Order {
		s := d.Steps[slices.IndexFunc(d.Steps, func(s Step) bool { return s.ID == id })]
		if s.V2 == nil || s.V2.Occurrence != pinned.Mappings[i].SourceOccurrence {
			return errors.New("legacy stimulus ordering differs")
		}
		b, err := l.Outbound(pinned.Mappings[i].OutboundOccurrence)
		if err != nil || !bytes.Equal(b, framed(p.files["inputs/"+id+".hl7"])) {
			return errors.New("legacy stimulus bytes differ")
		}
	}
	return nil
}
func legacyEvidence(p *Plan, a *testrunner.Artifact, instance, enginePin string) (Execution, Evidence, error) {
	execution := Execution{Instance: instance, Engine: enginePin, State: "complete", Setup: "operator-declared", Cleanup: "not-requested"}
	evidence := Evidence{Datasets: map[string]Sample{}}
	sample := Sample{Complete: true, Messages: map[string][]byte{}}
	if a.Run == nil {
		execution.State = "incomplete"
		sample.Complete = false
	} else {
		if a.Run.Manifest.Target.Identity() != p.document.Environment.TargetIdentity || a.Result.ObservationBoundary != testrunner.ACKBoundary || len(a.Run.Events) != len(p.document.Order) {
			return execution, evidence, errors.New("legacy evidence does not bind to connected plan")
		}
		sample.SourceIdentity = a.Run.Identity
		for i, event := range a.Run.Events {
			stepID := p.document.Order[i]
			step := p.document.Test.Steps[slices.IndexFunc(p.document.Test.Steps, func(s Step) bool { return s.ID == stepID })]
			intended, err := a.Run.Raw(event.Intended)
			if err != nil || step.V2 == nil || event.SourceOccurrence != step.V2.Occurrence || !bytes.Equal(intended, framed(p.files["inputs/"+stepID+".hl7"])) {
				return execution, evidence, errors.New("retained legacy input differs")
			}
			attempt := Attempt{Step: stepID, Kind: "v2-send", Outcome: "complete"}
			switch event.Outcome {
			case replay.Accepted, replay.ApplicationError, replay.Rejected:
			default:
				attempt.Outcome = "unknown"
				attempt.Uncertain = true
				execution.State = "uncertain"
				sample.Complete = false
			}
			execution.Attempts = append(execution.Attempts, attempt)
			if event.Received.Path != "" {
				b, err := a.Run.Raw(event.Received)
				if err != nil {
					return execution, evidence, err
				}
				doc, parseErr := hl7.Parse(b, hl7.Options{})
				if parseErr == nil && len(doc.Messages) == 1 {
					sample.Messages[event.SourceOccurrence] = b
				} else {
					sample.Complete = false
					execution.State = "incomplete"
				}
			}
		}
	}
	evidence.Datasets[p.document.Test.Bindings.Observed] = sample
	return execution, evidence, nil
}

func framed(b []byte) []byte {
	d, err := hl7.Parse(b, hl7.Options{})
	if err == nil && d.Format == hl7.Raw {
		return mllp.Frame(b)
	}
	return b
}
