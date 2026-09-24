package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
)

func TestEngineImportActualSourceExportsThroughPublicCLI(t *testing.T) {
	for _, target := range []struct {
		engine, version, expectedSHA string
	}{
		{"mirth", "4.5.2", "126c628591d15244e4a4459c9f5d98fb1cb15bdb72a5e4d0bb24c97f66ffe125"},
		{"oie", "4.6.0", "126c628591d15244e4a4459c9f5d98fb1cb15bdb72a5e4d0bb24c97f66ffe125"},
	} {
		t.Run(target.engine, func(t *testing.T) {
			root := t.TempDir()
			plan := writeDocument(t, root, "adapter.json", fmt.Sprintf(`{"schema":"readmit-engine-export/v1","engine":%q,"version":%q,"format":"message-xml","terminator":"cr"}`, target.engine, target.version))
			source, err := filepath.Abs(filepath.Join("..", "testdata", "engineexport", target.engine+"-"+target.version, "source", "1.xml"))
			if err != nil {
				t.Fatal(err)
			}
			container, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			preview, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--preview")
			if err != nil || stderr != "" || !strings.Contains(preview, `"stage":"raw"`) || !strings.Contains(preview, `"correlation":"unknown"`) || strings.Contains(preview, "SYNTH-") {
				t.Fatalf("public preview refused or disclosed values: %v %s %s", err, preview, stderr)
			}
			output := filepath.Join(root, "imported")
			stdout, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", output)
			if err != nil || stderr != "" || strings.Contains(stdout, "SYNTH-") {
				t.Fatalf("public import refused or disclosed values: %v %s %s", err, stdout, stderr)
			}
			opened, err := bundle.Open(output)
			if err != nil || opened.Manifest.Schema != "readmit-case/v5" || len(opened.Events) != 1 || opened.Events[0].Direction != bundle.Unknown || opened.Events[0].ObservedAt != nil {
				t.Fatalf("retained engine evidence or uncertainty changed: %v", err)
			}
			original, err := opened.EngineContainer()
			if err != nil || !bytes.Equal(original, container) {
				t.Fatal("the imported case lost exact engine export bytes")
			}
			payload, err := opened.Raw(opened.Events[0].ID)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(payload)) != target.expectedSHA {
				t.Fatal("the imported source raw bytes differ from the independent synthetic input")
			}
			if timeline, stderr, err := run(t, "timeline", output); err != nil || stderr != "" || strings.Contains(timeline, "SYNTH-") {
				t.Fatalf("public timeline refused or disclosed a value: %v %s %s", err, timeline, stderr)
			}
		})
	}
}

