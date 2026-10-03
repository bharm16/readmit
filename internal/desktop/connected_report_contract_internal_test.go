package desktop

import (
	"encoding/json/jsontext"
	"testing"
)

func TestConnectedReportLegacyMembershipRefusesNewMembers(t *testing.T) {
	for _, value := range []string{"null", `""`, `{}`} {
		if _, err := decodeReportSources([]byte(`{"schema":"readmit-report-sources/v1","family":` + value + `,"runs":[{"role":"current","run":"0123456789abcdef01234567","job":""}]}`)); err == nil {
			t.Fatal("legacy source adopted connected family")
		}
		if validateReportShareDraft(EditorDraft{ContentSchema: "readmit-report-share-draft/v1", Content: jsontext.Value(`{"report":"r","connectedMode":` + value + `}`)}) == nil {
			t.Fatal("legacy share adopted connected mode")
		}
	}
	if validateReportShareDraft(EditorDraft{ContentSchema: "readmit-report-share-draft/v2", Content: jsontext.Value(`{"report":"r","connectedMode":"value-free-extract","token":"old-consent"}`)}) == nil {
		t.Fatal("share restored consent")
	}
}
