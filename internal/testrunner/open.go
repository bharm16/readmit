package testrunner

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

// Open is strictly offline and relocation-safe. It reads only files inside dir;
// original spec, target, source case, and live observation paths are never used.
// Verdicts are reevaluated from the retained run and observation evidence.
func Open(dir string) (*Artifact, error) {
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid or inconsistent test result")
	files, err := readDirectory(dir)
	if err != nil {
		return nil, err
	}
	identity := directoryIdentity(files)
	if string(files["identity.sha256"]) != identity+"\n" {
		return nil, errors.New("test result identity mismatch or incomplete result")
	}
	var result Result
	if len(files["result.json"]) > maxResultBytes || json.Unmarshal(files["result.json"], &result, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	if result.Schema != Schema || result.State != "complete" || !result.ContainsSourceValues || result.ExportPolicy != "customer-local-only" || result.Assertions == nil {
		return nil, invalid
	}
	if result.Status != Pass && result.Status != AssertionFailure && result.Status != ExecutionError {
		return nil, invalid
	}
	artifact := &Artifact{Result: result, Identity: identity}
	allowed := map[string]bool{"result.json": true, "identity.sha256": true}
	load := func(ref *bundle.Payload, name string, max int) ([]byte, error) {
		if ref == nil {
			return nil, nil
		}
		data, exists := files[name]
		if !exists || ref.Path != name || len(data) != ref.Size || ref.Size > max || digest(data) != ref.SHA256 {
			return nil, invalid
		}
		allowed[name] = true
		return data, nil
	}
	rawSpec, err := load(result.Spec, "spec.json", MaxSpecBytes)
	if err != nil {
		return nil, err
	}
	if result.Spec == nil {
		if result.SpecIdentity != "" {
			return nil, invalid
		}
	} else {
		if result.SpecIdentity != result.Spec.SHA256 {
			return nil, invalid
		}
		spec, err := DecodeSpec(rawSpec)
		if err == nil {
			artifact.Spec = &spec
		}
	}
	initial, err := load(result.InitialObservation, "initial-observation.json", observation.MaxBytes)
	if err != nil {
		return nil, err
	}
	if initial != nil {
		if snapshot, err := observation.Decode(initial); err == nil {
			artifact.InitialObservation = &snapshot
		}
	}
	final, err := load(result.FinalObservation, "observation.json", observation.MaxBytes)
	if err != nil {
		return nil, err
	}
	if final != nil {
		if snapshot, err := observation.Decode(final); err == nil {
			artifact.FinalObservation = &snapshot
		}
	}
	if result.Run != nil {
		if result.Run.Path != "run" || !validDigest(result.Run.Identity) {
			return nil, invalid
		}
		artifact.Run, err = replay.Open(filepath.Join(dir, "run"))
		if err != nil {
			return nil, errors.New("test result references an invalid run")
		}
		if artifact.Run.Identity != result.Run.Identity || result.Target == nil || !sameJSON(artifact.Run.Manifest.Target, *result.Target) || artifact.Run.Manifest.SourceBundleIdentity != result.InputBundleIdentity {
			return nil, invalid
		}
		if len(artifact.Run.Manifest.Transformations) != 0 || len(artifact.Run.Manifest.Changes) != 0 {
			return nil, errors.New("v1 test result cannot reference transformed replay")
		}
	} else if _, err := os.Lstat(filepath.Join(dir, "run")); !os.IsNotExist(err) {
		return nil, invalid
	}
	for name := range files {
		if !allowed[name] && !(result.Run != nil && strings.HasPrefix(name, "run/")) {
			return nil, errors.New("unexpected test evidence file")
		}
	}
	if result.Target == nil {
		if result.TargetIdentity != "" || result.InputBundleIdentity != "" {
			return nil, invalid
		}
	} else if result.TargetIdentity != result.Target.Identity() || !validDigest(result.InputBundleIdentity) {
		return nil, invalid
	}
	if artifact.Spec == nil {
		if result.Status != ExecutionError || result.ErrorClass != "configuration" || result.Run != nil || result.Target != nil || result.ObservationBoundary != "" || len(result.Assertions) != 0 || result.InitialObservation != nil || result.FinalObservation != nil || result.ReceiverSessionID != "" || result.ReceiverMode != "" {
			return nil, invalid
		}
		return artifact, nil
	}
	spec := *artifact.Spec
	if result.ObservationBoundary != spec.Observation.Boundary {
		return nil, invalid
	}
	if result.Run == nil {
		if result.Status != ExecutionError || !sameJSON(result.Assertions, pending(spec)) || result.FinalObservation != nil || result.ReceiverSessionID != "" || result.ReceiverMode != "" {
			return nil, invalid
		}
		switch result.ErrorClass {
		case "configuration":
			if result.Target != nil || result.InitialObservation != nil {
				return nil, invalid
			}
		case "configuration_changed":
			if result.Target == nil || result.InitialObservation != nil {
				return nil, invalid
			}
		case "initial_observation":
			if result.Target == nil || spec.Observation.Boundary != LedgerBoundary || initialState(artifact.InitialObservation) {
				return nil, invalid
			}
		default:
			return nil, invalid
		}
		return artifact, nil
	}
	if spec.Observation.Boundary == ACKBoundary && (result.InitialObservation != nil || result.FinalObservation != nil) {
		return nil, invalid
	}
	status, class, assertions := evaluate(spec, artifact.Run, artifact.InitialObservation, artifact.FinalObservation)
	if status != result.Status || class != result.ErrorClass || !sameJSON(assertions, result.Assertions) {
		return nil, invalid
	}
	if status != ExecutionError && artifact.FinalObservation != nil {
		if result.ReceiverSessionID != artifact.FinalObservation.SessionID || result.ReceiverMode != artifact.FinalObservation.Mode {
			return nil, invalid
		}
	} else if result.ReceiverSessionID != "" || result.ReceiverMode != "" {
		return nil, invalid
	}
	return artifact, nil
}