func TestEngineImportPublicCLIRefusesActualUnsupportedVariantsWithoutOutput(t *testing.T) {
	for _, target := range []struct{ engine, version, variant, id string }{
		{"mirth", "4.5.2", "multi", "3"},
		{"mirth", "4.5.2", "encrypted", "1"},
		{"mirth", "4.5.2", "attachment", "1"},
		{"oie", "4.6.0", "multi", "1"},
		{"oie", "4.6.0", "encrypted", "1"},
		{"oie", "4.6.0", "attachment", "1"},
	} {
		t.Run(target.engine+"/"+target.variant, func(t *testing.T) {
			root := t.TempDir()
			plan := writeDocument(t, root, "adapter.json", fmt.Sprintf(`{"schema":"readmit-engine-export/v1","engine":%q,"version":%q,"format":"message-xml","terminator":"cr"}`, target.engine, target.version))
			source, err := filepath.Abs(filepath.Join("..", "testdata", "engineexport", target.engine+"-"+target.version, target.variant, target.id+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "refused")
			for _, args := range [][]string{{"--preview"}, {"--output", output}} {
				command := []string{"import", "engine", "--plan", plan, "--file", source}
				command = append(command, args...)
				stdout, stderr, err := run(t, command...)
				if err == nil || stdout != "" || strings.Contains(stderr, "SYNTH-") {
					t.Fatalf("unsupported engine variant admitted or disclosed: %v %s %s", err, stdout, stderr)
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("refused export left a case")
				}
			}
		})
	}
}

func TestEngineImportRetainsActualEngineExportsOfMalformedSourceBytes(t *testing.T) {
	for _, target := range []struct{ engine, version, id, expectedSHA string }{
		{"mirth", "4.5.2", "7", "8480be4d13bbe63ffb9409cfeb55cde870fa2b2f9e89bbdc83b415f2cbfd1252"},
		{"mirth", "4.5.2", "8", "5493a43c9b1ec502c1b2bc78fc468a9fbe4e24727ce872d5a03518774272b5e2"},
		{"oie", "4.6.0", "7", "8480be4d13bbe63ffb9409cfeb55cde870fa2b2f9e89bbdc83b415f2cbfd1252"},
		{"oie", "4.6.0", "8", "5493a43c9b1ec502c1b2bc78fc468a9fbe4e24727ce872d5a03518774272b5e2"},
	} {
		t.Run(target.engine+"/"+target.id, func(t *testing.T) {
			root := t.TempDir()
			plan := writeDocument(t, root, "adapter.json", fmt.Sprintf(`{"schema":"readmit-engine-export/v1","engine":%q,"version":%q,"format":"message-xml","terminator":"cr"}`, target.engine, target.version))
			source, err := filepath.Abs(filepath.Join("..", "testdata", "engineexport", target.engine+"-"+target.version, "negative", target.id+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "case")
			if stdout, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", output); err != nil || stderr != "" || strings.Contains(stdout, "SYNTH-") {
				t.Fatalf("malformed source was not retained privately: %v %s %s", err, stdout, stderr)
			}
			opened, err := bundle.Open(output)
			if err != nil || len(opened.Events) != 1 || opened.Events[0].Kind != bundle.Unparsed {
				t.Fatalf("malformed engine source was not quarantined: %v", err)
			}
			raw, err := opened.Raw(opened.Events[0].ID)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != target.expectedSHA {
				t.Fatal("quarantine did not retain the malformed source bytes")
			}
		})
	}
}

func TestEngineImportRawFallbackRetainsExporterAddedSeparatorsWithoutClaimingAStage(t *testing.T) {
	for _, target := range []struct{ engine, version string }{{"mirth", "4.5.2"}, {"oie", "4.6.0"}} {
		t.Run(target.engine, func(t *testing.T) {
			root := t.TempDir()
			plan := writeDocument(t, root, "adapter.json", fmt.Sprintf(`{"schema":"readmit-engine-export/v1","engine":%q,"version":%q,"format":"raw","terminator":"cr"}`, target.engine, target.version))
			source, err := filepath.Abs(filepath.Join("..", "testdata", "engineexport", target.engine+"-"+target.version, "raw", "1.xml"))
			if err != nil {
				t.Fatal(err)
			}
			container, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			preview, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--preview")
			if err != nil || stderr != "" || !strings.Contains(preview, `"stage":"unknown"`) || strings.Contains(preview, "SYNTH-") {
				t.Fatalf("raw fallback preview guessed a stage or disclosed content: %v %s %s", err, preview, stderr)
			}
			output := filepath.Join(root, "raw-case")
			if _, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", output); err != nil || stderr != "" {
				t.Fatalf("raw fallback import: %v %s", err, stderr)
			}
			opened, err := bundle.Open(output)
			if err != nil || len(opened.Events) != 1 || opened.Events[0].Kind != bundle.Unparsed {
				t.Fatalf("exporter separators were treated as a complete HL7 message: %v", err)
			}
			original, err := opened.EngineContainer()
			if err != nil || !bytes.Equal(original, container) {
				t.Fatal("raw fallback changed the engine's bytes")
			}
		})
	}
}

func TestEngineImportPublicPreviewAndRetainedRawEvidence(t *testing.T) {
	dir := t.TempDir()
	plan := writeDocument(t, dir, "adapter.json", `{"schema":"readmit-engine-export/v1","engine":"oie","version":"4.6.0","format":"raw","terminator":"cr"}`)
	raw := importFixture("ENGINE-PRIVATE") + "\r\n\r\n\r\n"
	file := writeDocument(t, dir, "export.hl7", raw)
	out, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", file, "--preview")
	if err != nil || stderr != "" || !strings.Contains(out, `"stage":"unknown"`) || !strings.Contains(out, `"qualification":"unqualified"`) || strings.Contains(out, "ENGINE-PRIVATE") {
		t.Fatalf("preview: %v %s %s", err, out, stderr)
	}
	dest := filepath.Join(dir, "case")
	out, stderr, err = run(t, "import", "engine", "--plan", plan, "--file", file, "--output", dest)
	if err != nil || stderr != "" || strings.Contains(out, "ENGINE-PRIVATE") {
		t.Fatalf("import: %v %s %s", err, out, stderr)
	}
	b, err := bundle.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := b.EngineContainer()
	if err != nil || !bytes.Equal(retained, []byte(raw)) {
		t.Fatal("container changed")
	}
	if _, _, err := run(t, "import", "engine", "--plan", plan, "--file", file, "--output", dest); err == nil {
		t.Fatal("overwrote evidence")
	}
	before, _ := os.ReadFile(file)
	if string(before) != raw {
		t.Fatal("changed original")
	}
}

