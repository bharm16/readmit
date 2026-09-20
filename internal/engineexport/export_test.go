package engineexport_test

import (
	"bytes"
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/engineexport"
	"strings"
	"testing"
)

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
