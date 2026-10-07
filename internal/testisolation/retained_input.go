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
	return retainedInputBindings(planRaw, registryRaw, instance, "")
}

// RetainedRuntimeInputBindings normalizes only a retained runtime allocation's
// parent and instance for template approval comparison. The caller must derive
// parentPlan from its separately verified immutable runtime template.
func RetainedRuntimeInputBindings(planRaw, registryRaw []byte, instance, parentPlan string) (map[string]networkaction.Binding, []byte, error) {
	if !networkaction.ValidDigest(parentPlan) {
		return nil, nil, refused
	}
	return retainedInputBindings(planRaw, registryRaw, instance, parentPlan)
}

func retainedInputBindings(planRaw, registryRaw []byte, instance, parentPlan string) (map[string]networkaction.Binding, []byte, error) {
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
	if parentPlan != "" {
		options.ParentPlan = parentPlan
	}
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
