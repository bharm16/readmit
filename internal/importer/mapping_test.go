package importer_test

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/importer"
)

// recipe reads one recipe document and fails the test when it is not valid, so
// a behaviour test never silently exercises a recipe the reader would refuse.
func recipe(t *testing.T, document string) importer.Recipe {
	t.Helper()
	read, err := importer.DecodeRecipe([]byte(document))
	if err != nil {
		t.Fatalf("test recipe is not valid: %v", err)
	}
	return read
}

func mapped(t *testing.T, document string, files []string) *importer.Extraction {
	t.Helper()
	e, err := importer.ExtractMapped(t.Context(), recipe(t, document), files, nil, nil)
	if err != nil {
		t.Fatalf("mapped extraction: %v", err)
	}
	return e
}

// csvRow quotes one CSV field. The message it quotes carries carriage returns
// and is written into the member exactly as it is, so the test can assert that
// the payload the import stores is the member's own bytes.
func csvRow(fields ...string) string {
	quoted := make([]string, len(fields))
	for i, field := range fields {
		quoted[i] = `"` + strings.ReplaceAll(field, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ",")
}

func csvMember(t *testing.T, rows ...string) string {
	t.Helper()
	return write(t, t.TempDir(), "export.csv", strings.Join(rows, "\r\n")+"\r\n")
}

func TestCSVRecipeMapsEveryDeclarationAndKeepsThePayloadBytes(t *testing.T) {
	first, second := message("AAA"), message("BBB")
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt-inbound", "EPIC", first),
		csvRow("2026-01-02T05:04:05+02:00", "OUT", "adt-outbound", "LAB", second),
	)
	e := mapped(t, csvRecipe, []string{member})
	if e.Totals.Sources != 2 || e.UnmappedRecords != 0 || len(e.Mappings) != 2 {
		t.Fatalf("totals: %+v unmapped %d mappings %d", e.Totals, e.UnmappedRecords, len(e.Mappings))
	}
	if string(e.Inputs[0].Data) != first || string(e.Inputs[1].Data) != second {
		t.Fatal("a stored payload is not the member's own bytes")
	}
	want := []importer.Mapping{
		{SourceID: "s0001", State: importer.Mapped, PayloadSize: len(first), Source: "EPIC", Direction: bundle.Inbound, Channel: "adt-inbound"},
		{SourceID: "s0002", State: importer.Mapped, PayloadSize: len(second), Source: "LAB", Direction: bundle.Outbound, Channel: "adt-outbound"},
	}
	// Both rows name the same instant in different offsets, so an observed
	// time is the instant the record stated and never a local reading.
	moment := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range want {
		want[i].ObservedAt = &moment
		if got := e.Mappings[i]; got.SourceID != want[i].SourceID || got.State != want[i].State ||
			got.PayloadSize != want[i].PayloadSize || got.Source != want[i].Source ||
			got.Direction != want[i].Direction || got.Channel != want[i].Channel ||
			got.ObservedAt == nil || !got.ObservedAt.Equal(moment) {
			t.Errorf("mapping %d:\n got %+v\nwant %+v", i, got, want[i])
		}
	}
	// The declared observation reaches the case bundle's own observation for
	// every occurrence of the source it was mapped from.
	if observation := e.Inputs[0].Observations[1]; observation.Direction != bundle.Inbound || observation.ObservedAt == nil || !observation.ObservedAt.Equal(moment) {
		t.Fatalf("the mapped observation did not reach the source: %+v", observation)
	}
}

