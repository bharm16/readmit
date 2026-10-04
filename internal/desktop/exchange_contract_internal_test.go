package desktop

import (
	"encoding/json/jsontext"
	"testing"
)

func TestExchangeAndEditorLegacyMembershipCannotAdoptNewPromotionMembers(t *testing.T) {
	for _, value := range []string{"null", `{}`, `""`} {
		for _, member := range []string{"inputs", "target_ref", "identity"} {
			raw := []byte(`{"schema":"readmit-exploratory-exchange/v1","` + member + `":` + value + `}`)
			if decodeExchange(raw, &ExchangeView{}) == nil {
				t.Fatalf("legacy exchange adopted %s=%s", member, value)
			}
		}
		links := []byte(`{"schema":"readmit-test-links/v1","source":{"kind":"case","exchange":` + value + `}}`)
		if _, err := decodeTestLinks(links); err == nil {
			t.Fatalf("legacy test links adopted exchange=%s", value)
		}
		for _, schema := range []string{TestEditorDraftSchema, TestEditorDraftSchemaV2} {
			raw := jsontext.Value(`{"schema":"` + schema + `","mode":"new","step":"setup","draft":{"test_links":{"source":{"kind":"case","exchange":` + value + `}}}}`)
			if validateTestEditorDraft(EditorDraft{Kind: TestEditorDraftKind, ContentSchema: schema, Content: raw}) == nil {
				t.Fatalf("legacy editor adopted exchange=%s", value)
			}
		}
	}
	// An original legacy exchange is read exactly as written, without inventing origins.
	old := []byte(`{"schema":"readmit-exploratory-exchange/v1","id":"legacy"}`)
	view := ExchangeView{}
	if decodeExchange(old, &view) != nil || view.Inputs != nil || view.TargetRef != nil {
		t.Fatal("original legacy exchange changed interpretation")
	}
}
