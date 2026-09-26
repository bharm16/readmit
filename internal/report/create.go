package report

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/fixturetrial"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testrunner"
)

func generatorInputs() bundle.GeneratorInputs {
	return bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: synth.GeneratorVersion, ProfileVersion: synth.ProfileVersion}
}

func target(address string) replay.Target {
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ApprovedTransport: false, ConnectTimeout: "3s", MessageTimeout: "5s", MaxACKBytes: 4096}
}

// Create owns generation and both fresh fixture executions. No supplied source,
// target, observation, or provenance label can opt arbitrary data into this mode.
func Create(ctx context.Context, scenario, output string) (*Packet, error) {
	if scenario != Scenario {
		return nil, errors.New("report supports only --scenario siu-reschedule-v1; customer-derived packets are unsupported")
	}
	spec, err := testrunner.DecodeSpec(scenarioSpec)
	if err != nil {
		return nil, errors.New("invalid embedded report scenario")
	}
	packet, err := artifactdir.Create(output, packetFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer packet.Close()
	dir := packet.Path()
	// Everything the execution workspace holds is scratch: the generated
	// family, each trial's inputs, its fixture's case and ledger and the result
	// its sender records. The workspace is removed before Create answers and
	// the packet keeps its copies through its own synced writes, so nothing
	// written there is flushed.
	work, err := os.MkdirTemp("", "readmit-report-")
	if err != nil {
		return nil, errors.New("cannot create report execution workspace")
	}
	defer os.RemoveAll(work)
	family := filepath.Join(work, "family")
	if _, err := synth.WriteWithDurability(family, generatorInputs(), artifactdir.Scratch); err != nil {
		return nil, err
	}
	caseFiles, err := readTree(filepath.Join(family, "regression"))
	if err != nil {
		return nil, err
	}
	if err := copyFiles(packet, caseFiles, "", "reproducer"); err != nil {
		return nil, err
	}
	if err := packet.WriteFile("spec.json", scenarioSpec); err != nil {
		return nil, err
	}
	for _, trial := range []struct {
		name string
		mode observation.Mode
	}{{"baseline", observation.Defective}, {"post-fix", observation.Fixed}} {
		trialDir := filepath.Join(work, trial.name)
		if err := runTrial(ctx, trialDir, caseFiles, trial.mode, spec); err != nil {
			return nil, err
		}
		files, err := readTree(filepath.Join(trialDir, "result"))
		if err != nil {
			return nil, err
		}
		if err := copyFiles(packet, files, "", trial.name); err != nil {
			return nil, err
		}
	}
	files, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	manifest, additions, err := inspectEvidence(dir, files)
	if err != nil {
		return nil, err
	}
	for name, data := range additions {
		if name == "spec.json" {
			continue
		}
		if directory := path.Dir(name); directory != "." && packet.Mkdir(directory) != nil {
			return nil, errors.New("cannot create report content directory")
		}
		if err := packet.WriteFile(name, data); err != nil {
			return nil, err
		}
		files[name] = data
	}
	manifest.Files = index(files)
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	if err := packet.WriteFile("manifest.json", raw); err != nil {
		return nil, err
	}
	// The completion record binds the manifest, whose complete index binds all
	// content files. These two root metadata files cannot index themselves.
	if _, err := packet.Seal(nil); err != nil {
		return nil, err
	}
	return Open(dir)
}

// runTrial runs one trial in its own scratch workspace at dir: the case and
// spec it sends and the fixture it sends them to.
func runTrial(ctx context.Context, dir string, caseFiles map[string][]byte, mode observation.Mode, spec testrunner.Spec) error {
	workspace, err := artifactdir.Create(dir, trialFamily, artifactdir.Scratch)
	if err != nil {
		return err
	}
	defer workspace.Close()
	if err := copyFiles(workspace, caseFiles, "", "reproducer"); err != nil {
		return err
	}
	if err := workspace.WriteFile("spec.json", scenarioSpec); err != nil {
		return err
	}
	// The fixture's live ledger is read back only by this process, inside the
	// execution workspace Create removes, and the packet keeps its copies in
	// synced writes. So the trial installs it in process: flushing it before
	// each ACK put the disk's latency inside the target's message timeout,
	// which every packet records and Open checks. Its case is scratch too.
	outcome := fixturetrial.Run(ctx, fixturetrial.Trial{
		Mode: mode, Dir: workspace.Path(), Spec: spec,
		CasePath:        filepath.Join(workspace.Path(), "receiver"),
		ObservationPath: filepath.Join(workspace.Path(), "observation.json"),
		Durability:      artifactdir.Scratch,
		Budget:          fixtureBudget,
		Configure: func(session fixturetrial.Session) error {
			config, err := encode(target(session.Address()))
			if err != nil {
				return err
			}
			return workspace.WriteFile("target.json", config)
		},
	})
	if outcome.ConfigErr != nil {
		return outcome.ConfigErr
	}
	if outcome.SenderErr != nil || outcome.ServeErr != nil || outcome.Artifact == nil || outcome.Artifact.Result.Status == testrunner.ExecutionError {
		return errors.New("report fixture execution failed; incomplete packet retained")
	}
	return nil
}

// fixtureBudget bounds one trial. The fixture receiver waits as long for this
// package's own sender, which syncs the run evidence it has just recorded
// before sending the next message: a shorter idle limit let that sender's
// storage, rather than the fixture, end the trial.
const fixtureBudget = 20 * time.Second
