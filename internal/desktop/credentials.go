package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/secret"
)

// A project's credentials are the references its secrets entry registers,
// managed by name. A reference names where a credential is kept and how it
// is read; the credential itself is never here, and a reference's locator
// arguments, the one place a credential could have been put, are counted and
// never answered once stored.

// CredentialRow is one registered reference as a list shows it. It never
// carries the locator's arguments, only how many there are. RotatedAt is
// RFC 3339. Bindable says an environment can present it: it was registered
// for an MLLP endpoint.
type CredentialRow struct {
	Name          string               `json:"name"`
	Purpose       secret.Purpose       `json:"purpose"`
	Store         secret.Store         `json:"store"`
	Address       string               `json:"address"`
	Command       string               `json:"command"`
	ArgumentCount int                  `json:"argument_count"`
	MaxAge        string               `json:"max_age,omitzero"`
	Generation    int                  `json:"generation"`
	RotatedAt     string               `json:"rotated_at"`
	Rotation      secret.RotationState `json:"rotation"`
	Bindable      bool                 `json:"bindable"`
}

// CredentialsResult answers a read or a change of the project's credentials
// with every reference as it is registered now. Referring names what still
// uses a reference a removal was refused for.
type CredentialsResult struct {
	State       State           `json:"state"`
	Reason      string          `json:"reason,omitzero"`
	Context     RequestContext  `json:"context"`
	Credentials []CredentialRow `json:"credentials"`
	Referring   []Referrer      `json:"referring"`
}

func (r *CredentialsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CredentialRequest names one reference of the open project.
type CredentialRequest struct {
	Context RequestContext `json:"context"`
	Name    string         `json:"name"`
}

// CredentialSaveRequest is one Save of a reference's sheet. Update edits the
// reference registered under Name, whose name and purpose never change;
// otherwise a reference is registered under Name for Purpose. An edit
// replaces the locator's arguments only when ReplaceArguments is set, and
// then with exactly Arguments, so an empty list clears them; a new reference
// is registered with Arguments.
type CredentialSaveRequest struct {
	Context          RequestContext `json:"context"`
	Name             string         `json:"name"`
	Update           bool           `json:"update"`
	Purpose          secret.Purpose `json:"purpose,omitzero"`
	Store            secret.Store   `json:"store"`
	Address          string         `json:"address"`
	Command          string         `json:"command"`
	Arguments        []string       `json:"arguments,omitzero"`
	ReplaceArguments bool           `json:"replace_arguments"`
	MaxAge           string         `json:"max_age,omitzero"`
}

// CredentialCheckResult says whether a reference's locator resolved. Nothing
// it resolved is kept or answered.
type CredentialCheckResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Name     string         `json:"name"`
	Resolved bool           `json:"resolved"`
}

