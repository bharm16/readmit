package index_test

import (
	"github.com/bharm16/readmit/internal/index"
	"testing"
)

func TestSchemaDeclarationSurvivesDamagedRemainderWithoutTrustingNestedKeys(t *testing.T) {
	for _, tt := range []struct {
		name, data string
		declared   bool
	}{
		{"known truncated", `{"schema":"readmit-index/v1","rows":[`, true},
		{"future truncated", `{"schema":"readmit-index/v99","rows":[`, true},
		{"after other members", `{"metadata":{"a":[1,2]},"schema":"readmit-index/v1",!`, true},
		{"escaped name and value", `{"sch\u0065ma":"readmit-index\/v99"`, true},
		{"duplicate declaration", `{"schema":null,"schema":"readmit-index/v1",`, true},
		{"nested only", `{"metadata":{"schema":"readmit-index/v1"}}`, false},
		{"nonstring schema", `{"schema":{"schema":"readmit-index/v1"}}`, false},
		{"other contract", `{"schema":"readmit-project/v1"}`, false},
		{"incomplete declaration", `{"schema":"readmit-index/v1`, false},
		{"not an object", `[{"schema":"readmit-index/v1"}]`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := index.DeclaresSchema([]byte(tt.data)); got != tt.declared {
				t.Fatalf("declared=%t want %t", got, tt.declared)
			}
		})
	}
}
