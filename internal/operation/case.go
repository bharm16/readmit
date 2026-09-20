// Package operation contains application operations shared by the command-line
// and desktop adapters. Presentation and state mapping stay in those adapters;
// verification and admission rules live here once.
package operation

import (
	"errors"

	"github.com/bharm16/readmit/internal/bundle"
)

var (
	ErrCaseUnverified      = errors.New("the case could not be verified as complete, unmodified evidence")
	ErrCaseIdentityChanged = errors.New("the case identity changed; reopen the case")
)

// OpenCase opens one case through the canonical evidence reader.
func OpenCase(path string) (*bundle.Bundle, error) {
	opened, err := bundle.Open(path)
	if err != nil {
		return nil, ErrCaseUnverified
	}
	return opened, nil
}

// OpenVerifiedCase admits only the exact case identity an adapter previously
// displayed or recorded. An absent identity is not an unbound request.
func OpenVerifiedCase(path, expectedIdentity string) (*bundle.Bundle, error) {
	opened, err := OpenCase(path)
	if err != nil {
		return nil, err
	}
	if expectedIdentity == "" || opened.Identity != expectedIdentity {
		return nil, ErrCaseIdentityChanged
	}
	return opened, nil
}
