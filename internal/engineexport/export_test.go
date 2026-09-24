package engineexport_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/engineexport"
)

func engineFixture(t *testing.T, engine, variant, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "engineexport", engine, variant, name+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestActualEngineSourceExportsKeepOnlyTheDeclaredRawStage(t *testing.T) {
	// These are original message exports from isolated Mirth 4.5.2 and OIE
	// 4.6.0 labs, not XML assembled to mirror the extractor. The digests are
	// from independently generated synthetic inputs, before either engine saw
	// them. Source connector 0 must keep those bytes while the other retained
	// stages remain only in the enclosing container.
	want := []struct{ group, name, digest string }{
		{"source", "1", "126c628591d15244e4a4459c9f5d98fb1cb15bdb72a5e4d0bb24c97f66ffe125"},
		{"source", "2", "126c628591d15244e4a4459c9f5d98fb1cb15bdb72a5e4d0bb24c97f66ffe125"},
		{"source", "3", "d89fc975cc4bd414f889db8f7ccecd81178e5b41dc8761dc02bcd35d0c125d9b"},
		{"source", "4", "11404c60e4690a03b0b7074d190285885350cc95bd536330b501449a8b2d5502"},
		{"negative", "7", "8480be4d13bbe63ffb9409cfeb55cde870fa2b2f9e89bbdc83b415f2cbfd1252"},
		{"negative", "8", "5493a43c9b1ec502c1b2bc78fc468a9fbe4e24727ce872d5a03518774272b5e2"},
	}
	for _, engine := range []struct{ name, version string }{{"mirth-4.5.2", "4.5.2"}, {"oie-4.6.0", "4.6.0"}} {
		t.Run(engine.name, func(t *testing.T) {
			plan := engineexport.Plan{Schema: engineexport.Schema, Engine: strings.Split(engine.name, "-")[0], Version: engine.version, Format: "message-xml", Terminator: "cr"}
			for _, fixture := range want {
				container := engineFixture(t, engine.name, fixture.group, fixture.name)
				records, err := engineexport.Extract(plan, container)
				if err != nil || len(records) != 1 {
					t.Fatalf("%s/%s: source-raw export refused: %v (%d records)", fixture.group, fixture.name, err, len(records))
				}
				record := records[0]
				got := sha256.Sum256(record.Payload)
				if hex.EncodeToString(got[:]) != fixture.digest || record.Stage != "raw" || record.Correlation != "unknown" || record.Offset != 0 || record.Size >= len(container) {
					t.Fatalf("%s/%s: source raw bytes, stage, correlation or container range changed", fixture.group, fixture.name)
				}
			}
		})
	}
}

func TestActualEngineExportsRefuseUnselectedDestinationsEncryptionAndAttachments(t *testing.T) {
	for _, engine := range []struct {
		name, version, multiID string
	}{{"mirth-4.5.2", "4.5.2", "3"}, {"oie-4.6.0", "4.6.0", "1"}} {
		plan := engineexport.Plan{Schema: engineexport.Schema, Engine: strings.Split(engine.name, "-")[0], Version: engine.version, Format: "message-xml", Terminator: "cr"}
		for _, variant := range []struct{ folder, name string }{{"multi", engine.multiID}, {"encrypted", "1"}, {"attachment", "1"}} {
			t.Run(engine.name+"/"+variant.folder, func(t *testing.T) {
				if records, err := engineexport.Extract(plan, engineFixture(t, engine.name, variant.folder, variant.name)); err == nil || records != nil {
					t.Fatal("an unselected or unsafe engine variant was admitted")
				}
			})
		}
		raw := engineFixture(t, engine.name, "raw", "1")
		plan.Format = "raw"
		records, err := engineexport.Extract(plan, raw)
		if err != nil || len(records) != 1 || !bytes.Equal(records[0].Payload, raw) || records[0].Stage != "unknown" {
			t.Fatalf("%s raw fallback changed exporter bytes or inferred stage: %v", engine.name, err)
		}
	}
}

func TestActualEngineSourceExportRefusesRecastStagesAndXStreamClasses(t *testing.T) {
	plan := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.2", Format: "message-xml", Terminator: "cr"}
	original := engineFixture(t, "mirth-4.5.2", "source", "1")
	for _, change := range []struct{ name, from, to string }{
		{"source connector id", "<int>0</int>", "<int>1</int>"},
		{"source metadata id", "<metaDataId>0</metaDataId>", "<metaDataId>1</metaDataId>"},
		{"processed stage type", "<contentType>PROCESSED_RAW</contentType>", "<contentType>RAW</contentType>"},
		{"encoded stage encryption", "<encoded>\n", "<encoded>\n<encrypted>true</encrypted>"},
		{"map class", `class="java.util.Collections$UnmodifiableMap"`, `class="java.lang.Runtime"`},
		{"source raw reference", "<raw>", `<raw reference="../outside">`},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := bytes.Replace(original, []byte(change.from), []byte(change.to), 1)
			if bytes.Equal(changed, original) {
				t.Fatal("test mutation did not reach engine export")
			}
			if records, err := engineexport.Extract(plan, changed); err == nil || records != nil {
				t.Fatal("an altered stage or class was accepted")
			}
		})
	}
}

