package importer_test

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/importer"
)

// csvRecipe is one complete mapping recipe. Every negative case below changes
// exactly one declaration of it, so what each case tests is the difference.
const csvRecipe = `{"schema":"readmit-mapping-recipe/v1","name":"engine-csv-export","revision":3,` +
	`"envelope":"csv","encoding":"utf-8","members":[".csv"],` +
	`"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":5},` +
	`"payload":{"operator":"verbatim","locator":["message"],"framing":"raw","terminator":"cr"},` +
	`"observed_at":{"operator":"rfc3339","locator":["received"]},` +
	`"source":{"operator":"field","locator":["interface"]},` +
	`"direction":{"operator":"field","locator":["flow"],"values":[{"envelope":"IN","mapped":"inbound"},{"envelope":"OUT","mapped":"outbound"}]},` +
	`"channel":{"operator":"field","locator":["channel"]}}`

// csvRecipeReordered declares exactly what csvRecipe declares, written with
// its members in another order and spaced differently.
const csvRecipeReordered = `{
  "channel": {"operator": "field", "locator": ["channel"]},
  "revision": 3,
  "envelope": "csv",
  "payload": {"framing": "raw", "terminator": "cr", "operator": "verbatim", "locator": ["message"]},
  "schema": "readmit-mapping-recipe/v1",
  "csv": {"fields": 5, "header": "present", "delimiter": ",", "record_separator": "crlf"},
  "members": [".csv"],
  "direction": {"locator": ["flow"], "operator": "field",
    "values": [{"mapped": "inbound", "envelope": "IN"}, {"mapped": "outbound", "envelope": "OUT"}]},
  "name": "engine-csv-export",
  "source": {"operator": "field", "locator": ["interface"]},
  "encoding": "utf-8",
  "observed_at": {"operator": "rfc3339", "locator": ["received"]}
}`

// vary replaces one declaration of the complete recipe and fails the test when
// the replacement changed nothing, so a negative case cannot silently pass by
// testing the complete document again.
func vary(t *testing.T, from, to string) string {
	t.Helper()
	varied := strings.Replace(csvRecipe, from, to, 1)
	if varied == csvRecipe {
		t.Fatalf("the recipe does not hold %q", from)
	}
	return varied
}