func TestEngineImportStructuredSubsetAndPrivacyRefusal(t *testing.T) {
	dir := t.TempDir()
	plan := writeDocument(t, dir, "adapter.json", `{"schema":"readmit-engine-export/v1","engine":"oie","version":"4.6.0","format":"message-xml","terminator":"cr"}`)
	// Independently hand-authored model example, not an engine-generated fixture.
	document := `<message><connectorMessages><entry><int>0</int><connectorMessage><metaDataId>0</metaDataId><raw><contentType>RAW</contentType><content>MSH|^~\&amp;|S|F|R|F|20260101||ADT^A01|PRIVATE-ENGINE|P|2.5.1&#13;PID|1</content><dataType>HL7V2</dataType><encrypted>false</encrypted></raw></connectorMessage></entry></connectorMessages></message>`
	file := writeDocument(t, dir, "messages.xml", document+"\r\n"+document)
	output := filepath.Join(dir, "case")
	stdout, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", file, "--output", output)
	if err != nil || stderr != "" || strings.Contains(stdout+stderr, "PRIVATE-ENGINE") {
		t.Fatalf("structured import: %v %s %s", err, stdout, stderr)
	}
	b, err := bundle.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Events) != 2 {
		t.Fatal("duplicate content was lost")
	}
	for _, e := range b.Events {
		raw, err := b.Raw(e.ID)
		if err != nil || string(raw) != "MSH|^~\\&|S|F|R|F|20260101||ADT^A01|PRIVATE-ENGINE|P|2.5.1\rPID|1" {
			t.Fatal("XML bytes decoded incorrectly")
		}
	}
	bad := writeDocument(t, dir, "unsupported.xml", strings.Replace(document, "<encrypted>false", "<encrypted>true", 1))
	refused := filepath.Join(dir, "refused")
	stdout, stderr, err = run(t, "import", "engine", "--plan", plan, "--file", bad, "--output", refused)
	if err == nil || strings.Contains(stdout+stderr, "PRIVATE-ENGINE") {
		t.Fatal("unsupported content accepted or disclosed")
	}
	if _, err := os.Stat(refused); !os.IsNotExist(err) {
		t.Fatal("unsupported file left output")
	}
}

func TestEngineImportRecoversWithFreshDestinationAfterIncompleteCase(t *testing.T) {
	dir := t.TempDir()
	plan := writeDocument(t, dir, "adapter.json", `{"schema":"readmit-engine-export/v1","engine":"mirth","version":"4.5.2","format":"raw","terminator":"cr"}`)
	source := writeDocument(t, dir, "message.hl7", importFixture("RECOVERY"))
	partial := filepath.Join(dir, "partial")
	if _, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", partial); err != nil {
		t.Fatalf("setup: %v %s", err, stderr)
	}
	// An interrupted writer has no completion marker; model that deterministic
	// persisted state rather than racing a process signal against filesystem IO.
	if err := os.Remove(filepath.Join(partial, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "timeline", partial); err == nil {
		t.Fatal("incomplete case accepted")
	}
	if _, _, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", partial); err == nil {
		t.Fatal("incomplete evidence overwritten")
	}
	fresh := filepath.Join(dir, "retry")
	if _, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", fresh); err != nil {
		t.Fatalf("retry: %v %s", err, stderr)
	}
	if _, err := bundle.Open(fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(partial, "identity.sha256")); !os.IsNotExist(err) {
		t.Fatal("retry changed incomplete evidence")
	}
}

