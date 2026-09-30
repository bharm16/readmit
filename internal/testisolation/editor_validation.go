package testisolation

// ValidateConfiguration validates a logical editor publication through the
// isolation compiler before any of its members is written. It neither reads
// provider material nor returns a prepared runtime plan or execution authority.
func ValidateConfiguration(contract, registry, policy []byte, options Options) error {
	_, err := prepare(contract, registry, policy, options)
	return err
}

// EditorVocabulary is the finite subset the managed isolation editor supports.
// Recorded baselines require separately selected retained snapshot proof and
// are left intact but refused by this editor, rather than accepting a hash as
// an instruction to mutate a target.
type EditorVocabulary struct {
	Modes         []string `json:"modes"`
	ResourceKinds []string `json:"resource_kinds"`
	Ownership     []string `json:"ownership"`
}

func EditorChoices() EditorVocabulary {
	return EditorVocabulary{Modes: []string{"isolated-tenant", "reserved-namespace"}, ResourceKinds: []string{"patient", "visit", "appointment", "order", "logical-resource", "business-identifier", "practitioner", "location", "reference"}, Ownership: []string{"create", "claim", "select"}}
}
