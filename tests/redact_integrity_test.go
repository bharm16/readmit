package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	encodingbinary "encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func redactDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Independently implement the documented identity framing for coherent-forgery
// regressions. A reader must enforce content relationships beyond fresh hashes.
func redactIdentity(schema string, files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "identity.sha256" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	hash := sha256.New()
	hash.Write([]byte(schema + "\n"))
	for _, name := range names {
		for _, raw := range [][]byte{[]byte(name), files[name]} {
			encodingbinary.Write(hash, encodingbinary.BigEndian, uint64(len(raw)))
			hash.Write(raw)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func redactReseal(t *testing.T, dir, schema string) string {
	t.Helper()
	id := redactIdentity(schema, redactTree(t, dir))
	if err := os.WriteFile(filepath.Join(dir, "identity.sha256"), []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return id
}

func redactPacketFixture(t *testing.T) string {
	t.Helper()
	request := redactFixture(t)
	review, err := redact.Create(context.Background(), request)
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("packet review: %+v %v", review, err)
	}
	packet := filepath.Join(filepath.Dir(request.Output), "packet")
	if _, err := redact.Export(context.Background(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: packet}); err != nil {
		t.Fatal(err)
	}
	return packet
}

func redactAddPlantedNote(t *testing.T, raw []byte) []byte {
	t.Helper()
	end := []byte{0x1c, '\r'}
	if !bytes.HasSuffix(raw, end) {
		t.Fatal("forgery fixture must be a complete MLLP frame")
	}
	return append(append(bytes.Clone(raw[:len(raw)-2]), []byte("NTE|1||PLANTED-NTE-ALDER\r")...), end...)
}

func redactForgeRetainedSource(t *testing.T, resultPath string) {
	t.Helper()
	artifact, err := testrunner.Open(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	run := artifact.Run
	event := &run.Events[0]
	for _, payload := range []*bundle.Payload{&event.Source, &event.Intended, &event.Sent} {
		raw, err := run.Raw(*payload)
		if err != nil {
			t.Fatal(err)
		}
		raw = redactAddPlantedNote(t, raw)
		if err := os.WriteFile(filepath.Join(resultPath, "run", payload.Path), raw, 0600); err != nil {
			t.Fatal(err)
		}
		payload.Size, payload.SHA256 = len(raw), redactDigest(raw)
	}
	run.Manifest.Mappings[0].SourceSHA256 = event.Source.SHA256
	redactResealResultRun(t, resultPath, artifact)
}

func redactResealResultRun(t *testing.T, resultPath string, artifact *testrunner.Artifact) {
	t.Helper()
	run := artifact.Run
	redactJSON(t, filepath.Join(resultPath, "run", "manifest.json"), run.Manifest)
	var lines []byte
	for _, event := range run.Events {
		raw, err := json.Marshal(event, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, raw...), '\n')
	}
	if err := os.WriteFile(filepath.Join(resultPath, "run", "events.jsonl"), lines, 0600); err != nil {
		t.Fatal(err)
	}
	artifact.Result.Run.Identity = redactReseal(t, filepath.Join(resultPath, "run"), replay.Schema)
	redactJSON(t, filepath.Join(resultPath, "result.json"), artifact.Result)
	redactReseal(t, resultPath, testrunner.Schema)
	if _, err := testrunner.Open(resultPath); err != nil {
		t.Fatalf("forgery must remain a coherent independently verified result: %v", err)
	}
}

func redactForgeReceiver(t *testing.T, path string, direction bundle.Direction) {
	t.Helper()
	redactRewriteReceiver(t, path, direction, func(raw []byte) []byte { return redactAddPlantedNote(t, raw) })
}

func redactRewriteReceiver(t *testing.T, path string, direction bundle.Direction, rewrite func([]byte) []byte) {
	t.Helper()
	received, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []bundle.Input{}
	changed := false
	for _, source := range received.Manifest.Sources {
		input := bundle.Input{Options: hl7.Options{Format: source.Format, Terminator: source.Terminator}, Observations: map[int]bundle.Observation{}}
		for _, event := range received.Events {
			if event.SourceID != source.ID {
				continue
			}
			raw, err := received.Raw(event.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !changed && event.Direction == direction {
				raw = rewrite(raw)
				changed = true
			}
			input.Data = append(input.Data, raw...)
			input.Observations[event.Sequence] = bundle.Observation{Direction: event.Direction, ObservedAt: event.ObservedAt}
		}
		inputs = append(inputs, input)
	}
	if !changed {
		t.Fatal("receiver fixture lacks the selected direction")
	}
	if err := os.Rename(path, filepath.Join(t.TempDir(), "previous-receiver.case")); err != nil {
		t.Fatal(err)
	}
	// Construct the forged case outside finalized evidence. Public writers must
	// refuse the sealed packet; the test itself then installs the adversarial bytes.
	replacement := filepath.Join(t.TempDir(), "replacement.case")
	if _, err := bundle.WriteRecorded(replacement, inputs, *received.Manifest.Provenance.StartedAt, *received.Observation); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Open(path); err != nil {
		t.Fatalf("forged receiver case must preserve its valid unchanged observation: %v", err)
	}
}

func TestRedactOpenExportRejectsPairedUnreviewedACKBytes(t *testing.T) {
	original := redactPacketFixture(t)
	for _, change := range []string{"note-segment", "error-segment", "msa-free-text", "header-metadata", "timestamp-text"} {
		t.Run(change, func(t *testing.T) {
			packet := filepath.Join(t.TempDir(), "packet")
			if err := os.CopyFS(packet, os.DirFS(original)); err != nil {
				t.Fatal(err)
			}
			manifest := redactReadJSON[redact.ExportManifest](t, filepath.Join(packet, "export-review.json"))
			rewrite := func(raw []byte) []byte {
				switch change {
				case "note-segment":
					return redactAddPlantedNote(t, raw)
				case "error-segment":
					return bytes.Replace(raw, []byte{0x1c, '\r'}, []byte("ERR|PLANTED-NTE-ALDER\r\x1c\r"), 1)
				case "msa-free-text":
					return bytes.Replace(raw, []byte("\rZRT|"), []byte("|PLANTED-NTE-ALDER\rZRT|"), 1)
				case "timestamp-text":
					doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
					if err != nil {
						t.Fatal(err)
					}
					span := doc.Messages[0].Segments[0].Field(7).Span
					return append(append(bytes.Clone(raw[:span.Start]), []byte("PLANTED-NTE-ALDER")...), raw[span.End:]...)
				default:
					return bytes.Replace(raw, []byte("|READMIT|FIXTURE|"), []byte("|READMIT|PLANTED-NTE-ALDER|"), 1)
				}
			}
			resultPath := filepath.Join(packet, "proof", "baseline", "result")
			artifact, err := testrunner.Open(resultPath)
			if err != nil {
				t.Fatal(err)
			}
			payload := &artifact.Run.Events[0].Received
			raw, err := artifact.Run.Raw(*payload)
			if err != nil {
				t.Fatal(err)
			}
			changed := rewrite(raw)
			if bytes.Equal(changed, raw) {
				t.Fatal("ACK fixture was not changed")
			}
			if err := os.WriteFile(filepath.Join(resultPath, "run", payload.Path), changed, 0600); err != nil {
				t.Fatal(err)
			}
			payload.Size, payload.SHA256 = len(changed), redactDigest(changed)
			redactResealResultRun(t, resultPath, artifact)
			redactRewriteReceiver(t, filepath.Join(packet, "proof", "baseline", "receiver.case"), bundle.Outbound, rewrite)
			redactResealPacket(t, packet, manifest)
			if _, err := redact.OpenExport(packet); err == nil {
				t.Fatal("matching resealed ACK copies concealed unreviewed content")
			}
		})
	}
}

func redactResealPacket(t *testing.T, packet string, manifest redact.ExportManifest) {
	t.Helper()
	baseline, err := testrunner.Open(filepath.Join(packet, "proof", "baseline", "result"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Proof.BaselineIdentity = baseline.Identity
	files := redactTree(t, packet)
	for i := range manifest.Files {
		file := &manifest.Files[i]
		file.Size, file.SHA256 = len(files[file.Path]), redactDigest(files[file.Path])
	}
	redactJSON(t, filepath.Join(packet, "export-review.json"), manifest)
	redactReseal(t, packet, redact.ExportSchema)
}

func TestRedactOpenExportRejectsCoherentlyResealedProofBytes(t *testing.T) {
	original := redactPacketFixture(t)
	for _, change := range []string{"retained-source", "receiver-sent", "receiver-ack"} {
		t.Run(change, func(t *testing.T) {
			packet := filepath.Join(t.TempDir(), "packet")
			if err := os.CopyFS(packet, os.DirFS(original)); err != nil {
				t.Fatal(err)
			}
			manifest := redactReadJSON[redact.ExportManifest](t, filepath.Join(packet, "export-review.json"))
			receiverPath := filepath.Join(packet, "proof", "baseline", "receiver.case")
			switch change {
			case "retained-source":
				redactForgeRetainedSource(t, filepath.Join(packet, "proof", "baseline", "result"))
				// Also match the forged receiver to that new sent payload: only
				// comparison with the approved derived case can detect this case.
				redactForgeReceiver(t, receiverPath, bundle.Inbound)
			case "receiver-sent":
				redactForgeReceiver(t, receiverPath, bundle.Inbound)
			case "receiver-ack":
				redactForgeReceiver(t, receiverPath, bundle.Outbound)
			}
			redactResealPacket(t, packet, manifest)
			if _, err := redact.OpenExport(packet); err == nil {
				t.Fatal("coherent hashes and unchanged verdicts concealed unapproved proof bytes")
			}
		})
	}
}

func TestRedactOpenExportValidatesTheEmbeddedReviewContract(t *testing.T) {
	original := redactPacketFixture(t)
	for _, change := range []string{"schema", "coverage", "origin"} {
		t.Run(change, func(t *testing.T) {
			packet := filepath.Join(t.TempDir(), "packet")
			if err := os.CopyFS(packet, os.DirFS(original)); err != nil {
				t.Fatal(err)
			}
			manifest := redactReadJSON[redact.ExportManifest](t, filepath.Join(packet, "export-review.json"))
			switch change {
			case "schema":
				manifest.Review.Schema = "readmit-export-review/v999"
			case "coverage":
				manifest.Review.Coverage[0].Handled++
			case "origin":
				manifest.Review.DataOrigin = "generated"
			}
			files := redactTree(t, packet)
			raw, err := json.Marshal(manifest.Review, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			reviewFiles := map[string][]byte{"review.json": append(raw, '\n'), "spec.json": files["spec.json"]}
			for name, raw := range files {
				if strings.HasPrefix(name, "case/") {
					reviewFiles[name] = raw
				}
			}
			manifest.ApprovedReview = redactIdentity(redact.ReviewSchema, reviewFiles)
			redactResealPacket(t, packet, manifest)
			if _, err := redact.OpenExport(packet); err == nil {
				t.Fatal("resealed packet accepted an invalid nested review contract")
			}
		})
	}
}

func TestRedactOpenExportRejectsCoherentlyResealedLedger(t *testing.T) {
	original := redactPacketFixture(t)
	for _, field := range []string{"patient", "placer", "filler", "time", "authority"} {
		t.Run(field, func(t *testing.T) {
			packet := filepath.Join(t.TempDir(), "packet")
			if err := os.CopyFS(packet, os.DirFS(original)); err != nil {
				t.Fatal(err)
			}
			manifest := redactReadJSON[redact.ExportManifest](t, filepath.Join(packet, "export-review.json"))
			approved := manifest.ApprovedReview
			resultPath := filepath.Join(packet, "proof", "baseline", "result")
			artifact, err := testrunner.Open(resultPath)
			if err != nil {
				t.Fatal(err)
			}
			change := func(record *observation.Record) {
				switch field {
				case "patient":
					record.PatientID.Value = "PLANTED-PATIENT-7391"
				case "placer":
					record.PlacerID.Value = "UNAPPROVED-PLACER"
				case "filler":
					record.FillerID.Value = "UNAPPROVED-FILLER"
				case "time":
					record.AppointmentStart = "20260101000000+0000"
				case "authority":
					record.PatientID.Namespace = "UNAPPROVED-AUTHORITY"
				}
			}
			change(&artifact.FinalObservation.Records[0])
			raw, err := observation.Encode(*artifact.FinalObservation)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(resultPath, "observation.json"), filepath.Join(packet, "proof", "baseline", "observation.json")} {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			artifact.Result.FinalObservation.Size, artifact.Result.FinalObservation.SHA256 = len(raw), redactDigest(raw)
			for i := range artifact.Result.Assertions {
				if value := artifact.Result.Assertions[i].Observed; value != nil && value.Records != nil {
					change(&(*value.Records)[0])
				}
			}
			redactJSON(t, filepath.Join(resultPath, "result.json"), artifact.Result)
			redactReseal(t, resultPath, testrunner.Schema)
			receiverPath := filepath.Join(packet, "proof", "baseline", "receiver.case")
			recorded := redactReadJSON[bundle.Manifest](t, filepath.Join(receiverPath, "manifest.json"))
			recorded.Observation.Size, recorded.Observation.SHA256 = len(raw), redactDigest(raw)
			if err := os.WriteFile(filepath.Join(receiverPath, "observation.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			redactJSON(t, filepath.Join(receiverPath, "manifest.json"), recorded)
			redactReseal(t, receiverPath, bundle.RecordedSchema)
			if _, err := bundle.Open(receiverPath); err != nil {
				t.Fatalf("recorded observation must remain coherent: %v", err)
			}
			// Generic result verification still agrees with the retained ledger and
			// unchanged failures. Only fixture proof can reject the invented values.
			redactResealPacket(t, packet, manifest)
			if manifest.ApprovedReview != approved {
				t.Fatal("test changed the review commitment")
			}
			if _, err := redact.OpenExport(packet); err == nil {
				t.Fatal("accepted ledger values that cannot follow from the approved case")
			}
		})
	}
}
