package durablerun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
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
	return inputIdentity(engine.Version(), inputs, ref)
}

// RetainedInputIdentity reconstructs an approval commitment solely from verified
// retained inputs. Credential registrations were not retained by this contract;
// those runs explicitly refuse rather than consulting a mutable live store.
func RetainedInputIdentity(path string) (string, error) {
	_, doc, err := readJob(path)
	if err != nil {
		return "", err
	}
	if doc.Inputs.Configuration.Credential.Declared() {
		return "", errors.New("retained credential registration is unavailable")
	}
	pin, err := Engine(path)
	if err != nil {
		return "", err
	}
	return inputIdentity(pin.Engine, doc.Inputs, secret.Reference{})
}

func inputIdentity(version string, inputs testrunner.PinnedInputs, ref secret.Reference) (string, error) {
	raw, err := json.Marshal(struct {
		Engine     string
		Inputs     testrunner.PinnedInputs
		Credential secret.Reference
	}{version, inputs, ref}, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
