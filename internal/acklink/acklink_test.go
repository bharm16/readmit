package acklink_test

import (
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/acklink"
)

// The fixture is one case captured from two sources, whose occurrences carry
// one control identifier in two spellings: an escape in s0001, plainly in
// s0002. Which occurrence an acknowledgement answers is decided by the scope
// and the comparison the index was built with, never by anything implicit.
var (
	escaped = acklink.Value{Bytes: []byte("ACK-1\\F\\2"), Text: "ACK-1|2", Decoded: true}
	plain   = acklink.Value{Bytes: []byte("ACK-1|2"), Text: "ACK-1|2", Decoded: true}
)

func TestPerSourceScopeAnswersOnlyInsideTheAcknowledgementSource(t *testing.T) {
	index := acklink.New[string](acklink.PerSource, acklink.FieldBytes)
	index.Add("s0001", plain, "s0001-e000001")
	index.Add("s0002", plain, "s0002-e000001")
	index.Add("s0002", plain, "s0002-e000002")

	// An acknowledgement captured in s0002 is answered by that source's own
	// occurrences and never reaches across the source boundary, however many
	// candidates another source holds.
	if answered := index.Answer("s0002", plain); !reflect.DeepEqual(answered, []string{"s0002-e000001", "s0002-e000002"}) {
		t.Fatalf("a source-scoped answer did not stay inside its source: %v", answered)
	}
	if answered := index.Answer("s0001", plain); !reflect.DeepEqual(answered, []string{"s0001-e000001"}) {
		t.Fatalf("a source-scoped answer did not stay inside its source: %v", answered)
	}
}

func TestCaseWideScopeAnswersAcrossSourcesInCaseOrder(t *testing.T) {
	index := acklink.New[string](acklink.CaseWide, acklink.FieldBytes)
	index.Add("s0001", plain, "s0001-e000001")
	index.Add("s0002", plain, "s0002-e000001")
	index.Add("s0002", plain, "s0002-e000002")

	// Every source recorded the echoed bytes, so the case itself is
	// ambiguous and every candidate is reported in case order.
	answered := index.Answer("s0002", plain)
	if !reflect.DeepEqual(answered, []string{"s0001-e000001", "s0002-e000001", "s0002-e000002"}) {
		t.Fatalf("a case-wide answer did not report every candidate in case order: %v", answered)
	}
}

func TestFieldBytesAndDecodedTextDisagreeOnlyWhenTheComparisonSaysSo(t *testing.T) {
	// The same case, indexed twice: once by exact field bytes, once by
	// decoded text. The escaped and the plain spelling are one decoded text
	// and two different bytes, so each comparison answers a different pair.
	bytesIndex := acklink.New[string](acklink.CaseWide, acklink.FieldBytes)
	bytesIndex.Add("s0001", escaped, "escaped")
	if answered := bytesIndex.Answer("s0001", acklink.Value{Bytes: escaped.Bytes}); !reflect.DeepEqual(answered, []string{"escaped"}) {
		t.Fatalf("the identical escaped bytes were not answered: %v", answered)
	}
	if answered := bytesIndex.Answer("s0001", acklink.Value{Bytes: plain.Bytes}); len(answered) != 0 {
		t.Fatalf("a plain echo answered an escaped identifier under a byte comparison: %v", answered)
	}

	textIndex := acklink.New[string](acklink.CaseWide, acklink.DecodedText)
	textIndex.Add("s0001", escaped, "escaped")
	if answered := textIndex.Answer("s0001", plain); !reflect.DeepEqual(answered, []string{"escaped"}) {
		t.Fatalf("the same decoded text was not answered: %v", answered)
	}
}

func TestAnEchoTheComparisonCannotTestIsNeverAnswered(t *testing.T) {
	index := acklink.New[string](acklink.CaseWide, acklink.DecodedText)
	index.Add("s0001", escaped, "escaped")
	// Bytes nobody decoded are not comparable text, whatever they echo.
	if answered := index.Answer("s0001", acklink.Value{Bytes: escaped.Bytes}); len(answered) != 0 {
		t.Fatalf("undecoded bytes were compared as text: %v", answered)
	}
	// Nothing was indexed under the empty identifier either.
	index.Add("s0001", acklink.Value{}, "empty")
	if answered := index.Answer("s0001", acklink.Value{Decoded: true}); len(answered) != 0 {
		t.Fatalf("an empty echo answered an occurrence: %v", answered)
	}
}

func TestNewDiagnosisIsTheCaseWideDecodedPairing(t *testing.T) {
	index := acklink.NewDiagnosis[string]()
	index.Add("s0001", escaped, "escaped")
	index.Add("s0002", plain, "plain")
	// A diagnosis links across the whole case, by decoded text: the escaped
	// booking and the plainly spelled echo are the same control identifier.
	if answered := index.Answer("s0002", plain); !reflect.DeepEqual(answered, []string{"escaped", "plain"}) {
		t.Fatalf("the diagnosis pairing did not answer case-wide by decoded text: %v", answered)
	}
}