func TestUnmappedRecordsAreRetainedWithAllOfTheirBytesAndNoProvenance(t *testing.T) {
	good := message("AAA")
	rows := []string{
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", good),
		csvRow("not a time", "IN", "adt", "EPIC", good),
		csvRow("2026-01-02T03:04:05Z", "SIDEWAYS", "adt", "EPIC", good),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", ""),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", good+message("BBB")),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt"),
	}
	member := csvMember(t, rows...)
	e := mapped(t, csvRecipe, []string{member})
	if e.Totals.Sources != 6 || e.UnmappedRecords != 5 {
		t.Fatalf("totals: %+v unmapped %d", e.Totals, e.UnmappedRecords)
	}
	reasons := []string{"", importer.ReasonTime, importer.ReasonDirection, importer.ReasonPayload, importer.ReasonFraming, importer.ReasonFields}
	for i, reason := range reasons {
		got := e.Mappings[i]
		if reason == "" {
			if got.State != importer.Mapped {
				t.Errorf("record %d: %+v", i, got)
			}
			continue
		}
		if got.State != importer.Unmapped || got.Reason != reason {
			t.Errorf("record %d reason:\n got %q\nwant %q", i, got.Reason, reason)
		}
		// Nothing is half read: an unmapped record declares no source, no
		// channel, no observed time, and an explicitly unknown direction.
		if got.ObservedAt != nil || got.Source != "" || got.Channel != "" || got.Direction != bundle.Unknown {
			t.Errorf("record %d kept a mapped value: %+v", i, got)
		}
		// The record's own bytes are retained, exactly as the member held them.
		stored, want := string(e.Inputs[i].Data), rows[i+1]
		if stored != want {
			t.Errorf("record %d retained %q, want %q", i, stored, want)
		}
	}
}

func TestQuotedRecordsAndMalformedRowsKeepEveryByteAccountedFor(t *testing.T) {
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		`2026-01-02T03:04:05Z,IN,adt,EP"IC,x`,
		`"2026-01-02T03:04:05Z,IN,adt,EPIC,x`,
	)
	e := mapped(t, csvRecipe, []string{member})
	if e.UnmappedRecords != 2 || e.Totals.Sources != 2 {
		t.Fatalf("totals: %+v unmapped %d", e.Totals, e.UnmappedRecords)
	}
	for i, got := range e.Mappings {
		if got.State != importer.Unmapped || got.Reason != importer.ReasonRecord {
			t.Errorf("record %d: %+v", i, got)
		}
	}
	// An unterminated quote leaves no boundary, so the rest of the member is
	// retained as one record rather than divided at a guess.
	raw, err := os.ReadFile(member)
	if err != nil {
		t.Fatal(err)
	}
	records := e.Containers[0].Members[0].Records
	if len(records) != 2 || records[1].Offset+records[1].Size != len(raw) {
		t.Fatalf("the remaining bytes were not retained: %+v", records)
	}
}