func TestDecodeRecipeRequiresEveryDeclaration(t *testing.T) {
	recipe, err := importer.DecodeRecipe([]byte(csvRecipe))
	if err != nil {
		t.Fatalf("complete recipe: %v", err)
	}
	if recipe.Name != "engine-csv-export" || recipe.Revision != 3 || recipe.Envelope != importer.CSVEnvelope {
		t.Fatalf("recipe lost a declaration: %+v", recipe)
	}
	if recipe.CSV == nil || recipe.CSV.Fields != 5 || recipe.CSV.Header != importer.HeaderPresent {
		t.Fatalf("recipe lost its dialect: %+v", recipe.CSV)
	}
	if recipe.Payload.Operator != importer.VerbatimPayload || recipe.ObservedAt.Operator != importer.RFC3339Time ||
		recipe.Source.Operator != importer.FieldLabel || recipe.Direction.Operator != importer.FieldDirection ||
		recipe.Channel.Operator != importer.FieldLabel {
		t.Fatalf("recipe lost an operator: %+v", recipe)
	}

	for name, document := range map[string]string{
		"missing name":            vary(t, `"name":"engine-csv-export",`, ``),
		"missing revision":        vary(t, `"revision":3,`, ``),
		"revision below one":      vary(t, `"revision":3`, `"revision":0`),
		"missing envelope":        vary(t, `"envelope":"csv",`, ``),
		"missing encoding":        vary(t, `"encoding":"utf-8",`, ``),
		"missing members":         vary(t, `"members":[".csv"],`, ``),
		"unknown member":          vary(t, `"members":[".csv"],`, `"members":[".csv"],"detect":true,`),
		"no dialect":              vary(t, `"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":5},`, ``),
		"two dialects":            vary(t, `"payload":`, `"text":{"field_separator":"|","record_separator":"lf","fields":2},"payload":`),
		"dialect is not envelope": vary(t, `"envelope":"csv"`, `"envelope":"text"`),
		"unknown envelope":        vary(t, `"envelope":"csv"`, `"envelope":"parquet"`),
		"carriage record end":     vary(t, `"record_separator":"crlf"`, `"record_separator":"cr"`),
		"quote delimiter":         vary(t, `"delimiter":","`, `"delimiter":"\""`),
		"two byte delimiter":      vary(t, `"delimiter":","`, `"delimiter":"||"`),
		"field count of zero":     vary(t, `"fields":5`, `"fields":0`),
		"automatic header":        vary(t, `"header":"present"`, `"header":"auto"`),
		"batch payload framing":   vary(t, `"framing":"raw"`, `"framing":"batch"`),
		"automatic terminator":    vary(t, `"terminator":"cr"`, `"terminator":"auto"`),
		"unknown payload encode":  vary(t, `"operator":"verbatim"`, `"operator":"gzip"`),
		"payload without locator": vary(t, `"locator":["message"],`, ``),
		"unknown time operator":   vary(t, `"operator":"rfc3339"`, `"operator":"local-time"`),
		"unknown time with field": vary(t, `"observed_at":{"operator":"rfc3339","locator":["received"]}`, `"observed_at":{"operator":"unknown","locator":["received"]}`),
		"declared and located":    vary(t, `"source":{"operator":"field","locator":["interface"]}`, `"source":{"operator":"declared","declared":"epic","locator":["interface"]}`),
		"blank declared label":    vary(t, `"source":{"operator":"field","locator":["interface"]}`, `"source":{"operator":"declared","declared":""}`),
		"declared direction too":  vary(t, `"direction":{"operator":"field"`, `"direction":{"operator":"field","declared":"inbound"`),
		"empty direction table":   vary(t, `"values":[{"envelope":"IN","mapped":"inbound"},{"envelope":"OUT","mapped":"outbound"}]`, `"values":[]`),
		"repeated direction key":  vary(t, `{"envelope":"OUT","mapped":"outbound"}`, `{"envelope":"IN","mapped":"outbound"}`),
		"unknown direction value": vary(t, `"mapped":"inbound"`, `"mapped":"both"`),
		"blank direction key":     vary(t, `"envelope":"IN"`, `"envelope":""`),
		"column past the count":   vary(t, `"header":"present"`, `"header":"absent"`),
		"deep flat locator":       vary(t, `"locator":["message"]`, `"locator":["a","b"]`),
	} {
		if _, err := importer.DecodeRecipe([]byte(document)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestFlatLocatorWithoutAHeaderNamesAColumnInsideTheRecord(t *testing.T) {
	indexed := strings.NewReplacer(
		`"header":"present"`, `"header":"absent"`,
		`"locator":["message"]`, `"locator":["5"]`,
		`"locator":["received"]`, `"locator":["1"]`,
		`"locator":["interface"]`, `"locator":["4"]`,
		`"locator":["flow"]`, `"locator":["2"]`,
		`"locator":["channel"]`, `"locator":["3"]`,
	).Replace(csvRecipe)
	if _, err := importer.DecodeRecipe([]byte(indexed)); err != nil {
		t.Fatalf("indexed locators: %v", err)
	}
	for name, column := range map[string]string{"past the count": `"6"`, "below one": `"0"`, "not a number": `"five"`} {
		if _, err := importer.DecodeRecipe([]byte(strings.Replace(indexed, `"locator":["5"]`, `"locator":`+column, 1))); err == nil {
			t.Errorf("a payload column %s was accepted", name)
		}
	}
}

func TestDocumentEnvelopesDeclareTheirRecordPath(t *testing.T) {
	base := `{"schema":"readmit-mapping-recipe/v1","name":"engine","revision":1,"envelope":"json","encoding":"utf-8","members":[".json"],` +
		`"json":{"record_path":["events"]},` +
		`"payload":{"operator":"base64","locator":["hl7"],"framing":"mllp","terminator":"cr"},` +
		`"observed_at":{"operator":"unknown"},"source":{"operator":"unknown"},` +
		`"direction":{"operator":"declared","declared":"unknown"},"channel":{"operator":"unknown"}}`
	if _, err := importer.DecodeRecipe([]byte(base)); err != nil {
		t.Fatalf("json recipe: %v", err)
	}
	// A JSON document may be the array itself, so an empty path is a real
	// declaration; an XML member always has a document element to name.
	if _, err := importer.DecodeRecipe([]byte(strings.Replace(base, `"record_path":["events"]`, `"record_path":[]`, 1))); err != nil {
		t.Fatalf("json root recipe: %v", err)
	}
	for name, document := range map[string]string{
		"absent json path":  strings.Replace(base, `"json":{"record_path":["events"]},`, `"json":{},`, 1),
		"null json path":    strings.Replace(base, `"record_path":["events"]`, `"record_path":null`, 1),
		"latin-1 json":      strings.Replace(base, `"encoding":"utf-8"`, `"encoding":"iso-8859-1"`, 1),
		"unknown json code": strings.Replace(base, `"encoding":"utf-8"`, `"encoding":"unknown"`, 1),
		"empty xml path": strings.NewReplacer(`"envelope":"json"`, `"envelope":"xml"`, `"json":{"record_path":["events"]}`, `"xml":{"record_path":[]}`).
			Replace(base),
	} {
		if _, err := importer.DecodeRecipe([]byte(document)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRecipeIdentityIsTheDeclarationNotTheDocumentBytes(t *testing.T) {
	recipe, err := importer.DecodeRecipe([]byte(csvRecipe))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := recipe.Identity()
	if err != nil || len(identity) != 64 {
		t.Fatalf("identity: %q %v", identity, err)
	}
	reordered, err := importer.DecodeRecipe([]byte(csvRecipeReordered))
	if err != nil {
		t.Fatal(err)
	}
	respaced, err := reordered.Identity()
	if err != nil || respaced != identity {
		t.Fatalf("reformatting the document changed the identity: %q %q", identity, respaced)
	}
	// A revision is part of the declaration, so publishing a new revision of a
	// recipe is always visible as a different identity in the receipt.
	revised, err := importer.DecodeRecipe([]byte(vary(t, `"revision":3`, `"revision":4`)))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision == recipe.Revision {
		t.Fatal("the revision did not change")
	}
	if again, err := revised.Identity(); err != nil || again == identity {
		t.Fatalf("a new revision kept the old identity: %q %v", again, err)
	}
}

func TestUnsupportedRecipeVersionIsReportedAsUnsupported(t *testing.T) {
	_, err := importer.DecodeRecipe([]byte(vary(t, importer.RecipeSchema, "readmit-mapping-recipe/v2")))
	if !errors.Is(err, importer.ErrUnsupportedRecipe) {
		t.Fatalf("a later version read as something else: %v", err)
	}
	if _, err := importer.DecodeRecipe([]byte(`{"name":"x","revision":1}`)); errors.Is(err, importer.ErrUnsupportedRecipe) {
		t.Fatal("a document with no version read as an unsupported version")
	}
	oversize := []byte(strings.Replace(csvRecipe, `"name":"engine-csv-export"`, `"name":"`+strings.Repeat("a", importer.MaxRecipeBytes)+`"`, 1))
	if _, err := importer.DecodeRecipe(oversize); err == nil {
		t.Fatal("an oversize recipe document was accepted")
	}
}

// FuzzMappingRecipe checks that whatever the reader accepts survives being
// recorded and read back, because a recipe is written verbatim into every
// preview and receipt an import produces.
func FuzzMappingRecipe(f *testing.F) {
	f.Add([]byte(csvRecipe))
	f.Add([]byte(`{"schema":"readmit-mapping-recipe/v1","name":"n","revision":1,"envelope":"text","encoding":"unknown","members":[],` +
		`"text":{"field_separator":"|","record_separator":"lf","fields":2},` +
		`"payload":{"operator":"base64","locator":["2"],"framing":"mllp","terminator":"lf"},` +
		`"observed_at":{"operator":"unknown"},"source":{"operator":"declared","declared":"s"},` +
		`"direction":{"operator":"declared","declared":"outbound"},"channel":{"operator":"unknown"}}`))
	f.Add([]byte(`{"schema":"readmit-mapping-recipe/v2"}`))
	f.Add([]byte(`{"schema":"readmit-mapping-recipe/v1","name":"n","revision":1,"envelope":"xml","encoding":"utf-8","members":[],"xml":{"record_path":["a","b"]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		recipe, err := importer.DecodeRecipe(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		declared := 0
		for _, present := range []bool{recipe.CSV != nil, recipe.Text != nil, recipe.JSON != nil, recipe.XML != nil} {
			if present {
				declared++
			}
		}
		if declared != 1 || recipe.Members == nil || recipe.Revision < 1 {
			t.Fatalf("an incomplete recipe was accepted: %+v", recipe)
		}
		if recipe.Payload.Framing == importer.BatchFraming || recipe.Payload.Terminator == "auto" {
			t.Fatalf("an undeclared payload framing was accepted: %+v", recipe.Payload)
		}
		identity, err := recipe.Identity()
		if err != nil {
			t.Fatal("accepted a recipe that cannot be recorded")
		}
		encoded, err := json.Marshal(recipe, json.Deterministic(true))
		if err != nil {
			t.Fatal("accepted a recipe that cannot be encoded")
		}
		again, err := importer.DecodeRecipe(encoded)
		if err != nil {
			t.Fatalf("a recorded recipe did not read back: %v", err)
		}
		repeated, err := again.Identity()
		if err != nil || repeated != identity {
			t.Fatalf("a recorded recipe read back as a different recipe: %q %q", identity, repeated)
		}
	})
}
