package importer

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// reasons are every reason an envelope reader may give. A fuzz target checks
// that nothing else reaches a receipt, because a reason is a fixed sentence and
// never a value read out of the evidence.
var reasons = []string{ReasonRecord, ReasonFields, ReasonMissing, ReasonContent, ReasonRemainder}

func TestCSVReaderKeepsBytesAndUndoesOnlyTheDialectsOwnEscape(t *testing.T) {
	// A quoted field may hold the record separator and any line ending, which
	// is what lets one CSV record carry a whole message; the reader keeps those
	// bytes exactly as the member wrote them rather than rewriting them to one
	// form the way a normalizing reader would.
	for name, testCase := range map[string]struct {
		member string
		fields []string
		ok     bool
	}{
		"plain":                    {"a,b,c", []string{"a", "b", "c"}, true},
		"quoted":                   {`"a","b","c"`, []string{"a", "b", "c"}, true},
		"quoted delimiter":         {`"a,b",c`, []string{"a,b", "c"}, true},
		"doubled quote":            {`"a""b",c`, []string{`a"b`, "c"}, true},
		"quoted line ending":       {"\"a\r\nb\",c", []string{"a\r\nb", "c"}, true},
		"quoted carriage return":   {"\"a\rb\",c", []string{"a\rb", "c"}, true},
		"empty fields":             {",,", []string{"", "", ""}, true},
		"bare quote":               {`a"b,c`, nil, false},
		"text after closing quote": {`"a"b,c`, nil, false},
		"unterminated quote":       {`"a,b,c`, nil, false},
	} {
		fields, _, _, ok := csvRecord([]byte(testCase.member), 0, ',', []byte{'\r', '\n'})
		if ok != testCase.ok {
			t.Errorf("%s: ok=%v, want %v", name, ok, testCase.ok)
			continue
		}
		if !ok {
			continue
		}
		read := make([]string, len(fields))
		for i, field := range fields {
			read[i] = string(field)
		}
		if !slices.Equal(read, testCase.fields) {
			t.Errorf("%s:\n got %q\nwant %q", name, read, testCase.fields)
		}
	}
}

func TestCSVRecordsAccountForEveryByteOfTheMember(t *testing.T) {
	member := []byte("h1,h2\r\na,b\r\nc\r\nd,e")
	dialect := CSVDialect{Delimiter: ",", RecordSeparator: CRLFSeparator, Header: HeaderPresent, Fields: 2}
	records, err := csvRecords(dialect, []Locator{{"h2"}}, member)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[1].reason != ReasonFields {
		t.Fatalf("records: %+v", records)
	}
	// The header is the member's own first record, and every record after it
	// begins where the previous one's bytes and separator ended.
	at := len("h1,h2\r\n")
	for i, record := range records {
		if record.offset != at {
			t.Fatalf("record %d begins at %d, want %d", i, record.offset, at)
		}
		at = record.offset + record.size + 2
	}
}

func TestXMLContentResolvesReferencesAndRefusesWhatNormalizationWouldChange(t *testing.T) {
	for name, testCase := range map[string]struct {
		raw   string
		value string
		ok    bool
	}{
		"plain text":           {"abc", "abc", true},
		"predefined entities":  {"a&amp;b&lt;c&gt;d&quot;e&apos;f", `a&b<c>d"e'f`, true},
		"decimal reference":    {"a&#13;b", "a\rb", true},
		"hexadecimal":          {"a&#x0D;b", "a\rb", true},
		"astral reference":     {"&#x1F600;", "\U0001F600", true},
		"cdata":                {"<![CDATA[a<b&c]]>", "a<b&c", true},
		"line feed kept":       {"a\nb", "a\nb", true},
		"raw carriage return":  {"a\rb", "", false},
		"carriage in cdata":    {"<![CDATA[a\rb]]>", "", false},
		"child element":        {"<b>x</b>", "", false},
		"unknown entity":       {"&nbsp;", "", false},
		"unterminated entity":  {"&amp", "", false},
		"unterminated cdata":   {"<![CDATA[abc", "", false},
		"forbidden code point": {"&#0;", "", false},
		"surrogate reference":  {"&#xD800;", "", false},
		"reference overflow":   {"&#x110000;", "", false},
	} {
		value, err := xmlContent([]byte(testCase.raw))
		if (err == nil) != testCase.ok {
			t.Errorf("%s: err=%v, want ok=%v", name, err, testCase.ok)
			continue
		}
		if err == nil && string(value) != testCase.value {
			t.Errorf("%s:\n got %q\nwant %q", name, value, testCase.value)
		}
	}
}