func TestStructuralDisagreementWithTheMemberIsRefused(t *testing.T) {
	good := message("AAA")
	for name, rows := range map[string][]string{
		"header is not the declared width": {csvRow("received", "flow", "channel", "interface"), csvRow("a", "b", "c", "d")},
		"header repeats a column":          {csvRow("received", "flow", "flow", "interface", "message"), csvRow("a", "b", "c", "d", good)},
		"header lacks a declared column":   {csvRow("received", "flow", "channel", "interface", "payload"), csvRow("a", "b", "c", "d", good)},
	} {
		if _, err := importer.ExtractMapped(t.Context(), recipe(t, csvRecipe), []string{csvMember(t, rows...)}, nil, nil); !errors.Is(err, importer.ErrDeclaredEnvelope) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A JSON or XML member that does not hold the declared record path is the
	// same disagreement: the recipe describes a member this is not.
	folder := t.TempDir()
	for name, document := range map[string]string{
		"json path absent": jsonRecipe,
		"xml path absent":  xmlRecipe,
	} {
		file := write(t, folder, strings.ReplaceAll(name, " ", "-"), `{"other":[]}`)
		if _, err := importer.ExtractMapped(t.Context(), recipe(t, document), []string{file}, nil, nil); !errors.Is(err, importer.ErrDeclaredEnvelope) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// jsonRecipe reads a strict JSON envelope whose payload is base64, which is how
// an envelope that cannot carry arbitrary bytes carries them unaltered.
const jsonRecipe = `{"schema":"readmit-mapping-recipe/v1","name":"engine-json","revision":1,` +
	`"envelope":"json","encoding":"utf-8","members":[".json"],"json":{"record_path":["events"]},` +
	`"payload":{"operator":"base64","locator":["message","bytes"],"framing":"raw","terminator":"cr"},` +
	`"observed_at":{"operator":"unix-milliseconds","locator":["at"]},` +
	`"source":{"operator":"declared","declared":"engine-export"},` +
	`"direction":{"operator":"declared","declared":"outbound"},` +
	`"channel":{"operator":"field","locator":["channel"]}}`

func TestJSONRecipeReadsNestedLocatorsAndDecodesTheDeclaredPayload(t *testing.T) {
	first := message("AAA")
	document := `{"exported":true,"events":[` +
		`{"at":1767322845000,"channel":"adt","message":{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(first)) + `"}},` +
		`{"at":1767322845000,"channel":"adt","message":{"bytes":"not base64 at all"}},` +
		`{"at":1767322845000,"channel":"adt"},` +
		`"a record that is not an object"` +
		`]}`
	e := mapped(t, jsonRecipe, []string{write(t, t.TempDir(), "export.json", document)})
	if e.Totals.Sources != 4 || e.UnmappedRecords != 3 {
		t.Fatalf("totals: %+v unmapped %d", e.Totals, e.UnmappedRecords)
	}
	if string(e.Inputs[0].Data) != first {
		t.Fatalf("the decoded payload is not the declared bytes: %q", e.Inputs[0].Data)
	}
	moment := time.Date(2026, 1, 2, 3, 0, 45, 0, time.UTC)
	if got := e.Mappings[0]; got.State != importer.Mapped || got.Source != "engine-export" ||
		got.Direction != bundle.Outbound || got.Channel != "adt" || got.ObservedAt == nil || !got.ObservedAt.Equal(moment) {
		t.Fatalf("mapped record: %+v", got)
	}
	for i, reason := range []string{importer.ReasonPayload, importer.ReasonMissing, importer.ReasonFields} {
		if got := e.Mappings[i+1]; got.State != importer.Unmapped || got.Reason != reason {
			t.Errorf("record %d:\n got %q\nwant %q", i+1, got.Reason, reason)
		}
	}
}

func TestJSONRemainderThatNoLongerDividesIsRetained(t *testing.T) {
	document := `{"events":[{"channel":"adt","at":0,"message":{"bytes":"TVNI"}}, @nonsense]}`
	e := mapped(t, jsonRecipe, []string{write(t, t.TempDir(), "export.json", document)})
	if len(e.Mappings) != 2 || e.Mappings[1].Reason != importer.ReasonRemainder {
		t.Fatalf("mappings: %+v", e.Mappings)
	}
	records := e.Containers[0].Members[0].Records
	if records[1].Offset+records[1].Size != len(document) {
		t.Fatalf("the remaining bytes were not retained: %+v", records)
	}
}

// xmlRecipe reads an XML envelope whose payload is character data. XML
// normalizes line endings, so a payload carrying a carriage return states it as
// a character reference and readmit refuses a raw one.
const xmlRecipe = `{"schema":"readmit-mapping-recipe/v1","name":"engine-xml","revision":1,` +
	`"envelope":"xml","encoding":"utf-8","members":[".xml"],"xml":{"record_path":["corpus","message"]},` +
	`"payload":{"operator":"verbatim","locator":["body"],"framing":"raw","terminator":"cr"},` +
	`"observed_at":{"operator":"hl7-dtm","locator":["when"]},` +
	`"source":{"operator":"unknown"},` +
	`"direction":{"operator":"field","locator":["flow"],"values":[{"envelope":"I","mapped":"inbound"}]},` +
	`"channel":{"operator":"unknown"}}`

// xmlMarkup escapes the characters XML reserves. xmlEscaped also states a
// carriage return as a character reference, which is the only way XML carries
// one losslessly; xmlMarkup alone leaves it raw, which is well formed XML that
// a conforming reader would silently read as a line feed.
func xmlMarkup(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}

func xmlEscaped(value string) string {
	return strings.ReplaceAll(xmlMarkup(value), "\r", "&#13;")
}

func TestXMLRecipeReadsCharacterDataFromTheMemberRatherThanTheDecoder(t *testing.T) {
	first := message("AAA")
	document := `<corpus>` +
		`<message><when>20260102030405-0500</when><flow>I</flow><body>` + xmlEscaped(first) + `</body></message>` +
		`<message><when>20260102030405-0500</when><flow>I</flow><body>` + xmlMarkup(first) + `</body></message>` +
		`<message><when>20260102030405</when><flow>I</flow><body>` + xmlEscaped(first) + `</body></message>` +
		`<message><when>20260102030405-0500</when><flow>I</flow><body><wrapped>x</wrapped></body></message>` +
		`</corpus>`
	e := mapped(t, xmlRecipe, []string{write(t, t.TempDir(), "export.xml", document)})
	if e.Totals.Sources != 4 || e.UnmappedRecords != 3 {
		t.Fatalf("totals: %+v unmapped %d", e.Totals, e.UnmappedRecords)
	}
	if string(e.Inputs[0].Data) != first {
		t.Fatalf("a character reference did not read back as its own byte: %q", e.Inputs[0].Data)
	}
	moment := time.Date(2026, 1, 2, 8, 4, 5, 0, time.UTC)
	if got := e.Mappings[0]; got.ObservedAt == nil || !got.ObservedAt.Equal(moment) || got.Direction != bundle.Inbound ||
		got.Source != "" || got.Channel != "" {
		t.Fatalf("mapped record: %+v", got)
	}
	// A raw carriage return, a date/time with no declared offset, and an
	// element holding child elements are each refused rather than read as
	// something a conforming reader would read differently.
	for i, reason := range []string{importer.ReasonContent, importer.ReasonTime, importer.ReasonContent} {
		if got := e.Mappings[i+1]; got.State != importer.Unmapped || got.Reason != reason {
			t.Errorf("record %d:\n got %q\nwant %q", i+1, got.Reason, reason)
		}
	}
}

func TestXMLRemainderIsRetainedFromTheRecordTheReaderStoppedInside(t *testing.T) {
	document := `<corpus>` +
		`<message><when>20260102030405-0500</when><flow>I</flow><body>` + xmlEscaped(message("AAA")) + `</body></message>` +
		`<message><when>20260102030405-0500</when><flow>I</flow><body>not & well formed</body></message>` +
		`</corpus>`
	e := mapped(t, xmlRecipe, []string{write(t, t.TempDir(), "export.xml", document)})
	if len(e.Mappings) != 2 || e.Mappings[1].Reason != importer.ReasonRemainder {
		t.Fatalf("mappings: %+v", e.Mappings)
	}
	// The reader stopped inside the second record, so the retained bytes begin
	// at that record's own first byte and run to the end of the member.
	records := e.Containers[0].Members[0].Records
	start := strings.Index(document, `<message><when>20260102030405-0500</when><flow>I</flow><body>not`)
	if records[1].Offset != start || records[1].Offset+records[1].Size != len(document) {
		t.Fatalf("the remaining bytes were not retained whole: %+v of %d, want %d", records[1], len(document), start)
	}
	if string(e.Inputs[1].Data) != document[start:] {
		t.Fatal("the retained record is not the member's own bytes")
	}
}

const textRecipe = `{"schema":"readmit-mapping-recipe/v1","name":"engine-log","revision":1,` +
	`"envelope":"text","encoding":"us-ascii","members":[".log"],` +
	`"text":{"field_separator":"|","record_separator":"lf","fields":4},` +
	`"payload":{"operator":"base64","locator":["4"],"framing":"mllp","terminator":"cr"},` +
	`"observed_at":{"operator":"unix-seconds","locator":["1"]},` +
	`"source":{"operator":"field","locator":["3"]},` +
	`"direction":{"operator":"declared","declared":"inbound"},` +
	`"channel":{"operator":"unknown"}}`

func TestTextLogRecipeReadsIndexedColumnsAndAnMLLPPayload(t *testing.T) {
	framedMessage := framed("AAA")
	encoded := base64.StdEncoding.EncodeToString([]byte(framedMessage))
	member := write(t, t.TempDir(), "engine.log", strings.Join([]string{
		"1767322845|info|EPIC|" + encoded,
		"1767322846|info|EPIC|" + base64.StdEncoding.EncodeToString([]byte(message("BBB"))),
		"1767322847|info|EPIC",
		"",
	}, "\n"))
	e := mapped(t, textRecipe, []string{member})
	if e.Totals.Sources != 3 || e.UnmappedRecords != 2 {
		t.Fatalf("totals: %+v unmapped %d", e.Totals, e.UnmappedRecords)
	}
	if string(e.Inputs[0].Data) != framedMessage {
		t.Fatalf("the decoded payload is not the declared bytes: %q", e.Inputs[0].Data)
	}
	moment := time.Date(2026, 1, 2, 3, 0, 45, 0, time.UTC)
	if got := e.Mappings[0]; got.State != importer.Mapped || got.Source != "EPIC" ||
		got.Direction != bundle.Inbound || got.ObservedAt == nil || !got.ObservedAt.Equal(moment) {
		t.Fatalf("mapped record: %+v", got)
	}
	// A payload that is not MLLP framed contradicts the declaration, and a
	// record that is not the declared shape does not hold the fields at all.
	for i, reason := range []string{importer.ReasonFraming, importer.ReasonFields} {
		if got := e.Mappings[i+1]; got.State != importer.Unmapped || got.Reason != reason {
			t.Errorf("record %d:\n got %q\nwant %q", i+1, got.Reason, reason)
		}
	}
}

func TestEveryTimeOperatorRequiresTheValueToCarryItsOwnOffset(t *testing.T) {
	good := message("AAA")
	for operator, cases := range map[string]map[string]bool{
		"rfc3339": {
			"2026-01-02T03:04:05Z":      true,
			"2026-01-02T03:04:05+02:00": true,
			"2026-01-02T03:04:05":       false,
			"2026-01-02":                false,
			"0001-01-01T00:00:00Z":      false,
		},
		"unix-seconds": {
			"1767322845":     true,
			"-62135596801":   false,
			"253402300800":   false,
			"1767322845.000": false,
		},
		"unix-milliseconds": {"1767322845000": true, "99999999999999999": false},
		"hl7-dtm": {
			"20260102030405-0500":      true,
			"202601020304-0500":        true,
			"20260102030405.1234-0500": true,
			"20260102030405":           false,
			"202601020304.5-0500":      false,
			"2026010203040500":         false,
			"20261302030405-0500":      false,
		},
	} {
		document := strings.Replace(csvRecipe, `"observed_at":{"operator":"rfc3339"`, `"observed_at":{"operator":"`+operator+`"`, 1)
		for value, readable := range cases {
			e := mapped(t, document, []string{csvMember(t,
				csvRow("received", "flow", "channel", "interface", "message"),
				csvRow(value, "IN", "adt", "EPIC", good))})
			if resolved := e.Mappings[0].State == importer.Mapped; resolved != readable {
				t.Errorf("%s %q: resolved=%v, want %v (%s)", operator, value, resolved, readable, e.Mappings[0].Reason)
			}
		}
	}
}

func TestMappedImportWritesTheCaseItsReceiptDescribes(t *testing.T) {
	first := message("AAA")
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", first),
		csvRow("2026-01-02T03:04:06Z", "SIDEWAYS", "adt", "EPIC", first),
	)
	read := recipe(t, csvRecipe)
	e := mapped(t, csvRecipe, []string{member})
	importedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	written, err := bundle.Write(filepath.Join(t.TempDir(), "incident.case"), e.Inputs,
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := importer.NewMappingReceipt(read, e, importedAt, written)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := read.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != importer.MappingReceiptSchema || receipt.Identity != identity ||
		receipt.Case.Identity != written.Identity || receipt.UnmappedRecords != 1 || len(receipt.Mappings) != 2 {
		t.Fatalf("receipt: %+v", receipt)
	}
	// The record the recipe could not map is in the case with its own bytes,
	// and the case's own reader reports it as retained rather than parsed.
	if len(receipt.Quarantined) != 1 || receipt.Quarantined[0].SourceID != "s0002" {
		t.Fatalf("quarantined: %+v", receipt.Quarantined)
	}
	encoded, err := importer.EncodeMappingReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SYNTH-") || strings.Contains(string(encoded), "ADT^A08") {
		t.Fatal("the receipt carried message evidence")
	}
	var again importer.MappingReceipt
	if err := json.Unmarshal(encoded, &again, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the receipt is not a strict document: %v", err)
	}
	repeated, err := importer.EncodeMappingReceipt(receipt)
	if err != nil || string(repeated) != string(encoded) {
		t.Fatal("the receipt is not encoded deterministically")
	}
}

func TestAMappingReceiptCannotDescribeEvidenceItDidNotExtract(t *testing.T) {
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("AAA")),
	)
	read := recipe(t, csvRecipe)
	e := mapped(t, csvRecipe, []string{member})
	importedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	other := mapped(t, csvRecipe, []string{csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("BBB")),
	)})
	written, err := bundle.Write(filepath.Join(t.TempDir(), "other.case"), other.Inputs,
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.NewMappingReceipt(read, e, importedAt, written); err == nil {
		t.Fatal("a receipt described a case holding other evidence")
	}
	if _, err := importer.EncodeMappingReceipt(importer.MappingReceipt{Schema: importer.MappingReceiptSchema}); err == nil {
		t.Fatal("a receipt naming no case was encoded")
	}
}

