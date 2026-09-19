package findingreview_test

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

const (
	booking = "MSH|^~\\&|SCHEDULE|SYNTHETIC|RECEIVER|LAB|20260101120000+0000||SIU^S12|BOOK-1|P|2.5.1\r" +
		"SCH|PLACER-001^READMIT|FILLER-001^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000+0000\r" +
		"PID|1||PATIENT-001^^^READMIT||SYNTHETIC^ONLY\r"
	acknowledgement = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SYNTHETIC|20260101120100+0000||ACK|BOOK-1-ACK|P|2.5.1\r" +
		"MSA|AE|BOOK-1|FREE-TEXT-NEVER-COPIED\r" +
		"ERR|||101^FREE-TEXT-NEVER-COPIED^HL70357|E\r"
	twoErrorACK = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SYNTHETIC|20260101120100+0000||ACK|BOOK-1-ACK|P|2.5.1\r" +
		"MSA|AE|BOOK-1|FREE-TEXT-NEVER-COPIED\r" +
		"ERR|||101^FREE-TEXT-NEVER-COPIED^HL70357|E\r" +
		"ERR|||102^FREE-TEXT-NEVER-COPIED^HL70357|W\r"
	unlinkedACK = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SYNTHETIC|20260101120100+0000||ACK|OTHER-ACK|P|2.5.1\r" +
		"MSA|AA|NOTHING-CAPTURED-ECHOES-THIS\r"
	incompleteBooking = "MSH|^~\\&|SCHEDULE|SYNTHETIC|RECEIVER|LAB|20260101120000+0000||SIU^S12|BOOK-2|P|2.5.1\r" +
		"SCH|PLACER-002^READMIT|FILLER-002^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000+0000\r" +
		"PID|1||||SYNTHETIC^ONLY\r"
)

// frame wraps occurrences as one MLLP source, which is the only way a captured
// acknowledgement is correlated to the message it answers.
func frame(messages ...string) []byte {
	var raw []byte
	for _, message := range messages {
		raw = append(raw, 0x0b)
		raw = append(raw, message...)
		raw = append(raw, 0x1c, 0x0d)
	}
	return raw
}

// openCase writes one case and opens it, reporting both the directory the
// diagnosis is run over and the verified bundle a promotion reads.
func openCase(t *testing.T, sources ...[]byte) (string, *bundle.Bundle) {
	t.Helper()
	imported := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	inputs := make([]bundle.Input, len(sources))
	for i, data := range sources {
		inputs[i] = bundle.Input{Path: "SYNTHETIC-FIXTURE", Data: data}
	}
	path := filepath.Join(t.TempDir(), "case")
	if _, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, opened
}

// diagnosed runs the shipped diagnosis and reports it with the identity of the
// exact bytes a decision names.
func diagnosed(t *testing.T, path string) (diagnose.Report, string) {
	t.Helper()
	report, err := diagnose.Run(path, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	data, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return report, hex.EncodeToString(sum[:])
}

func digest(t *testing.T, value string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func reviewed(report diagnose.Report, identity string, source *bundle.Bundle) findingreview.Reviewed {
	return findingreview.Reviewed{Report: report, Identity: identity, Case: source, Entry: "case"}
}

func decisions(report string, list ...findingreview.Decision) findingreview.Decisions {
	return findingreview.Decisions{Schema: findingreview.DecisionsSchema, Report: report, Decisions: list}
}

// review runs one review that is expected to succeed.
func review(t *testing.T, report diagnose.Report, identity string, source *bundle.Bundle, list ...findingreview.Decision) findingreview.Record {
	t.Helper()
	record, err := findingreview.Review(reviewed(report, identity, source), decisions(identity, list...), digest(t, "decisions"))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func status(t *testing.T, record findingreview.Record, finding string) findingreview.Status {
	t.Helper()
	at := slices.IndexFunc(record.Findings, func(s findingreview.Status) bool { return s.Finding == finding })
	if at < 0 {
		t.Fatalf("the review does not report %s", finding)
	}
	return record.Findings[at]
}

// findingsOf reports the identifiers of every finding one rule produced, in the
// order the report records them.
func findingsOf(report diagnose.Report, rule string) []string {
	identifiers := []string{}
	for _, finding := range report.Findings {
		if finding.RuleID == rule {
			identifiers = append(identifiers, finding.ID)
		}
	}
	return identifiers
}
