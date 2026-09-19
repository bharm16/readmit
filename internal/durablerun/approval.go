package durablerun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/testrunner"
)

// InputIdentity seals the same prepared inputs Start consumes. Credential
// registration is validated and committed, but no secret value is resolved.
func (p *Prepared) InputIdentity() (string, error) {
	inputs := p.plan.PinnedInputs()
	ref, err := replay.BindCredential(inputs.Configuration)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Engine     string
		Inputs     testrunner.PinnedInputs
		Credential secret.Reference
	}{engine.Version(), inputs, ref}, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