func TestEngineImportProcessInterruptionRetainsIncompleteAndRetriesFresh(t *testing.T) {
	for _, stop := range []struct {
		name   string
		signal os.Signal
	}{
		{"kill", os.Kill}, {"interrupt", os.Interrupt},
	} {
		t.Run(stop.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && stop.signal == os.Interrupt {
				t.Skip("Windows does not support sending os.Interrupt to another process; portable kill is covered")
			}
			dir := t.TempDir()
			plan := writeDocument(t, dir, "adapter.json", `{"schema":"readmit-engine-export/v1","engine":"oie","version":"4.6.0","format":"message-xml","terminator":"cr"}`)
			// Enough separately synced payloads to interrupt a real writer after its
			// exclusive reservation. This is synthetic source-model data, not a lab export.
			record := `<message><connectorMessages><entry><int>0</int><connectorMessage><metaDataId>0</metaDataId><raw><contentType>RAW</contentType><content>MSH|^~\&amp;|S|F|R|F|20260101||ADT^A01|PROCESS-PRIVATE|P|2.5.1&#13;OBX|1|TX|SYNTHETIC||` + strings.Repeat("X", 64<<10) + `</content><dataType>HL7V2</dataType><encrypted>false</encrypted></raw></connectorMessage></entry></connectorMessages></message>`
			document := strings.Repeat(record, 128)
			source := writeDocument(t, dir, "messages.xml", document)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			verifyCompleted := func(path string) {
				t.Helper()
				b, err := bundle.Open(path)
				if err != nil {
					t.Fatalf("completed attempt did not verify: %v", err)
				}
				original, err := b.EngineContainer()
				if err != nil || string(original) != document || len(b.Events) != 128 {
					t.Fatal("completed attempt changed evidence")
				}
			}
			partial := ""
			for attempt := 0; attempt < 3 && partial == ""; attempt++ {
				output := filepath.Join(dir, fmt.Sprintf("interrupted-%d", attempt))
				command := testCommand(ctx, t, "import", "engine", "--plan", plan, "--file", source, "--output", output)
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan struct{})
				var waitErr error
				go func() { waitErr = command.Wait(); close(done) }()
				t.Cleanup(func() { _ = command.Process.Kill(); <-done })
				tick := time.NewTicker(100 * time.Microsecond)
				reserved := false
			waitForReservation:
				for {
					if info, err := os.Stat(output); err == nil && info.IsDir() {
						reserved = true
						break
					}
					select {
					case <-done:
						break waitForReservation
					case <-ctx.Done():
						tick.Stop()
						t.Fatal("import never reserved output")
					case <-tick.C:
					}
				}
				tick.Stop()
				marker := filepath.Join(output, "identity.sha256")
				if _, err := os.Stat(marker); err == nil {
					<-done
					verifyCompleted(output)
					continue
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if !reserved {
					t.Fatalf("import exited before output reservation: %v %s", waitErr, stderr.String())
				}
				signalErr := command.Process.Signal(stop.signal)
				<-done
				// Reservation does not pause the child. If it won the scheduling race,
				// verify the completed artifact and try a fresh destination. Never call
				// a complete case partial merely because termination arrived late.
				if _, err := os.Stat(marker); err == nil {
					verifyCompleted(output)
					continue
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if signalErr != nil {
					t.Fatal(signalErr)
				}
				if waitErr == nil || strings.Contains(stdout.String(), "Engine export imported") || strings.Contains(stdout.String()+stderr.String(), "PROCESS-PRIVATE") {
					t.Fatal("interrupted import claimed completion or disclosed payload")
				}
				partial = output
			}
			if partial == "" {
				t.Fatal("three imports completed before interruption; no interrupted-write coverage obtained")
			}
			if _, err := bundle.Open(partial); err == nil {
				t.Fatal("interrupted case verified")
			}
			if _, _, err := run(t, "timeline", partial); err == nil {
				t.Fatal("CLI accepted incomplete evidence")
			}
			if _, _, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", partial); err == nil {
				t.Fatal("retry overwrote interrupted evidence")
			}
			fresh := filepath.Join(dir, "retry")
			if _, stderr, err := run(t, "import", "engine", "--plan", plan, "--file", source, "--output", fresh); err != nil {
				t.Fatalf("fresh retry: %v %s", err, stderr)
			}
			b, err := bundle.Open(fresh)
			if err != nil {
				t.Fatal(err)
			}
			original, err := b.EngineContainer()
			if err != nil || string(original) != document || len(b.Events) != 128 {
				t.Fatal("fresh retry changed original container or dropped records")
			}
			original, err = os.ReadFile(source)
			if err != nil || string(original) != document {
				t.Fatal("interruption changed source")
			}
			if _, err := os.Stat(filepath.Join(partial, "identity.sha256")); !os.IsNotExist(err) {
				t.Fatal("fresh retry rewrote incomplete evidence")
			}
		})
	}
}
