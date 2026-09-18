package importer_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/importer"
)

func located(t *testing.T, shape importer.EnvelopeShape, locator importer.Locator, data string) []importer.LocatedRecord {
	t.Helper()
	records, err := shape.Divide([]importer.Locator{locator}, []byte(data))
	if err != nil {
		t.Fatalf("divide: %v", err)
	}
	return records
}

func TestDivideReadsEveryDeclaredEnvelopeThroughOneReader(t *testing.T) {
	for _, test := range []struct {
		name    string
		shape   importer.EnvelopeShape
		locator importer.Locator
		data    string
		want    []string
	}{
		{
			name: "csv with a declared header",
			shape: importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8,
				CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 2}},
			locator: importer.Locator{"id"},
			data:    "id,state\nA1,booked\nA2,moved\n",
			want:    []string{"A1", "A2"},
		},
		{
			name: "csv named by column index",
			shape: importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UnknownEncoding,
				CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2}},
			locator: importer.Locator{"1"},
			data:    "A1,booked\n",
			want:    []string{"A1"},
		},
		{
			name: "a timestamped text log",
			shape: importer.EnvelopeShape{Envelope: importer.TextEnvelope, Encoding: importer.UnknownEncoding,
				Text: &importer.TextDialect{FieldSeparator: "|", RecordSeparator: importer.LFSeparator, Fields: 2}},
			locator: importer.Locator{"2"},
			data:    "2026-01-03T11:00:00Z|A1\n",
			want:    []string{"A1"},
		},
		{
			name: "a json document",
			shape: importer.EnvelopeShape{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8,
				JSON: &importer.DocumentDialect{RecordPath: []string{"appointments"}}},
			locator: importer.Locator{"id"},
			data:    `{"appointments":[{"id":"A1"},{"id":"A2"}]}`,
			want:    []string{"A1", "A2"},
		},
		{
			name: "an xml document",
			shape: importer.EnvelopeShape{Envelope: importer.XMLEnvelope, Encoding: importer.UTF8,
				XML: &importer.DocumentDialect{RecordPath: []string{"export", "appointment"}}},
			locator: importer.Locator{"id"},
			data:    `<export><appointment><id>A1</id></appointment><appointment><id>A2</id></appointment></export>`,
			want:    []string{"A1", "A2"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			records := located(t, test.shape, test.locator, test.data)
			if len(records) != len(test.want) {
				t.Fatalf("divided into %d records, want %d", len(records), len(test.want))
			}
			for index, want := range test.want {
				value, ok := records[index].Value(test.locator)
				if !ok || value != want {
					t.Fatalf("record %d located %q, want %q", index, value, want)
				}
			}
		})
	}
}

// Dividing a document changes none of its bytes. Go's encoding/csv rewrites a
// carriage return inside a quoted field, which would alter an HL7 payload
// carried in one; this reader does not, and neither does dividing through it.
func TestDivideKeepsTheBytesInsideAQuotedFieldExactly(t *testing.T) {
	shape := importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UnknownEncoding,
		CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2}}
	records := located(t, shape, importer.Locator{"2"}, "A1,\"MSH|^~\\&\r\nEVN|A01\"\n")
	if len(records) != 1 {
		t.Fatalf("divided into %d records, want 1", len(records))
	}
	value, ok := records[0].Value(importer.Locator{"2"})
	if !ok || value != "MSH|^~\\&\r\nEVN|A01" {
		t.Fatalf("located %q; the record's own bytes were rewritten", value)
	}
}

// A record that could not be read into the declared schema comes back carrying
// its reason and no values, so a caller reports evidence with no single reading
// rather than a document that held fewer records.
func TestDivideReportsARecordItCouldNotReadRatherThanDroppingIt(t *testing.T) {
	shape := importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UnknownEncoding,
		CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2}}
	records := located(t, shape, importer.Locator{"1"}, "A1,booked\nA2\n")
	if len(records) != 2 {
		t.Fatalf("divided into %d records, want 2", len(records))
	}
	if records[1].Reason == "" || records[1].Values != nil {
		t.Fatalf("a record that is not the declared shape was read: %+v", records[1])
	}
}

func TestDivideRefusesADeclarationItCannotApply(t *testing.T) {
	flat := importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8,
		CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2}}
	for _, test := range []struct {
		name     string
		shape    importer.EnvelopeShape
		locators []importer.Locator
		data     string
	}{
		{"no locator at all", flat, nil, "A1,booked\n"},
		{"a column past the declared field count", flat, []importer.Locator{{"3"}}, "A1,booked\n"},
		{"two dialects for one envelope", importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8,
			CSV:  &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2},
			Text: &importer.TextDialect{FieldSeparator: "|", RecordSeparator: importer.LFSeparator, Fields: 2}},
			[]importer.Locator{{"1"}}, "A1,booked\n"},
		{"an envelope that is not declared", importer.EnvelopeShape{Encoding: importer.UTF8}, []importer.Locator{{"1"}}, "A1\n"},
		{"bytes that contradict the declared encoding",
			importer.EnvelopeShape{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8,
				CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderAbsent, Fields: 2}},
			[]importer.Locator{{"1"}}, "A1,\xff\xfe\n"},
		{"a document that does not hold the declared record path",
			importer.EnvelopeShape{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8,
				JSON: &importer.DocumentDialect{RecordPath: []string{"appointments"}}},
			[]importer.Locator{{"id"}}, `{"other":[{"id":"A1"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.shape.Divide(test.locators, []byte(test.data)); err == nil {
				t.Fatal("the declaration was applied")
			}
		})
	}
}
