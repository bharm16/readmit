package redact

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

const resetInstructions = "Start a fresh readmit listen session in the selected defective or fixed mode with new output and observation paths. Wait for Listening. The ledger and processed list must both be empty. Choose a new result directory for every run."

type literal struct {
	location     string
	value        *string
	protocolCode bool
}

func (t *transformer) transformSpec(spec testrunner.Spec) (testrunner.Spec, error) {
	// Assertion values include pointers and slices. Keep the original proof's
	// expectations immutable instead of sharing a shallow Spec copy.
	raw, err := encode(spec)
	if err != nil {
		return spec, err
	}
	spec, err = testrunner.DecodeSpec(raw)
	if err != nil {
		return spec, err
	}
	if spec.Observation.Boundary != testrunner.LedgerBoundary {
		return spec, errors.New("derived export v1 requires the appointment-ledger fixture boundary")
	}
	for _, index := range t.policy.RequiredFailures {
		if index > len(spec.Assertions) {
			return spec, errors.New("required failing assertion does not exist")
		}
	}
	if err := t.finding("spec/metadata-and-paths", "other-unique-identifiers", "spec-name-reset-paths-and-assertion-labels", Metadata, t.has(Metadata)); err != nil {
		return spec, err
	}
	if err := t.finding("spec/filename", "other-unique-identifiers", "source-filename", Filenames, t.has(Filenames)); err != nil {
		return spec, err
	}
	if err := t.finding("spec/execution", "other-unique-identifiers", "fresh-fixture-reruns-required", Rerun, t.has(Rerun)); err != nil {
		return spec, err
	}
	spec.Name = "Derived appointment regression"
	spec.Input.Case = "case"
	spec.Target = "target.json"
	spec.Setup.ResetInstructions = resetInstructions
	spec.Observation.Path = "observation.json"
	bindings := map[string]LiteralBinding{}
	for _, binding := range t.policy.SpecBindings {
		bindings[binding.Location] = binding
	}
	used := map[string]bool{}
	for i := range spec.Assertions {
		assertion := &spec.Assertions[i]
		assertion.ID = fmt.Sprintf("a%06d", i+1)
		var literals []literal
		base := fmt.Sprintf("assertions/%d/expected", i+1)
		if assertion.Expected.Records != nil {
			for j := range *assertion.Expected.Records {
				r := &(*assertion.Expected.Records)[j]
				path := fmt.Sprintf("%s/records/%d", base, j+1)
				for _, item := range []struct {
					name string
					id   *observation.Identifier
				}{{"patient_id", &r.PatientID}, {"placer_id", &r.PlacerID}, {"filler_id", &r.FillerID}} {
					for _, part := range []struct {
						name  string
						value *string
					}{{"value", &item.id.Value}, {"namespace", &item.id.Namespace}, {"universal_id", &item.id.UniversalID}, {"universal_id_type", &item.id.UniversalIDType}} {
						literals = append(literals, literal{location: path + "/" + item.name + "/" + part.name, value: part.value})
					}
				}
				literals = append(literals, literal{location: path + "/appointment_start", value: &r.AppointmentStart})
			}
		}
		if assertion.Expected.Field != nil && assertion.Expected.Field.Text != nil {
			selector, _ := hl7.ParseSelector(assertion.Selector)
			literals = append(literals, literal{location: base + "/field/text", value: assertion.Expected.Field.Text, protocolCode: selector.String() == "MSA[1]-1[1]"})
		}
		for _, item := range literals {
			binding, exists := bindings[item.location]
			if *item.value == "" && !exists {
				continue
			} // Explicit empty tuple members carry no source bytes.
			used[item.location] = exists
			resolved := false
			if exists && t.has(SpecLiterals) {
				if binding.Constant != nil {
					resolved = item.protocolCode && *binding.Constant == *item.value
				} else {
					original, oldOK := selectedText(t.original[binding.Occurrence], binding.Selector)
					derived, newOK := selectedText(t.derived[binding.Occurrence], binding.Selector)
					if oldOK && newOK && original == *item.value && len(derived) > 0 {
						*item.value = derived
						resolved = true
					}
				}
			}
			if err := t.finding("spec/"+item.location, "other-unique-identifiers", "spec-literal", SpecLiterals, resolved); err != nil {
				return spec, err
			}
		}
	}
	for location := range bindings {
		if !used[location] {
			return spec, errors.New("literal binding does not name an expected spec value")
		}
	}
	if err := spec.Validate(); err != nil {
		return spec, errors.New("derived spec cannot preserve the original assertion contract")
	}
	return spec, nil
}

func selectedText(doc *hl7.Document, path string) (string, bool) {
	if doc == nil {
		return "", false
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		return "", false
	}
	value, err := doc.Read(0, selector, hl7.IgnoreMSH18)
	if err != nil {
		return "", false
	}
	return value.Text()
}

func failedAssertions(artifact *testrunner.Artifact) []int {
	failed := []int{}
	for i, result := range artifact.Result.Assertions {
		if result.Status == "failed" {
			failed = append(failed, i+1)
		}
	}
	return failed
}
