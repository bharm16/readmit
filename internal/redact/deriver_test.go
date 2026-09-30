package redact

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
)

// A keyed derivation is reproducible under one key and differs under
// another, and each value a handled field held can be looked up as what it
// derived to.
func TestAKeyedDerivationIsReproducibleAndLooksUpWhatItDerived(t *testing.T) {
	in := deriveFixture(t)
	key := bytes.Repeat([]byte{7}, MinDeriverKeyBytes)
	derive := func(key []byte) ([][]byte, *Deriver) {
		deriver, err := NewDeriver(in.Policy, key)
		if err != nil {
			t.Fatal(err)
		}
		var out [][]byte
		for _, event := range in.Case.Events {
			raw, err := in.Case.Raw(event.ID)
			if err != nil {
				t.Fatal(err)
			}
			derived, findings, err := deriver.Message(event.ID, raw, event.Kind, event.Terminator)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) == 0 {
				t.Fatalf("message %s derived without findings", event.ID)
			}
			out = append(out, derived)
		}
		return out, deriver
	}
	first, deriver := derive(key)
	again, _ := derive(key)
	other, _ := derive(bytes.Repeat([]byte{8}, MinDeriverKeyBytes))
	for i := range first {
		if !bytes.Equal(first[i], again[i]) {
			t.Fatalf("message %d derived differently under the same key", i)
		}
	}
	differs := false
	for i := range first {
		differs = differs || !bytes.Equal(first[i], other[i])
	}
	if !differs {
		t.Fatal("another key derived the same surrogates and shifts")
	}
	if len(deriver.Terms()) == 0 {
		t.Fatal("the derivation replaced no known value")
	}
	looked := 0
	for _, term := range deriver.Terms() {
		value := deriver.Lookup(string(term))
		if value == nil || value.Conflict {
			continue
		}
		looked++
		if value.Derived == string(term) {
			t.Fatalf("a replaced value looks up as itself under %s", value.Rule.Policy)
		}
	}
	if looked == 0 {
		t.Fatal("no replaced value can be looked up")
	}
	if _, err := NewDeriver(in.Policy, []byte("short")); err == nil {
		t.Fatal("a short key was accepted")
	}
}

// A single rule is checked with the same words the policy reader uses.
func TestValidateFieldRuleUsesThePolicyReadersWords(t *testing.T) {
	if err := ValidateFieldRule(FieldRule{Selector: "PID-5", Policy: Remove, Class: "names"}); err != nil {
		t.Fatal(err)
	}
	err := ValidateFieldRule(FieldRule{Selector: "PID-5", Policy: Replace, Class: "names"})
	if err == nil || !strings.Contains(err.Error(), "must declare a replacement") {
		t.Fatalf("a replace rule without text was %v", err)
	}
	if err := ValidateFieldRule(FieldRule{Selector: "not a selector", Policy: Remove, Class: "names"}); err == nil {
		t.Fatal("an unsupported selector was accepted")
	}
}

// A review created under a key derives the case exactly as a Deriver under
// the same key derives each of its messages, so what a check runs is what a
// share previewed.
func TestAKeyedReviewDerivesTheMessagesADeriverPreviews(t *testing.T) {
	request := createRequest(t)
	request.Key = bytes.Repeat([]byte{9}, MinDeriverKeyBytes)
	review, err := Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "ready-for-approval" {
		t.Fatalf("the keyed review is %s: %+v", review.State, review.Findings)
	}
	original, err := bundle.Open(request.CasePath)
	if err != nil {
		t.Fatal(err)
	}
	derivedCase, err := bundle.Open(filepath.Join(request.Output, "case"))
	if err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile(request.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := DecodePolicy(policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	deriver, err := NewDeriver(policy, request.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range original.Events {
		raw, _ := original.Raw(event.ID)
		previewed, _, err := deriver.Message(event.ID, raw, event.Kind, event.Terminator)
		if err != nil {
			t.Fatal(err)
		}
		created, err := derivedCase.Raw(event.ID)
		if err != nil || !bytes.Equal(created, previewed) {
			t.Fatalf("message %s: the review's derived case is not the previewed message", event.ID)
		}
	}
	if _, err := createWithProver(context.Background(), Request{Key: []byte("short")}, &fakeProver{}); err == nil {
		t.Fatal("a short key was accepted")
	}
}