func TestAMemberIsRefusedRatherThanDividedPastTheSourceLimit(t *testing.T) {
	rows := []string{csvRow("received", "flow", "channel", "interface", "message")}
	for i := range importer.MaxEnvelopeRecords + 1 {
		rows = append(rows, csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("A"+strconv.Itoa(i))))
	}
	_, err := importer.ExtractMapped(t.Context(), recipe(t, csvRecipe), []string{csvMember(t, rows...)}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "envelope records") {
		t.Fatalf("an oversized member was not refused by name: %v", err)
	}
}

func TestAMappedImportFiltersMembersAndChecksTheDeclaredEncoding(t *testing.T) {
	folder := t.TempDir()
	write(t, folder, "export.csv", csvRow("received", "flow", "channel", "interface", "message")+"\r\n"+
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("AAA"))+"\r\n")
	write(t, folder, "notes.md", "operator notes, not evidence")
	e, err := importer.ExtractMapped(t.Context(), recipe(t, csvRecipe), nil, []string{folder}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Totals.Members != 2 || e.Totals.Excluded != 1 || e.Totals.Sources != 1 {
		t.Fatalf("totals: %+v", e.Totals)
	}
	if e.Containers[0].Members[1].Reason != importer.ReasonSuffix {
		t.Fatalf("exclusion reason: %+v", e.Containers[0].Members[1])
	}
	// The encoding declaration is checked exactly as an import plan's is: a
	// member that cannot be what the recipe says is refused, never transcoded.
	invalid := write(t, t.TempDir(), "bad.csv", csvRow("received", "flow", "channel", "interface", "message")+"\r\n"+
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", "\xff\xfe")+"\r\n")
	if _, err := importer.ExtractMapped(t.Context(), recipe(t, csvRecipe), []string{invalid}, nil, nil); !errors.Is(err, importer.ErrDeclaredEncoding) {
		t.Fatalf("declared encoding: %v", err)
	}
}

func TestAMappedPreviewCarriesTheRecipeAndCreatesNothing(t *testing.T) {
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("AAA")),
	)
	read := recipe(t, csvRecipe)
	preview, err := importer.NewMappingPreview(read, mapped(t, csvRecipe, []string{member}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := importer.EncodeMappingPreview(preview)
	if err != nil {
		t.Fatal(err)
	}
	var again importer.MappingPreview
	if err := json.Unmarshal(encoded, &again, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the preview is not a strict document: %v", err)
	}
	if again.Schema != importer.MappingPreviewSchema || again.Recipe.Name != read.Name || again.Identity != preview.Identity {
		t.Fatalf("preview: %+v", again)
	}
	if strings.Contains(string(encoded), "SYNTH-") || strings.Contains(string(encoded), "ADT^A08") {
		t.Fatal("the preview carried message evidence")
	}
	if _, err := importer.EncodeMappingPreview(importer.MappingPreview{}); !errors.Is(err, importer.ErrUnsupportedRecipe) {
		t.Fatal("a preview under no version was encoded")
	}
}

// TestARetainedRecordIsEvidenceRatherThanAVerdict pins what retaining a record
// does and does not do. Its bytes go into the case unchanged and nothing is
// appended to mark it, so a record whose bytes are a message parses like any
// other occurrence: a case is evidence, not a judgement. What the record never
// gains is provenance, and the receipt beside it is the only thing that says
// the recipe did not map it.
func TestARetainedRecordIsEvidenceRatherThanAVerdict(t *testing.T) {
	// One field holding the whole record, read both as the payload and as the
	// observed time. The time cannot read, so the record is retained with bytes
	// that are still exactly the message.
	document := strings.NewReplacer(
		`"envelope":"csv"`, `"envelope":"text"`,
		`"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":5}`,
		`"text":{"field_separator":"|","record_separator":"lf","fields":1}`,
		`"locator":["message"]`, `"locator":["1"]`,
		`"locator":["received"]`, `"locator":["1"]`,
		`"source":{"operator":"field","locator":["interface"]}`, `"source":{"operator":"unknown"}`,
		`"direction":{"operator":"field","locator":["flow"],"values":[{"envelope":"IN","mapped":"inbound"},{"envelope":"OUT","mapped":"outbound"}]}`,
		`"direction":{"operator":"declared","declared":"inbound"}`,
		`"channel":{"operator":"field","locator":["channel"]}`, `"channel":{"operator":"unknown"}`,
	).Replace(csvRecipe)
	e := mapped(t, document, []string{write(t, t.TempDir(), "engine.log", message("AAA"))})
	if len(e.Mappings) != 1 || e.Mappings[0].State != importer.Unmapped || e.Mappings[0].Reason != importer.ReasonTime {
		t.Fatalf("mappings: %+v", e.Mappings)
	}
	if string(e.Inputs[0].Data) != message("AAA") {
		t.Fatalf("the retained record is not the member's own bytes: %q", e.Inputs[0].Data)
	}
	importedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	written, err := bundle.Write(filepath.Join(t.TempDir(), "incident.case"), e.Inputs,
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
	if err != nil {
		t.Fatal(err)
	}
	// The bytes are a message, so the case reports an ordinary occurrence.
	// That is the case telling the truth about the bytes, not a mapping that
	// passed: the occurrence carries no observed time and no direction.
	if len(written.Events) != 1 || written.Events[0].Kind == bundle.Unparsed {
		t.Fatalf("this test needs a retained record the parser reads: %+v", written.Events)
	}
	if written.Events[0].Direction != bundle.Unknown || written.Events[0].ObservedAt != nil {
		t.Fatalf("a retained record gained provenance: %+v", written.Events[0])
	}
	receipt, err := importer.NewMappingReceipt(recipe(t, document), e, importedAt, written)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.UnmappedRecords != 1 || len(receipt.Quarantined) != 0 ||
		receipt.Mappings[0].SourceID != written.Manifest.Sources[0].ID {
		t.Fatalf("the receipt does not say which source was not mapped: %+v", receipt)
	}
}

// TestExtendingTheRecipeMapsARecordThatWasRetained is the recovery an unmapped
// record asks for: the operator adds the declaration the reason named and runs
// the same import again.
func TestExtendingTheRecipeMapsARecordThatWasRetained(t *testing.T) {
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "BIDIRECTIONAL", "adt", "EPIC", message("AAA")),
	)
	before := mapped(t, csvRecipe, []string{member})
	if before.UnmappedRecords != 1 || before.Mappings[0].Reason != importer.ReasonDirection {
		t.Fatalf("mappings: %+v", before.Mappings)
	}
	extended := strings.Replace(csvRecipe, `{"envelope":"IN","mapped":"inbound"}`,
		`{"envelope":"IN","mapped":"inbound"},{"envelope":"BIDIRECTIONAL","mapped":"unknown"}`, 1)
	after := mapped(t, extended, []string{member})
	if after.UnmappedRecords != 0 || after.Mappings[0].State != importer.Mapped ||
		after.Mappings[0].Direction != bundle.Unknown || after.Mappings[0].Channel != "adt" {
		t.Fatalf("the extended recipe did not map the record: %+v", after.Mappings)
	}
	// Extending the recipe is a new revision of it, and its identity says so.
	first, err := recipe(t, csvRecipe).Identity()
	if err != nil {
		t.Fatal(err)
	}
	second, err := recipe(t, extended).Identity()
	if err != nil || first == second {
		t.Fatalf("an extended recipe kept the old identity: %q %v", second, err)
	}
}

func TestACancelledMappedImportStopsBeforeReadingAndWritesNothing(t *testing.T) {
	member := csvMember(t,
		csvRow("received", "flow", "channel", "interface", "message"),
		csvRow("2026-01-02T03:04:05Z", "IN", "adt", "EPIC", message("AAA")),
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := importer.ExtractMapped(ctx, recipe(t, csvRecipe), []string{member}, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("a cancelled mapped import was not refused: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(member))
	if err != nil || len(entries) != 1 {
		t.Fatalf("a cancelled mapped import changed the filesystem: %v %d", err, len(entries))
	}
}

func TestAMappedExtractionRefusesAnInvalidRecipeAndAnEmptyDeclaration(t *testing.T) {
	if _, err := importer.ExtractMapped(t.Context(), importer.Recipe{}, []string{"x"}, nil, nil); !errors.Is(err, importer.ErrUnsupportedRecipe) {
		t.Fatal("an unversioned recipe was extracted with")
	}
	if _, err := importer.ExtractMapped(t.Context(), recipe(t, csvRecipe), nil, nil, nil); err == nil {
		t.Fatal("an import declaring no container was accepted")
	}
}
