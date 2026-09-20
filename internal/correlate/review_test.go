package correlate_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/correlate"
	"strings"
	"testing"
)

const reviewDocument = `{"schema":"readmit-correlation-review/v1","machine":"digest","parent":"prefix","decisions":[{"action":"add","link":"","from":"s0001-e000001","to":"s0002-e000001","actor":"analyst","reason":"local context"}]}`

func TestCorrelationReviewReaderRequiresEveryNestedMember(t *testing.T) {
	if _, err := correlate.DecodeReview([]byte(reviewDocument)); err != nil {
		t.Fatal(err)
	}
	var object map[string]jsontext.Value
	if err := json.Unmarshal([]byte(reviewDocument), &object); err != nil {
		t.Fatal(err)
	}
	for key := range object {
		copy := map[string]jsontext.Value{}
		for k, v := range object {
			copy[k] = v
		}
		delete(copy, key)
		data, _ := json.Marshal(copy)
		if _, err := correlate.DecodeReview(data); err == nil {
			t.Errorf("accepted missing %s", key)
		}
	}
	var decisions []map[string]jsontext.Value
	if err := json.Unmarshal(object["decisions"], &decisions); err != nil {
		t.Fatal(err)
	}
	for key := range decisions[0] {
		copy := map[string]jsontext.Value{}
		for k, v := range decisions[0] {
			copy[k] = v
		}
		delete(copy, key)
		data, _ := json.Marshal([]map[string]jsontext.Value{copy})
		changed := map[string]jsontext.Value{}
		for k, v := range object {
			changed[k] = v
		}
		changed["decisions"] = data
		encoded, _ := json.Marshal(changed)
		if _, err := correlate.DecodeReview(encoded); err == nil {
			t.Errorf("accepted missing nested %s", key)
		}
	}
	for _, bad := range []string{
		strings.Replace(reviewDocument, `"action":`, `"unknown":true,"action":`, 1),
		strings.Replace(reviewDocument, `"schema":`, `"unknown":true,"schema":`, 1),
		strings.Replace(reviewDocument, `"actor":"analyst"`, `"actor":null`, 1),
		strings.Replace(reviewDocument, `"actor":"analyst"`, `"actor":"a","actor":"b"`, 1),
		strings.Replace(reviewDocument, "readmit-correlation-review/v1", "readmit-correlation-review/v2", 1),
		strings.Repeat(" ", correlate.MaxReviewBytes) + reviewDocument,
	} {
		if _, err := correlate.DecodeReview([]byte(bad)); err == nil {
			t.Error("accepted malformed review")
		}
	}
}

func FuzzCorrelationReviewDocument(f *testing.F) {
	f.Add([]byte(reviewDocument))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := correlate.DecodeReview(data)
		if err != nil {
			return
		}
		if decoded.Schema != correlate.ReviewSchema || decoded.Decisions == nil || len(decoded.Decisions) > correlate.MaxDecisions {
			t.Fatal("accepted unsupported review shape")
		}
		encoded, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := correlate.DecodeReview(encoded); err != nil {
			t.Fatal("accepted document cannot round trip")
		}
	})
}