func (r *CredentialCheckResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// credentialRows lists a document's references as rows, in its order.
func (a *App) credentialRows(document secret.Document) []CredentialRow {
	rows := make([]CredentialRow, 0, len(document.References))
	for _, reference := range document.References {
		rows = append(rows, CredentialRow{Name: reference.Name, Purpose: reference.Purpose, Store: reference.Store, Address: reference.Address,
			Command: reference.Command, ArgumentCount: len(reference.Arguments), MaxAge: reference.MaxAge, Generation: reference.Generation,
			RotatedAt: catalog.Stamp(reference.RotatedAt), Rotation: reference.Rotation(a.now()), Bindable: reference.Purpose == secret.MLLPEndpoint})
	}
	return rows
}

// projectSecrets resolves the open project's secrets entry.
func (a *App) projectSecrets(ctx context.Context, request RequestContext) (string, string, refusal) {
	root, declined := a.projectRoot(ctx, request)
	if root == "" {
		return "", "", declined
	}
	return root, filepath.Join(root, ProjectSecrets), refusal{}
}

// ListCredentials lists the references the open project registers. A project
// that registers none lists nothing. It is a read.
func (a *App) ListCredentials(request ItemRequest) CredentialsResult {
	return run(a, false, false, func(ctx context.Context) CredentialsResult {
		result := CredentialsResult{Context: request.Context, Credentials: []CredentialRow{}, Referring: []Referrer{}}
		_, path, declined := a.projectSecrets(ctx, request.Context)
		if path == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		document, err := operation.OpenOrEmptySecrets(path)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Credentials = Completed, a.credentialRows(document)
		if len(result.Credentials) == 0 {
			result.State = Empty
		}
		return result
	})
}

// SaveCredential registers or edits one reference of the open project
// through the shared operations behind `readmit secret add` and
// `readmit secret update`, refused for what they refuse. An edit writes only
// what changed from the reference as registered now; an edit that changes
// nothing writes nothing.
func (a *App) SaveCredential(request CredentialSaveRequest) CredentialsResult {
	return run(a, false, true, func(ctx context.Context) CredentialsResult {
		result := CredentialsResult{Context: request.Context, Credentials: []CredentialRow{}, Referring: []Referrer{}}
		_, path, declined := a.projectSecrets(ctx, request.Context)
		if path == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		var document secret.Document
		var err error
		if !request.Update {
			document, _, err = operation.AddSecretReference(path, secret.Reference{Name: request.Name, Store: request.Store, Purpose: request.Purpose,
				Address: request.Address, Command: request.Command, Arguments: slices.Clone(request.Arguments), MaxAge: request.MaxAge})
		} else {
			document, err = operation.ReadSecrets(path)
			var current secret.Reference
			if err == nil {
				current, err = secret.Find(document, request.Name)
			}
			if err == nil && request.Purpose != "" && request.Purpose != current.Purpose {
				err = errors.New("a reference's purpose never changes; a credential for another purpose is a different reference")
			}
			if err == nil {
				change := credentialChange(current, request)
				if !change.Empty() {
					if change.Arguments != nil && slices.Contains(*change.Arguments, "") {
						err = errors.New("a locator argument is never empty; an empty list clears them")
					} else {
						document, _, err = operation.UpdateSecretReference(path, request.Name, change)
					}
				}
			}
		}
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Credentials = Completed, a.credentialRows(document)
		return result
	})
}

// credentialChange is what an edit changes from the reference as registered.
func credentialChange(current secret.Reference, request CredentialSaveRequest) secret.Change {
	change := secret.Change{}
	if request.Store != current.Store {
		change.Store = &request.Store
	}
	if request.Address != current.Address {
		change.Address = &request.Address
	}
	if request.Command != current.Command {
		change.Command = &request.Command
	}
	if request.MaxAge != current.MaxAge {
		change.MaxAge = &request.MaxAge
	}
	if request.ReplaceArguments {
		arguments := slices.Clone(request.Arguments)
		if arguments == nil {
			arguments = []string{}
		}
		change.Arguments = &arguments
	}
	return change
}

// CheckCredential runs one reference's locator deliberately and says whether
// it resolved a credential, which is dropped at once.
func (a *App) CheckCredential(request CredentialRequest) CredentialCheckResult {
	return runNamed[CredentialCheckResult, *CredentialCheckResult](a, profiles["CheckCredential"], func(ctx context.Context) CredentialCheckResult {
		result := CredentialCheckResult{Context: request.Context, Name: request.Name}
		_, path, declined := a.projectSecrets(ctx, request.Context)
		if path == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		document, err := operation.ReadSecrets(path)
		var reference secret.Reference
		if err == nil {
			reference, err = secret.Find(document, request.Name)
		}
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		if err := operation.TestSecretReference(ctx, reference); err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Resolved = Completed, true
		return result
	})
}

// RecordCredentialRotation records that the credential a reference names
// was rotated in its store, once its locator resolves: the generation moves
// on and the rotation time is now. It rotates nothing itself.
func (a *App) RecordCredentialRotation(request CredentialRequest) CredentialsResult {
	return runNamed[CredentialsResult, *CredentialsResult](a, profiles["RecordCredentialRotation"], func(ctx context.Context) CredentialsResult {
		result := CredentialsResult{Context: request.Context, Credentials: []CredentialRow{}, Referring: []Referrer{}}
		_, path, declined := a.projectSecrets(ctx, request.Context)
		if path == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		document, _, err := operation.RotateSecretReference(ctx, path, request.Name)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Credentials = Completed, a.credentialRows(document)
		return result
	})
}

// RemoveCredential removes one reference from the project's secrets entry.
// It is refused, naming them, while an environment's current revision
// presents it; the credential in its store is never touched.
func (a *App) RemoveCredential(request CredentialRequest) CredentialsResult {
	return run(a, false, true, func(ctx context.Context) CredentialsResult {
		result := CredentialsResult{Context: request.Context, Credentials: []CredentialRow{}, Referring: []Referrer{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		for _, item := range loaded.document.Items {
			if item.Kind != string(EnvironmentItem) || loaded.removed(item) {
				continue
			}
			target, err := loaded.targetOf(item)
			if err == nil && target.Credential.Reference == request.Name && filepath.Clean(target.Credential.SecretsFile) == ProjectSecrets {
				result.Referring = append(result.Referring, Referrer{Ref: ItemRef{Kind: EnvironmentItem, ID: item.ID, Revision: item.RevisionLabel()}, Name: loaded.read(item).Name})
			}
		}
		if len(result.Referring) > 0 {
			result.refuse(Failed, "an environment presents this credential; choose another credential there first. Nothing was removed")
			return result
		}
		document, err := operation.RemoveSecretReference(filepath.Join(loaded.root, ProjectSecrets), request.Name)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		result.State, result.Credentials = Completed, a.credentialRows(document)
		return result
	})
}
