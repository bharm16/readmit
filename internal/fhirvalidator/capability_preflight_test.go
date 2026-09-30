package fhirvalidator_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

func TestFHIRValidatorCapabilityPreflightNeedsNoResponseResource(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*fhirvalidator.Manifest)
		want string
	}{
		{name: "qualified"},
		{name: "wrong validator", edit: func(m *fhirvalidator.Manifest) { m.Validator.Version = "6.10.5" }, want: "unsupported-runtime"},
		{name: "wrong Java", edit: func(m *fhirvalidator.Manifest) { m.Runtime.Version = "17.0.14+7" }, want: "unsupported-runtime"},
		{name: "missing package root", edit: func(m *fhirvalidator.Manifest) { m.ValidatorPackages = m.ValidatorPackages[:1] }, want: "package-unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, assets := contractManifest()
			if tc.edit != nil {
				tc.edit(&m)
			}
			c, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "capability"), m, assets)
			if err != nil {
				t.Fatal(err)
			}
			err = c.Check()
			if tc.want == "" {
				if err != nil {
					t.Fatal("qualified local metadata did not pass", err)
				}
				return
			}
			var status fhirvalidator.Status
			if !errors.As(err, &status) || status.State != tc.want || status.Requirement == "" {
				t.Fatal("capability refusal was not actionable", err)
			}
		})
	}
}