// Hand-authored source-model example, never a real engine export or matrix acceptance.
const structured = `<message><messageId>1</messageId><connectorMessages><entry><int>0</int><connectorMessage><metaDataId>0</metaDataId><raw><contentType>RAW</contentType><content>MSH|^~\&amp;|S|F|R|F|20260101||ADT^A01|one|P|2.5.1&#13;PID|1</content><dataType>HL7V2</dataType><encrypted>false</encrypted></raw></connectorMessage></entry></connectorMessages></message>`

func TestStructuredRawPreservesContainerAndExplicitStage(t *testing.T) {
	p := engineexport.Plan{Schema: engineexport.Schema, Engine: "oie", Version: "4.6.0", Format: "message-xml", Terminator: "cr"}
	got, err := engineexport.Extract(p, []byte(structured))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Stage != "raw" || got[0].Correlation != "unknown" || !bytes.Equal(got[0].Payload, []byte("MSH|^~\\&|S|F|R|F|20260101||ADT^A01|one|P|2.5.1\rPID|1")) {
		t.Fatalf("unexpected extraction: %#v", got)
	}
	if got[0].Offset != 0 || got[0].Size != len(structured) {
		t.Fatal("record offset lost")
	}
}

func TestStructuredExportRefusesUnsupportedWithoutReturningPartialRecords(t *testing.T) {
	p := engineexport.Plan{Schema: engineexport.Schema, Engine: "oie", Version: "4.6.0", Format: "message-xml", Terminator: "cr"}
	for _, data := range []string{
		`<channel><id>patient-secret</id></channel>`,
		strings.Replace(structured, "<encrypted>false</encrypted>", "<encrypted>true</encrypted>", 1),
		strings.Replace(structured, "<contentType>RAW</contentType>", "", 1),
		strings.Replace(structured, "<int>0</int>", "<int>1</int>", 1),
		strings.Replace(structured, "&#13;", "\r", 1),
		strings.Replace(structured, "<raw>", "<sent/> <raw>", 1),
		structured + `<message>`,
		strings.Replace(structured, "<content>", `<content reference="../raw">`, 1),
		`<!DOCTYPE message [<!ENTITY secret SYSTEM "file:///etc/passwd">]>` + structured,
	} {
		if got, err := engineexport.Extract(p, []byte(data)); err == nil || got != nil {
			t.Fatal("accepted unsupported or partial export")
		}
	}
}
func TestExportPlanIsStrictAndRawFallbackDoesNotInferStage(t *testing.T) {
	document := `{"schema":"readmit-engine-export/v1","engine":"mirth","version":"4.5.2","format":"raw","terminator":"cr"}`
	p, err := engineexport.Decode([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{'M', 'S', 'H', '|', 0xff, '\r', '\n'}
	records, err := engineexport.Extract(p, raw)
	if err != nil || !bytes.Equal(records[0].Payload, raw) || records[0].Stage != "unknown" {
		t.Fatal("raw fallback changed evidence")
	}
	for _, bad := range []string{strings.Replace(document, `"raw"`, `"zip"`, 1), strings.Replace(document, `"4.5.2"`, `"4.6.0"`, 1), strings.Replace(document, `"cr"`, `null`, 1), strings.Replace(document, `"engine":`, `"surprise":1,"engine":`, 1)} {
		if _, err := engineexport.Decode([]byte(bad)); err == nil {
			t.Fatal("accepted invalid plan")
		}
	}
}

func TestEngineExportLimitsAndMissingDeclarations(t *testing.T) {
	p := engineexport.Plan{Schema: engineexport.Schema, Engine: "oie", Version: "4.6.0", Format: "message-xml", Terminator: "cr"}
	for _, data := range []string{strings.Repeat(structured, 129), strings.Repeat("<message>", 33) + strings.Repeat("</message>", 33)} {
		if _, err := engineexport.Extract(p, []byte(data)); err == nil {
			t.Fatal("unbounded XML accepted")
		}
	}
	for _, member := range []string{"schema", "engine", "version", "format", "terminator"} {
		document := map[string]any{"schema": engineexport.Schema, "engine": "oie", "version": "4.6.0", "format": "raw", "terminator": "cr"}
		delete(document, member)
		data, _ := json.Marshal(document)
		if _, err := engineexport.Decode(data); err == nil {
			t.Fatalf("missing %s accepted", member)
		}
	}
}
