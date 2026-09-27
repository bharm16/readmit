package profileeval

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/bundle"
)

// EvaluateBundle opens canonical retained evidence through its verifying reader.
// CompleteCapture is an explicit caller declaration; file completeness alone
// never proves that a clinical prerequisite was captured.
func EvaluateBundle(ctx context.Context, profile, pack []byte, path string, options Options) (Report, error) {
	source, err := bundle.Open(path)
	if err != nil {
		return Report{}, err
	}
	inputs := []Occurrence{}
	for _, event := range source.Events {
		if event.Kind != bundle.Message {
			continue
		}
		if len(inputs) >= 1024 {
			return Report{}, errors.New("profile occurrence limit")
		}
		raw, err := source.Raw(event.ID)
		if err != nil {
			return Report{}, err
		}
		inputs = append(inputs, Occurrence{ID: event.ID, Bytes: raw})
	}
	report, err := Evaluate(ctx, profile, pack, inputs, options)
	if err != nil {
		return Report{}, err
	}
	report.CaseIdentity = source.Identity
	return report, nil
}
