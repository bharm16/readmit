package testisolation

import (
	"bytes"
	"encoding/json/v2"

	"github.com/bharm16/readmit/internal/networkaction"
)

// RetainedInputBindings verifies the actual retained allocation plan, matches
// its decoded registry to the separately pinned original registry bytes, and
// derives the fixed metadata-instance allocation. It reads no file or provider
// and returns metadata only, never executable authority or a live session.
func RetainedInputBindings(planRaw, registryRaw []byte, instance string) (map[string]networkaction.Binding, []byte, error) {
	p, err := readPlan(map[string][]byte{"plan.json": planRaw})
	if err != nil {
		return nil, nil, err
	}
	var registry Registry
	if len(registryRaw) > MaxBytes || json.Unmarshal(registryRaw, &registry, json.RejectUnknownMembers(true)) != nil || !bytes.Equal(canonical(registry), canonical(p.document.Registry)) {
		return nil, nil, refused
	}
	options := p.document.Options
	options.Instance = instance
	metadata, err := prepare(canonical(p.document.Contract), registryRaw, p.document.Policy, options)
	if err != nil {
		return nil, nil, err
	}
	bindings := map[string]networkaction.Binding{}
	for _, role := range []string{"read", "setup", "cleanup"} {
		bindings["isolation:"+role] = metadata.Review(role).Binding
	}
	return bindings, bytes.Clone(p.document.Policy), nil
}
