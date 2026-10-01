package desktop

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// validatorFolder is where this application keeps the validators it
// installed, beside its own documents, one folder per capability identity.
const validatorFolder = "validators"

// ValidatorInstallRequest names the saved FHIR environment whose connection
// will select the installed validator, the package folder the person chose
// and the identity its administrator published. IntentID identifies the
// environment save the installation ends with.
type ValidatorInstallRequest struct {
	Context  RequestContext `json:"context"`
	Ref      ItemRef        `json:"ref"`
	Package  string         `json:"package"`
	Identity string         `json:"identity"`
	IntentID string         `json:"intent_id"`
}

// InstallValidator installs a validator package on this computer, as
// `readmit validator install` does, into this application's own storage,
// and saves the environment's connection selecting it: the package is
// verified offline against the published identity first, the worker image
// is loaded from the package only, and nothing is fetched. It answers the
// check the installed validator then meets.
func (a *App) InstallValidator(request ValidatorInstallRequest) ValidatorCheckResult {
	return runNamed[ValidatorCheckResult, *ValidatorCheckResult](a, profiles["InstallValidator"], func(ctx context.Context) ValidatorCheckResult {
		result := ValidatorCheckResult{Context: request.Context}
		if request.IntentID == "" || len(request.IntentID) > 64 {
			result.refuse(Failed, "a validator change names its submission")
			return result
		}
		_, _, members, declined := a.savedEnvironment(ctx, ItemRequest{Context: request.Context, Ref: request.Ref})
		if members == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if members.fhir == nil {
			result.refuse(Failed, "only a FHIR connection selects a local validator")
			return result
		}
		if !filepath.IsAbs(request.Package) {
			result.refuse(Failed, "choose the validator package with the folder dialog")
			return result
		}
		answer := func(err error) ValidatorCheckResult {
			result.State, result.Check = Completed, checkFailure(err)
			return result
		}
		p, err := fhirvalidator.VerifyPackage(request.Package, request.Identity)
		if err != nil {
			return answer(err)
		}
		validation := ConnectionValidation{Engine: "local"}
		if members.fhir.Validation != nil {
			validation.Socket = members.fhir.Validation.Socket
		}
		output := filepath.Join(a.documents.Folder, validatorFolder, p.Capability.Identity())
		if _, err := fhirvalidator.OpenCapability(output); err != nil {
			if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
				result.refuse(Failed, "this application's storage cannot hold the validator")
				return result
			}
			engine, err := fhirvalidator.LocalEngine(validation.Socket)
			if err != nil {
				return answer(err)
			}
			_, err = engine.InstallPackage(ctx, request.Package, request.Identity, output)
			engine.Close()
			if err != nil {
				return answer(err)
			}
		}
		validation.Capability = output
		connection := *members.fhir
		connection.Validation = &validation
		saved, refused := a.saveConnection(ctx, request.Context, request.Ref, request.IntentID, func(draft *ItemDraft) { draft.FHIR = &connection })
		if refused.state != "" {
			result.refuse(refused.state, refused.reason)
			return result
		}
		result.State, result.Saved, result.Check = Completed, saved, checkValidator(ctx, &validation)
		return result
	})
}

// RemoveValidator saves a saved FHIR environment's connection without the
// validator this application installed for it, and only then removes that
// validator as `readmit validator remove` does — its worker image, unless
// another validator this application installed uses the same image, and its
// folder — so a refused save removes nothing.
// Results that validated with it keep their own copy and stay readable.
func (a *App) RemoveValidator(request ValidatorRemoveRequest) ValidatorCheckResult {
	return runNamed[ValidatorCheckResult, *ValidatorCheckResult](a, profiles["RemoveValidator"], func(ctx context.Context) ValidatorCheckResult {
		result := ValidatorCheckResult{Context: request.Context}
		if request.IntentID == "" || len(request.IntentID) > 64 {
			result.refuse(Failed, "a validator change names its submission")
			return result
		}
		_, _, members, declined := a.savedEnvironment(ctx, ItemRequest{Context: request.Context, Ref: request.Ref})
		if members == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		installed := filepath.Join(a.documents.Folder, validatorFolder)
		if members.fhir == nil || members.fhir.Validation == nil || filepath.Dir(members.fhir.Validation.Capability) != installed {
			result.refuse(Failed, "this connection selects no validator this application installed")
			return result
		}
		validation := *members.fhir.Validation
		keep := []string{}
		entries, _ := os.ReadDir(installed)
		for _, entry := range entries {
			other := filepath.Join(installed, entry.Name())
			if _, err := fhirvalidator.OpenCapability(other); err == nil && other != validation.Capability {
				keep = append(keep, other)
			}
		}
		// The connection stops selecting the validator first, so a refused
		// save leaves both the connection and the installed validator as
		// they were.
		connection := *members.fhir
		connection.Validation = nil
		saved, refused := a.saveConnection(ctx, request.Context, request.Ref, request.IntentID, func(draft *ItemDraft) { draft.FHIR = &connection })
		if refused.state != "" {
			result.refuse(refused.state, refused.reason)
			return result
		}
		result.Saved = saved
		engine, err := fhirvalidator.LocalEngine(validation.Socket)
		if err != nil {
			result.State, result.Check = Completed, checkFailure(err)
			return result
		}
		err = engine.RemoveInstalled(ctx, validation.Capability, keep...)
		engine.Close()
		if err != nil {
			result.State, result.Check = Completed, checkFailure(err)
			return result
		}
		result.State, result.Check = Completed, checkValidator(ctx, nil)
		return result
	})
}

// ValidatorRemoveRequest names the saved FHIR environment whose installed
// validator is removed; IntentID identifies the environment save.
type ValidatorRemoveRequest struct {
	Context  RequestContext `json:"context"`
	Ref      ItemRef        `json:"ref"`
	IntentID string         `json:"intent_id"`
}

// saveConnection saves a new revision of an environment with change applied
// to its current draft, inside the slot its caller holds.
func (a *App) saveConnection(ctx context.Context, context RequestContext, ref ItemRef, intent string, change func(*ItemDraft)) (*ItemRef, refusal) {
	draft := a.openSavedDraft(ctx, context, ref)
	if draft == nil {
		return nil, refusal{Failed, "the environment could not be read again"}
	}
	change(draft)
	answer := a.saveItem(ctx, SaveItemRequest{Context: context, Kind: EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: *draft, IntentID: intent})
	if answer.Outcome != SavedOutcome || answer.Saved == nil {
		reason := answer.Reason
		if reason == "" {
			reason = "the environment was not saved"
		}
		return nil, refusal{Failed, reason}
	}
	return answer.Saved, refusal{}
}
