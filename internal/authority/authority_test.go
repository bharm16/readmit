package authority_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/authority"
)

func TestValidateAnswersEachWayADeclaredSetIsRefused(t *testing.T) {
	valid := []authority.Mapping{
		{Key: "READMIT", Namespace: "READMIT"},
		{Key: "ISO", UniversalID: "1.2.3", UniversalIDType: "ISO"},
	}
	if defect := authority.Validate(valid); defect != authority.Valid {
		t.Fatalf("a complete declared set was refused: %v", defect)
	}
	for name, mappings := range map[string][]authority.Mapping{
		"over the bound":  mappingsOverTheBound(),
		"keyless key":     {{Namespace: "READMIT"}},
		"empty key":       {{Key: "", Namespace: "READMIT"}},
		"bad key":         {{Key: ".leading", Namespace: "READMIT"}},
		"unbounded key":   {{Key: strings.Repeat("k", 129), Namespace: "READMIT"}},
		"control part":    {{Key: "X", Namespace: "READ\tMIT"}},
		"invalid part":    {{Key: "X", Namespace: "READMIT\x80"}},
		"no identity":     {{Key: "X"}},
		"half universal":  {{Key: "X", Namespace: "READMIT", UniversalID: "1.2.3"}},
		"half universal2": {{Key: "X", UniversalIDType: "ISO"}},
		"duplicate":       {{Key: "A", Namespace: "READMIT"}, {Key: "B", Namespace: "READMIT"}},
	} {
		if defect := authority.Validate(mappings); defect == authority.Valid {
			t.Fatalf("%s was accepted", name)
		}
	}
	// A namespace beside a complete universal identifier is one assertion
	// too many, not a refusal: the parts decide.
	if defect := authority.Validate([]authority.Mapping{{Key: "X", Namespace: "READMIT", UniversalID: "1.2.3", UniversalIDType: "ISO"}}); defect != authority.Valid {
		t.Fatalf("a fully spelled authority was refused: %v", defect)
	}
}

func mappingsOverTheBound() []authority.Mapping {
	mappings := make([]authority.Mapping, authority.MaxMappings+1)
	for i := range mappings {
		mappings[i] = authority.Mapping{Key: "K" + strings.Repeat("x", i)}
	}
	return mappings
}

func TestDecodeReadsOneMappingExactlyAsWritten(t *testing.T) {
	mapping, defect := authority.Decode([]byte(`{"key":"K","namespace":"READMIT","universal_id":"","universal_id_type":""}`))
	if defect != authority.Valid || mapping != (authority.Mapping{Key: "K", Namespace: "READMIT"}) {
		t.Fatalf("a complete mapping did not decode: %v %v", mapping, defect)
	}
	if _, defect := authority.Decode([]byte(`{"namespace":"READMIT"}`)); defect != authority.Absent {
		t.Fatalf("a keyless mapping was not answered as absent: %v", defect)
	}
	if _, defect := authority.Decode([]byte(`{"key":"K","alias":true}`)); defect != authority.UnknownMember {
		t.Fatalf("an unknown member was not answered: %v", defect)
	}
	if _, defect := authority.Decode([]byte(`{"key":5}`)); defect != authority.Absent {
		t.Fatalf("a mapping a key cannot be read from was not answered as absent: %v", defect)
	}
}

func TestTableResolvesTheConfiguredKeyOfOneAuthority(t *testing.T) {
	table := authority.NewTable([]authority.Mapping{
		{Key: "READMIT", Namespace: "READMIT"},
		{Key: "ISO", UniversalID: "1.2.3", UniversalIDType: "ISO"},
	})
	if key, ok := table.Resolve(authority.Parts{Namespace: "READMIT"}); !ok || key != "READMIT" {
		t.Fatalf("a configured namespace did not resolve: %q %v", key, ok)
	}
	if key, ok := table.Resolve(authority.Parts{UniversalID: "1.2.3", UniversalIDType: "ISO"}); !ok || key != "ISO" {
		t.Fatalf("a configured universal identifier did not resolve: %q %v", key, ok)
	}
	if _, ok := table.Resolve(authority.Parts{Namespace: "OTHER"}); ok {
		t.Fatal("an unconfigured authority resolved")
	}
	if _, ok := table.Resolve(authority.Parts{Namespace: "READMIT", UniversalID: "1.2.3"}); ok {
		t.Fatal("a half-specified universal identifier resolved")
	}
	if _, ok := table.Resolve(authority.Parts{}); ok {
		t.Fatal("no authority at all resolved")
	}
}

func TestCompleteNamesEveryShapeTheContractsAccept(t *testing.T) {
	for name, parts := range map[string]authority.Parts{
		"namespace only":                         {Namespace: "READMIT"},
		"universal id and type":                  {UniversalID: "1.2.3", UniversalIDType: "ISO"},
		"id and type, and namespace beside them": {Namespace: "READMIT", UniversalID: "1.2.3", UniversalIDType: "ISO"},
		"id without type":                        {Namespace: "READMIT", UniversalID: "1.2.3"},
		"type without id":                        {Namespace: "READMIT", UniversalIDType: "ISO"},
		"no authority at all":                    {},
	} {
		complete := parts.Complete()
		want := name != "id without type" && name != "type without id" && name != "no authority at all"
		if complete != want {
			t.Fatalf("%s: completeness is %v", name, complete)
		}
	}
}
