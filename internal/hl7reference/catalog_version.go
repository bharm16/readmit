package hl7reference

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
)

// A strict reader must reject members introduced after the named schema by
// presence, including null/empty values that disappear in a shared Go DTO.
func checkVersionMembers(raw []byte, schema string) error {
	var members struct {
		Coverage map[string]jsontext.Value   `json:"coverage"`
		Records  []map[string]jsontext.Value `json:"records"`
	}
	if err := json.Unmarshal(raw, &members); err != nil {
		return err
	}
	rank := schemaRank(schema)
	recordIntroduced := map[string]int{"container": 2, "position": 2, "table_id": 3, "table_kind": 3, "item_id": 3, "code": 3, "content_state": 3, "uses": 3, "unparsed_rows": 3, "message_code": 4, "event": 4, "structures": 4, "structure_id": 4, "sequence": 4, "name_origin": 5, "definition_origin": 5, "table_metadata": 6}
	coverageIntroduced := map[string]int{"datatypes": 2, "components": 2, "tables": 3, "codes": 3, "elements": 3, "messages": 4, "structures": 4}
	for _, key := range slices.Sorted(maps.Keys(members.Coverage)) {
		if introduced := coverageIntroduced[key]; introduced > rank {
			return fmt.Errorf("coverage member %s requires catalog v%d", key, introduced)
		}
	}
	for _, record := range members.Records {
		if rank < 5 {
			for _, name := range []string{"datatype", "optionality", "length", "conformance_length", "repetition", "item", "table", "section"} {
				var attribute map[string]jsontext.Value
				if err := json.Unmarshal(record[name], &attribute); err != nil {
					return err
				}
				if _, present := attribute["origin"]; present {
					return fmt.Errorf("attribute origin requires catalog v5")
				}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(record)) {
			if introduced := recordIntroduced[key]; introduced > rank {
				return fmt.Errorf("record member %s requires catalog v%d", key, introduced)
			}
		}
	}
	return nil
}

func schemaRank(schema string) int {
	return map[string]int{Schema: 1, SchemaV2: 2, SchemaV3: 3, SchemaV4: 4, SchemaV5: 5, SchemaV6: 6}[schema]
}