// fuzzRecipes are one valid recipe per envelope, so every reader is exercised
// over the same untrusted bytes.
func fuzzRecipes(t testing.TB) []Recipe {
	t.Helper()
	documents := []string{
		`{"schema":"readmit-mapping-recipe/v1","name":"csv","revision":1,"envelope":"csv","encoding":"unknown","members":[],` +
			`"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":3},` +
			`"payload":{"operator":"verbatim","locator":["m"],"framing":"raw","terminator":"cr"},` +
			`"observed_at":{"operator":"rfc3339","locator":["t"]},"source":{"operator":"field","locator":["s"]},` +
			`"direction":{"operator":"declared","declared":"inbound"},"channel":{"operator":"unknown"}}`,
		`{"schema":"readmit-mapping-recipe/v1","name":"csv-indexed","revision":1,"envelope":"csv","encoding":"unknown","members":[],` +
			`"csv":{"delimiter":";","record_separator":"lf","header":"absent","fields":2},` +
			`"payload":{"operator":"base64","locator":["2"],"framing":"mllp","terminator":"lf"},` +
			`"observed_at":{"operator":"unix-seconds","locator":["1"]},"source":{"operator":"unknown"},` +
			`"direction":{"operator":"field","locator":["1"],"values":[{"envelope":"i","mapped":"inbound"}]},"channel":{"operator":"unknown"}}`,
		`{"schema":"readmit-mapping-recipe/v1","name":"text","revision":1,"envelope":"text","encoding":"unknown","members":[],` +
			`"text":{"field_separator":"|","record_separator":"lf","fields":3},` +
			`"payload":{"operator":"verbatim","locator":["3"],"framing":"raw","terminator":"cr"},` +
			`"observed_at":{"operator":"hl7-dtm","locator":["1"]},"source":{"operator":"field","locator":["2"]},` +
			`"direction":{"operator":"declared","declared":"unknown"},"channel":{"operator":"unknown"}}`,
		`{"schema":"readmit-mapping-recipe/v1","name":"json","revision":1,"envelope":"json","encoding":"utf-8","members":[],` +
			`"json":{"record_path":["events"]},` +
			`"payload":{"operator":"verbatim","locator":["m","b"],"framing":"raw","terminator":"cr"},` +
			`"observed_at":{"operator":"unix-milliseconds","locator":["t"]},"source":{"operator":"unknown"},` +
			`"direction":{"operator":"declared","declared":"outbound"},"channel":{"operator":"field","locator":["c"]}}`,
		`{"schema":"readmit-mapping-recipe/v1","name":"xml","revision":1,"envelope":"xml","encoding":"utf-8","members":[],` +
			`"xml":{"record_path":["corpus","message"]},` +
			`"payload":{"operator":"verbatim","locator":["body"],"framing":"raw","terminator":"cr"},` +
			`"observed_at":{"operator":"unknown"},"source":{"operator":"field","locator":["src"]},` +
			`"direction":{"operator":"declared","declared":"inbound"},"channel":{"operator":"unknown"}}`,
	}
	recipes := make([]Recipe, len(documents))
	for i, document := range documents {
		read, err := DecodeRecipe([]byte(document))
		if err != nil {
			t.Fatalf("fuzz recipe %d: %v", i, err)
		}
		recipes[i] = read
	}
	return recipes
}

