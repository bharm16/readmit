package report

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
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
	if _, err := testrunner.DecodeSpec(scenarioSpec); err != nil {
		return nil, errors.New("invalid embedded report scenario")
	}
	dir, err := reserve(output)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "readmit-report-")
	if err != nil {
		return nil, errors.New("cannot create report execution workspace")
	}
	defer os.RemoveAll(work)
	family := filepath.Join(work, "family")
	if _, err := synth.Write(family, generatorInputs()); err != nil {
		return nil, err
	}
	caseFiles, err := readTree(filepath.Join(family, "regression"))
	if err != nil {
		return nil, err
	}
	if err := copyFiles(caseFiles, "", filepath.Join(dir, "reproducer")); err != nil {
		return nil, err
	}
	if err := writeFile(dir, "spec.json", scenarioSpec); err != nil {
		return nil, err
	}
	for _, trial := range []struct {
		name string
		mode observation.Mode
	}{{"baseline", observation.Defective}, {"post-fix", observation.Fixed}} {
		trialDir := filepath.Join(work, trial.name)
		if os.Mkdir(trialDir, 0700) != nil {
			return nil, errors.New("cannot create report trial workspace")
		}
		if err := copyFiles(caseFiles, "", filepath.Join(trialDir, "reproducer")); err != nil {
			return nil, err
		}
		if err := writeFile(trialDir, "spec.json", scenarioSpec); err != nil {
			return nil, err
		}
		if err := executeFixture(ctx, trialDir, trial.mode); err != nil {
			return nil, err
		}
		files, err := readTree(filepath.Join(trialDir, "result"))
		if err != nil {
			return nil, err
		}
		if err := copyFiles(files, "", filepath.Join(dir, trial.name)); err != nil {
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
		if os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0700) != nil {
			return nil, errors.New("cannot create report content directory")
		}
		if err := writeFile(dir, name, data); err != nil {
			return nil, err
		}
		files[name] = data
	}
	manifest.Files = index(files)
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	if err := writeFile(dir, "manifest.json", raw); err != nil {
		return nil, err
	}
	// The completion record binds the manifest, whose complete index binds all
	// content files. These two root metadata files cannot index themselves.
	if err := writeFile(dir, "identity.sha256", []byte(digest(raw)+"\n")); err != nil {
		return nil, err
	}
	return Open(dir)
}

func executeFixture(ctx context.Context, dir string, mode observation.Mode) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errors.New("cannot start report loopback fixture")
	}
	defer listener.Close()
	config, err := encode(target(listener.Addr().String()))
	if err != nil {
		return err
	}
	if err := writeFile(dir, "target.json", config); err != nil {
		return err
	}
	fixture, err := receiver.New(receiver.Config{Mode: mode, OutputPath: filepath.Join(dir, "receiver"), ObservationPath: filepath.Join(dir, "observation.json"), MaxMessages: 2, MaxFrameBytes: 1 << 20, IdleTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := fixture.Serve(ctx, listener); done <- err }()
	// New has installed the empty observation before Execute can connect.
	artifact, runErr := testrunner.Run(ctx, filepath.Join(dir, "spec.json"), filepath.Join(dir, "result"))
	cancel()
	serveErr := <-done
	if runErr != nil || serveErr != nil || artifact == nil || artifact.Result.Status == testrunner.ExecutionError {
		return errors.New("report fixture execution failed; incomplete packet retained")
	}
	return nil
}
