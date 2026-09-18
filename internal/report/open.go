package report

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Open is offline and never resolves historical source/spec/target paths. It
// verifies the index, nested artifact contracts, canonical synthetic evidence,
// both verdicts, and every derived report and profile snapshot.
func Open(dir string) (*Packet, error) {
	// Resolve a raw symlink/../ traversal before joining retained relative
	// paths. A symlink used as the packet root itself remains unsupported.
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("report packet must be a regular directory")
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, errors.New("cannot resolve report packet")
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, errors.New("cannot resolve report packet")
	}
	files, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed, or unsupported report packet")
	raw, ok := files["manifest.json"]
	if !ok || len(raw) > 1<<20 || string(files["identity.sha256"]) != digest(raw)+"\n" {
		return nil, invalid
	}
	var manifest Manifest
	if json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)) != nil || !reflect.DeepEqual(manifest.Files, index(files)) {
		return nil, invalid
	}
	expected, additions, err := inspectEvidence(dir, files)
	if err != nil {
		return nil, err
	}
	expected.Files = index(files)
	expectedBytes, err := encode(expected)
	if err != nil || !bytes.Equal(raw, expectedBytes) {
		return nil, invalid
	}
	for name, data := range additions {
		if !bytes.Equal(files[name], data) {
			return nil, invalid
		}
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" || additions[name] != nil || strings.HasPrefix(name, "reproducer/") || strings.HasPrefix(name, "baseline/") || strings.HasPrefix(name, "post-fix/") {
			continue
		}
		return nil, invalid
	}
	return &Packet{Manifest: manifest, Identity: digest(raw), files: files}, nil
}

func inspectEvidence(dir string, files map[string][]byte) (Manifest, map[string][]byte, error) {
	invalid := errors.New("report evidence does not match the committed synthetic scenario")
	var manifest Manifest
	if !bytes.Equal(files["spec.json"], scenarioSpec) {
		return manifest, nil, invalid
	}
	source, err := bundle.Open(filepath.Join(dir, "reproducer"))
	if err != nil || source.Identity != caseIdentity || source.Manifest.Provenance.Mode != bundle.Generated {
		return manifest, nil, invalid
	}
	// This frozen, independently calculated identity binds every source byte,
	// provenance field and occurrence. A generated label alone is insufficient.
	manifest = Manifest{Schema: Schema, State: "complete", Scenario: Scenario, Provenance: "synthetic-only", Generator: generatorInputs(), InputIdentity: source.Identity, SpecIdentity: digest(scenarioSpec), ObservationBoundary: testrunner.LedgerBoundary, InputChanged: false, ReceiverBehaviorChanged: true, Limitations: append([]string{}, limitations...), Runs: []RunLabel{}}
	additions := map[string][]byte{"spec.json": scenarioSpec, "profiles/receiver.json": receiver.ProfileSnapshot(), "profiles/diagnosis.json": diagnose.ProfileSnapshot()}
	for _, trial := range []struct {
		name   string
		mode   observation.Mode
		status testrunner.Status
	}{{"baseline", observation.Defective, testrunner.AssertionFailure}, {"post-fix", observation.Fixed, testrunner.Pass}} {
		artifact, err := testrunner.Open(filepath.Join(dir, trial.name))
		if err != nil {
			return manifest, nil, invalid
		}
		result := artifact.Result
		if result.Status != trial.status || result.ReceiverMode != trial.mode || result.InputBundleIdentity != source.Identity || result.SpecIdentity != manifest.SpecIdentity || !bytes.Equal(files[trial.name+"/spec.json"], scenarioSpec) || result.ObservationBoundary != testrunner.LedgerBoundary || result.Target == nil || artifact.Run == nil {
			return manifest, nil, invalid
		}
		if err := canonicalObservations(artifact, trial.mode); err != nil {
			return manifest, nil, err
		}
		if err := canonicalACKs(artifact); err != nil {
			return manifest, nil, err
		}
		if !loopbackAddress(result.Target.Address) {
			return manifest, nil, invalid
		}
		wantTarget := target(result.Target.Address)
		rawTarget, err := encode(wantTarget)
		if err != nil {
			return manifest, nil, err
		}
		// Exact settings are owned by this scenario. No customer host, CA path,
		// arbitrary target labels or hidden transport settings enter a packet.
		if result.Target.Transport != "plain" || !result.Target.TestEndpoint || result.Target.ApprovedTransport || result.Target.CASHA256 != "" || result.Target.ConnectTimeout != wantTarget.ConnectTimeout || result.Target.MessageTimeout != wantTarget.MessageTimeout || result.Target.MaxACKBytes != wantTarget.MaxACKBytes {
			return manifest, nil, invalid
		}
		additions[trial.name+"-target.json"] = rawTarget
		comparison, err := diff.Compare(diff.Input{Path: filepath.Join(dir, "reproducer")}, diff.Input{Path: filepath.Join(dir, trial.name)}, diff.Options{Boundary: diff.Messages})
		if err != nil || comparison.Summary.Paired != 2 || comparison.Summary.Unchanged != 2 || comparison.Summary.Changed != 0 || comparison.Summary.Uncompared != 0 || len(comparison.Unsupported) != 0 {
			return manifest, nil, invalid
		}
		manifest.Runs = append(manifest.Runs, RunLabel{Path: trial.name, ResultIdentity: artifact.Identity, InputBundleIdentity: result.InputBundleIdentity, SpecIdentity: result.SpecIdentity, TargetIdentity: result.TargetIdentity, ReceiverImplementation: "readmit built-in SIU fixture", ReceiverProfile: observation.Profile, ReceiverMode: result.ReceiverMode, ReceiverSession: result.ReceiverSessionID, Status: result.Status, LedgerCount: len(artifact.FinalObservation.Records)})
	}
	if manifest.Runs[0].ReceiverSession == manifest.Runs[1].ReceiverSession {
		return manifest, nil, invalid
	}
	config := diagnose.DefaultConfig()
	diagnosis, err := diagnose.Run(filepath.Join(dir, "reproducer"), config)
	if err != nil {
		return manifest, nil, err
	}
	additions["diagnosis.json"], err = diagnose.JSON(diagnosis)
	if err != nil {
		return manifest, nil, err
	}
	additions["diagnosis.md"] = diagnose.Markdown(diagnosis)
	additions["profiles/diagnose-config.json"], err = encode(config)
	if err != nil {
		return manifest, nil, err
	}
	comparison, err := diff.Compare(diff.Input{Path: filepath.Join(dir, "baseline")}, diff.Input{Path: filepath.Join(dir, "post-fix")}, diff.Options{Boundary: diff.Messages})
	if err != nil {
		return manifest, nil, err
	}
	additions["diff.json"], err = diff.JSON(comparison)
	if err != nil {
		return manifest, nil, err
	}
	additions["diff.md"] = diff.Markdown(comparison)
	additions["history.json"] = []byte("{\"schema\":\"readmit-report-history/v1\",\"input_changed\":false,\"replay_transformations\":[],\"redaction\":\"none; committed synthetic scenario only\"}\n")
	additions["SUMMARY.md"] = summary(manifest)
	additions["RERUN.md"] = packetInstructions()
	return manifest, additions, nil
}

func loopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	n, parseErr := strconv.Atoi(port)
	ip := net.ParseIP(host)
	return err == nil && parseErr == nil && n > 0 && n <= 65535 && ip != nil && ip.IsLoopback() && net.JoinHostPort(ip.String(), strconv.Itoa(n)) == address
}

func canonicalObservations(artifact *testrunner.Artifact, mode observation.Mode) error {
	invalid := errors.New("report observation is not the canonical fixture ledger")
	if artifact.InitialObservation == nil || artifact.FinalObservation == nil || artifact.Spec == nil {
		return invalid
	}
	initial, final := artifact.InitialObservation, artifact.FinalObservation
	want, err := testrunner.DecodeSpec(scenarioSpec)
	if err != nil {
		return invalid
	}
	records := append([]observation.Record{}, (*want.Assertions[1].Expected.Records)...)
	if mode == observation.Defective {
		booked := records[0]
		booked.AppointmentStart = "20260102120000+0000"
		records[0].RecordID = "r000002"
		records = append([]observation.Record{booked}, records...)
	}
	processed := []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "SYNTH-000001"}, {OccurrenceID: "s0001-e000003", ControlID: "SYNTH-000002"}}
	if initial.Mode != mode || final.Mode != mode || len(initial.Records) != 0 || len(initial.Processed) != 0 || !reflect.DeepEqual(final.Records, records) || !reflect.DeepEqual(final.Processed, processed) {
		return invalid
	}
	return nil
}

// Retained ACKs may vary only in valid UTC timestamp and receipt session. This
// prevents arbitrary extra segments or values from hiding in synthetic evidence.
func canonicalACKs(artifact *testrunner.Artifact) error {
	invalid := errors.New("report ACK is not a canonical synthetic fixture acknowledgement")
	if len(artifact.Run.Events) != 2 {
		return invalid
	}
	for i, event := range artifact.Run.Events {
		raw, err := artifact.Run.Raw(event.Received)
		if err != nil {
			return invalid
		}
		doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
		if err != nil || len(doc.Messages) != 1 {
			return invalid
		}
		stamp := string(doc.Bytes(doc.Messages[0].Segments[0].Field(7).Span))
		parsed, err := time.Parse("20060102150405-0700", stamp)
		if err != nil || parsed.UTC().Format("20060102150405-0700") != stamp {
			return invalid
		}
		trigger := "S12"
		if i == 1 {
			trigger = "S13"
		}
		want := fmt.Sprintf("\x0bMSH|^~\\&|READMIT|FIXTURE|||%s||ACK^%s|READMITACK%06d|P|2.5.1\rMSA|AA|SYNTH-%06d\rZRT|readmit-receipt/v1|%s|s0001-e%06d\r\x1c\r", stamp, trigger, i+1, i+1, artifact.Result.ReceiverSessionID, 2*i+1)
		if !bytes.Equal(raw, []byte(want)) {
			return invalid
		}
	}
	return nil
}
