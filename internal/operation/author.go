package operation

import (
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

var (
	ErrProjectOpen        = errors.New("project could not be opened")
	ErrProjectRevisions   = errors.New("project revisions could not be read")
	ErrProjectNoteInvalid = errors.New("project note is invalid")
	ErrProjectNoteWrite   = errors.New("project note could not be stored")
)

type BaselineRequest struct {
	Spec, Previous, Output      string
	ShowValues                  bool
	Review, Approver, Rationale string
}

type BaselineResult struct {
	Comparison baseline.Comparison
	Previous   *baseline.Revision
	Approved   *baseline.Revision
}

func ReviewBaseline(request BaselineRequest) (BaselineResult, error) {
	return baselineOperation(request, false)
}

func ApproveBaseline(request BaselineRequest) (BaselineResult, error) {
	return baselineOperation(request, true)
}

func baselineOperation(request BaselineRequest, approve bool) (BaselineResult, error) {
	raw, err := baseline.ReadBytes(request.Spec, testrunner.MaxSpecBytes)
	if err != nil {
		return BaselineResult{}, err
	}
	var previous *baseline.Revision
	if request.Previous != "" {
		value, err := baseline.Read(request.Previous)
		if err != nil {
			return BaselineResult{}, err
		}
		previous = &value
	}
	comparison, err := baseline.Review(raw, previous, request.ShowValues)
	if err != nil {
		return BaselineResult{}, err
	}
	result := BaselineResult{Comparison: comparison, Previous: previous}
	if !approve {
		return result, nil
	}
	revision, err := baseline.Approve(raw, previous, request.Review, request.Approver, request.Rationale)
	if err != nil {
		return BaselineResult{}, err
	}
	if err := baseline.Save(request.Output, revision); err != nil {
		return BaselineResult{}, err
	}
	result.Approved = &revision
	return result, nil
}

type ExpectationRequest struct {
	ID, Spec, Previous, Output  string
	Profiles                    []string
	ShowValues                  bool
	Review, Approver, Rationale string
}

type ExpectationResult struct {
	Comparison expectation.Comparison
	Previous   *expectation.Release
	Approved   *expectation.Release
}

func ReviewExpectation(request ExpectationRequest) (ExpectationResult, error) {
	return expectationOperation(request, false)
}

func ApproveExpectation(request ExpectationRequest) (ExpectationResult, error) {
	return expectationOperation(request, true)
}

func expectationOperation(request ExpectationRequest, approve bool) (ExpectationResult, error) {
	raw, err := baseline.ReadBytes(request.Spec, testrunner.MaxSpecBytes)
	if err != nil {
		return ExpectationResult{}, err
	}
	pins, err := expectation.ReadProfiles(request.Profiles)
	if err != nil {
		return ExpectationResult{}, err
	}
	var previous *expectation.Release
	if request.Previous != "" {
		value, err := expectation.Read(request.Previous)
		if err != nil {
			return ExpectationResult{}, err
		}
		previous = &value
	}
	comparison, err := expectation.Review(request.ID, raw, pins, previous, request.ShowValues)
	if err != nil {
		return ExpectationResult{}, err
	}
	result := ExpectationResult{Comparison: comparison, Previous: previous}
	if !approve {
		return result, nil
	}
	release, err := expectation.Approve(request.ID, raw, pins, previous, request.Review, request.Approver, request.Rationale)
	if err != nil {
		return ExpectationResult{}, err
	}
	if err := expectation.Save(request.Output, release); err != nil {
		return ExpectationResult{}, err
	}
	result.Approved = &release
	return result, nil
}

type ProjectNoteResult struct {
	Root      string
	Revisions project.Revisions
	Stored    project.Note
}

func SetProjectNote(path string, note project.Note) (ProjectNoteResult, error) {
	result := ProjectNoteResult{Root: path}
	opened, err := project.Open(path)
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrProjectOpen, err)
	}
	result.Root = opened.Root
	revisions, err := project.ReadRevisions(opened.Root)
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrProjectRevisions, err)
	}
	updated, stored, err := project.SetNote(opened.Document, revisions, note)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrProjectNoteInvalid, err)
	}
	if err := project.WriteRevisions(opened.Root, updated); err != nil {
		return result, fmt.Errorf("%w: %v", ErrProjectNoteWrite, err)
	}
	return ProjectNoteResult{Root: opened.Root, Revisions: updated, Stored: stored}, nil
}

type TransformRequest struct {
	Case  string
	Rules []byte
	Plan  []byte
	Pack  *profilepack.Pack
}

func PreviewTransform(request TransformRequest) (transform.Preview, error) {
	rules, err := correlate.ParseRules(request.Rules)
	if err != nil {
		return transform.Preview{}, err
	}
	plan, err := transform.DecodePlan(request.Plan)
	if err != nil {
		return transform.Preview{}, err
	}
	return transform.Run(request.Case, plan, rules, request.Pack)
}