// FuzzEnvelopeRecords exercises all four readers directly. Whatever bytes they
// are given, a member divides into ordered, non-overlapping records inside its
// own bounds, each either carrying every declared value or naming one of the
// fixed reasons it does not, or the member is refused. There is no third
// outcome in which a record reaches outside the member or a reason repeats a
// byte of it.
func FuzzEnvelopeRecords(f *testing.F) {
	for _, seed := range []string{
		"t,s,m\r\n2026-01-02T03:04:05Z,a,MSH|^~\\&|A\r\r\n",
		"t,s,m\r\n\"a\r\nb\",c\r\n",
		"i;TVNI\n",
		"20260102030405-0500|a|MSH|^~\\&|A\r\n",
		`{"events":[{"t":0,"c":"a","m":{"b":"MSH"}}]}`,
		`{"events":[{"t":0,"c":"a","m":{"b":"MSH"}}, @]}`,
		"<corpus><message><src>a</src><body>MSH&#13;</body></message></corpus>",
		"<corpus><message><body><![CDATA[x]]></body></message>",
		"", "\"", "&", "<", "\x00\xff",
	} {
		f.Add(seed)
	}
	recipes := fuzzRecipes(f)
	f.Fuzz(func(t *testing.T, member string) {
		data := []byte(member)
		for _, read := range recipes {
			locators := read.locators()
			records, err := envelopeRecords(read, locators, data)
			if err != nil {
				if len(err.Error()) > 256 {
					t.Fatal("unbounded diagnostic")
				}
				continue
			}
			if len(records) > MaxEnvelopeRecords {
				t.Fatalf("%s accepted %d records", read.Envelope, len(records))
			}
			at := 0
			for i, record := range records {
				if record.offset < at || record.size < 0 || record.offset+record.size > len(data) {
					t.Fatalf("%s record %d reaches outside the member: %+v of %d", read.Envelope, i, record, len(data))
				}
				at = record.offset + record.size
				switch {
				case record.reason == "":
					for _, locator := range locators {
						if _, located := record.value(locator); !located {
							t.Fatalf("%s record %d is missing a declared value", read.Envelope, i)
						}
					}
				case !slices.Contains(reasons, record.reason):
					t.Fatalf("%s record %d gave an unknown reason: %q", read.Envelope, i, record.reason)
				case record.values != nil:
					t.Fatalf("%s record %d is half read", read.Envelope, i)
				}
			}
			// Every record becomes one source, and a record the recipe could
			// not map is stored with exactly the bytes the member held.
			divider := recipeDivider{recipe: read, locators: locators}
			sources, err := divider.divide(data)
			if err != nil || len(sources) != len(records) {
				t.Fatalf("%s divided %d records into %d sources: %v", read.Envelope, len(records), len(sources), err)
			}
			for i, extracted := range sources {
				if extracted.mapping == nil {
					t.Fatalf("%s source %d has no mapping", read.Envelope, i)
				}
				if extracted.mapping.State != Unmapped {
					continue
				}
				if !slices.Contains(reasons, extracted.mapping.Reason) && !slices.Contains(
					[]string{ReasonPayload, ReasonFraming, ReasonTime, ReasonDirection, ReasonLabel}, extracted.mapping.Reason) {
					t.Fatalf("%s source %d gave an unknown reason: %q", read.Envelope, i, extracted.mapping.Reason)
				}
				if extracted.mapping.ObservedAt != nil || extracted.mapping.Source != "" ||
					extracted.mapping.Channel != "" || extracted.mapping.Direction != bundle.Unknown {
					t.Fatalf("%s source %d kept a mapped value: %+v", read.Envelope, i, extracted.mapping)
				}
				if !bytes.Equal(extracted.data, data[records[i].offset:records[i].offset+records[i].size]) {
					t.Fatalf("%s source %d did not retain the record's own bytes", read.Envelope, i)
				}
			}
		}
	})
}

// FuzzCSVRecord exercises the record splitter directly. It is the one boundary
// standing between a crafted export and a record that overlaps another, and it
// must always report a boundary that moves forward so no byte is read twice
// and no member can be scanned without end.
func FuzzCSVRecord(f *testing.F) {
	for _, seed := range []string{
		"a,b\r\nc,d\r\n", `"a""b",c`, `"a`, `a"b`, `"a"b`, ",,,", "\r\n", "", "\"\r\n\"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, member string) {
		data := []byte(member)
		for _, separator := range [][]byte{{'\n'}, {'\r', '\n'}} {
			at := 0
			for reads := 0; at < len(data); reads++ {
				if reads > len(data) {
					t.Fatal("the reader did not move forward")
				}
				fields, content, end, ok := csvRecord(data, at, ',', separator)
				if content < at || content > len(data) || end < content || end > len(data) || end <= at {
					t.Fatalf("record at %d reported content %d end %d of %d", at, content, end, len(data))
				}
				if ok && len(fields) > MaxEnvelopeFields {
					t.Fatalf("record at %d accepted %d fields", at, len(fields))
				}
				if ok {
					// An accepted record's fields are the member's own bytes
					// with only doubled quotes undone, so they never grow.
					total := 0
					for _, field := range fields {
						total += len(field)
					}
					if total > content-at {
						t.Fatalf("record at %d grew %d bytes into %d", at, content-at, total)
					}
				}
				at = end
			}
		}
	})
}

// FuzzXMLContent exercises the character-data reader directly. It reads bytes an
// export fully controls, and it is the one refusal standing between a document
// whose line endings a conforming reader would rewrite and a payload readmit
// would store as though the rewrite had not happened.
func FuzzXMLContent(f *testing.F) {
	for _, seed := range []string{
		"abc", "a&amp;b", "a&#13;b", "a\rb", "<![CDATA[a]]>", "<![CDATA[", "&#x110000;", "&", "&;", "&#;", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		value, err := xmlContent([]byte(raw))
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		// Resolving references and CDATA only ever removes markup, so the read
		// value can never be longer than the bytes it was read from.
		if len(value) > len(raw) {
			t.Fatalf("%d bytes of character data read as %d", len(raw), len(value))
		}
		// A raw carriage return is exactly what XML normalization changes, so a
		// value that was accepted may only hold one a reference stood for.
		if bytes.IndexByte(value, '\r') >= 0 && !strings.Contains(strings.ToLower(raw), "&#") {
			t.Fatal("a raw carriage return was read as character data")
		}
		again, err := xmlContent([]byte(raw))
		if err != nil || !bytes.Equal(again, value) {
			t.Fatal("the same character data read differently twice")
		}
	})
}

// TestPayloadFramingUsesTheSameRefusalsAnImportPlanUses keeps one reading of
// what a message is: a mapped payload is held to the framing declaration
// exactly as a plan-divided record is.
func TestPayloadFramingUsesTheSameRefusalsAnImportPlanUses(t *testing.T) {
	message := "MSH|^~\\&|A\rPID|1\r"
	for name, testCase := range map[string]struct {
		framing Framing
		payload string
		ok      bool
	}{
		"raw message":       {RawFraming, message, true},
		"raw pair":          {RawFraming, message + message, false},
		"raw but framed":    {RawFraming, "\x0b" + message + "\x1c\r", false},
		"mllp frame":        {MLLPFraming, "\x0b" + message + "\x1c\r", true},
		"mllp but unframed": {MLLPFraming, message, false},
		"not a message":     {RawFraming, "plain text", true},
	} {
		_, err := recordStarts(testCase.framing, "", hl7.CR, []byte(testCase.payload))
		if (err == nil) != testCase.ok {
			t.Errorf("%s: err=%v, want ok=%v", name, err, testCase.ok)
		}
	}
}
