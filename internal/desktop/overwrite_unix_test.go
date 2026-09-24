//go:build !windows

package desktop_test

// A save that may overwrite its output replaces the entry at the output name
// and never writes into what is there. Whatever is planted at that name — a
// symbolic link out of the workspace or to one of its own entries, a hard link
// to a file elsewhere, a FIFO — is itself replaced, so the file it led to keeps
// its bytes; a folder there, or anything where the replacement is written
// first, is refused. A regular file at the name is replaced with the same bytes
// the save has always written there.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

// victim is what the file outside the workspace holds before any save.
const victim = "synthetic document outside the workspace; a save must never change it\n"

// overwriteSave is one save that may overwrite its output. prepare puts the
// documents the save reads into a workspace, and save calls it with the output
// named output.
type overwriteSave struct {
	name    string
	output  string
	prepare func(t *testing.T, root string)
	save    func(app *desktop.App, root, output string) saved
}

// saved is what one save answered, and report what it said it saved, which
// must be the same over a regular file and over every entry it replaces.
type saved struct {
	state  desktop.State
	reason string
	report any
}

// hostileOutputs plants one entry at output for each way it can lead
// elsewhere or stand in the way. Each link leads to victim, outside the
// workspace, or to own, the workspace's own document. The save must replace
// the entry, or, when refusal names a sentence, refuse with it.
var hostileOutputs = []struct {
	how     string
	plant   func(root, outside, output, own string) error
	refusal string
}{
	{"a symbolic link out of the workspace", func(root, outside, output, _ string) error {
		return os.Symlink(filepath.Join(outside, "victim.json"), filepath.Join(root, output))
	}, ""},
	{"a symbolic link to an entry of the workspace", func(root, _, output, own string) error {
		return os.Symlink(filepath.Join(root, own), filepath.Join(root, output))
	}, ""},
	{"a hard link to a file outside the workspace", func(root, outside, output, _ string) error {
		return os.Link(filepath.Join(outside, "victim.json"), filepath.Join(root, output))
	}, ""},
	{"a FIFO", func(root, _, output, _ string) error {
		return syscall.Mkfifo(filepath.Join(root, output), 0o600)
	}, ""},
	{"a folder", func(root, _, output, _ string) error {
		return os.Mkdir(filepath.Join(root, output), 0o700)
	}, "cannot write destination file"},
	{"the partial file an interrupted save left", func(root, _, output, _ string) error {
		return os.WriteFile(filepath.Join(root, output+".incomplete"), []byte("an interrupted save\n"), 0o600)
	}, "cannot replace destination file; an interrupted write is retained beside it"},
	{"a symbolic link out of the workspace where the replacement is written", func(root, outside, output, _ string) error {
		return os.Symlink(filepath.Join(outside, "victim.json"), filepath.Join(root, output+".incomplete"))
	}, "cannot replace destination file; an interrupted write is retained beside it"},
}

// overwriteWorkspaces returns a prepared workspace and a folder beside it
// holding the victim document.
func overwriteWorkspaces(t *testing.T, prepare func(*testing.T, string)) (root, outside string) {
	t.Helper()
	parent := t.TempDir()
	for _, folder := range []string{"workspace", "outside"} {
		if err := os.Mkdir(filepath.Join(parent, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, outside = resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
	writeDocument(t, outside, "victim.json", victim)
	if prepare != nil {
		prepare(t, root)
	}
	return root, outside
}

// ownerOnlyBytes reads the file at path, failing the test unless the entry
// there is one regular file only its owner can read.
func ownerOnlyBytes(t *testing.T, path string) []byte {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file: %v %v", filepath.Base(path), info, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s is %v, want owner-only", filepath.Base(path), info.Mode().Perm())
	}
	return mustRead(t, path)
}

// overwritesReplaceTheEntry drives each save over a regular file, then over
// every hostile entry at its output name, and holds it to the rule.
func overwritesReplaceTheEntry(t *testing.T, saves []overwriteSave) {
	t.Helper()
	for _, s := range saves {
		// Over a regular file the save completes and leaves the bytes it
		// reported, owner-only, with nothing else beside them.
		regularRoot, _ := overwriteWorkspaces(t, s.prepare)
		writeDocument(t, regularRoot, s.output, "the previous document at this name\n")
		regularListed := entriesOf(t, regularRoot)
		regular := s.save(workspaceApp(t), regularRoot, s.output)
		if regular.state != desktop.Completed {
			t.Fatalf("%s over a regular file: %+v", s.name, regular)
		}
		want := ownerOnlyBytes(t, filepath.Join(regularRoot, s.output))
		if after := entriesOf(t, regularRoot); !reflect.DeepEqual(regularListed, after) {
			t.Errorf("%s over a regular file left %v, was %v", s.name, after, regularListed)
		}

		for _, hostile := range hostileOutputs {
			call := s.name + " over " + hostile.how
			root, outside := overwriteWorkspaces(t, s.prepare)
			own := "own.json"
			writeDocument(t, root, own, "the workspace's own document\n")
			if err := hostile.plant(root, outside, s.output, own); err != nil {
				t.Fatal(err)
			}
			listed, before, ownBefore := entriesOf(t, root), bytesUnder(t, outside), ownerOnlyBytes(t, filepath.Join(root, own))
			app := workspaceApp(t)
			got := answeredWithin(t, call, func() saved { return s.save(app, root, s.output) })
			if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
				t.Errorf("%s changed the file outside the workspace", call)
			}
			if ownAfter := ownerOnlyBytes(t, filepath.Join(root, own)); string(ownAfter) != string(ownBefore) {
				t.Errorf("%s changed another entry of the workspace", call)
			}
			// Replaced or refused, the workspace holds the same names: no
			// partial replacement is left beside the entry, and a refusal
			// leaves what was there.
			if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
				t.Errorf("%s left %v, was %v", call, after, listed)
			}
			if hostile.refusal != "" {
				if got.state != desktop.Failed || got.reason != hostile.refusal {
					t.Errorf("%s: %+v, want the refusal %q", call, got, hostile.refusal)
				}
				continue
			}
			if got.state != desktop.Completed {
				t.Errorf("%s: %+v, want the entry replaced", call, got)
				continue
			}
			if written := ownerOnlyBytes(t, filepath.Join(root, s.output)); string(written) != string(want) {
				t.Errorf("%s saved other bytes than it saves over a regular file", call)
			}
			if !reflect.DeepEqual(got.report, regular.report) {
				t.Errorf("%s reported %+v, want what it reports over a regular file: %+v", call, got.report, regular.report)
			}
		}
	}
}

// The two saves that may overwrite a workspace entry: upgrading one saved
// test's profile pin, and saving a scenario library entry into the library it
// was read from. The library is read by its name trimmed of surrounding
// spaces and saved under the name as given, so a name that differs only by a
// trailing space reads the regular library and saves over whatever is at the
// untrimmed name.
func TestOverwritingAWorkspaceEntryReplacesItAndNeverWritesThroughIt(t *testing.T) {
	generator := string(mustRead(t, filepath.Join("..", "..", "testdata", "fixtures", "scenario-generator.json")))
	overwritesReplaceTheEntry(t, []overwriteSave{
		{
			name:   "UpgradeProfilePin",
			output: "pinned.json",
			prepare: func(t *testing.T, root string) {
				writeDocument(t, root, "references.json", string(mustRead(t, filepath.Join("..", "..", "testdata", "fixtures", "profile-references.json"))))
			},
			save: func(app *desktop.App, root, output string) saved {
				result := app.UpgradeProfilePin(desktop.ProfileUpgradePinRequest{
					Workspace: root, References: "references.json", Test: "test-reschedule.json",
					WasPin: profileversion.Pin{ID: "fixture-local-siu", Version: "1", SHA256: "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054"},
					NowPin: profileversion.Pin{ID: "fixture-local-siu", Version: "2", SHA256: "4444444444444444444444444444444444444444444444444444444444444444"},
					Output: output,
				})
				if result.State == desktop.Completed {
					// The bytes the save has always written are the upgraded
					// index's own encoding.
					encoded, err := result.References.Encode()
					if err != nil {
						return saved{desktop.Failed, err.Error(), nil}
					}
					if written, err := os.ReadFile(filepath.Join(root, output)); err != nil || string(written) != string(encoded) {
						return saved{desktop.Failed, "the saved index is not the upgraded index's encoding", nil}
					}
				}
				return saved{result.State, result.Reason, result.References}
			},
		},
		{
			name:   "SaveScenarioLibraryEntry",
			output: "library.json ",
			prepare: func(t *testing.T, root string) {
				writeDocument(t, root, "library.json", string(mustRead(t, filepath.Join("..", "..", "testdata", "fixtures", "scenario-library.json"))))
			},
			save: func(app *desktop.App, root, output string) saved {
				request := desktop.ScenarioLibraryRequest{
					Workspace: root, Library: output, TemplateID: "siu-appointment-lifecycle", TemplateVer: "1",
					Profile: "readmit-siu-lifecycle-v1", Plan: generator, Coverage: "baseline",
				}
				// The same entry saved as a new library is written by the
				// exclusive writer this change leaves alone: the overwrite
				// must write exactly those bytes.
				request.Output = "fresh.json"
				fresh := app.SaveScenarioLibraryEntry(request)
				request.Output = ""
				result := app.SaveScenarioLibraryEntry(request)
				if fresh.State != desktop.Completed {
					return saved{fresh.State, fresh.Reason, nil}
				}
				defer os.Remove(filepath.Join(root, "fresh.json"))
				if result.State == desktop.Completed {
					created, _ := os.ReadFile(filepath.Join(root, "fresh.json"))
					if written, err := os.ReadFile(filepath.Join(root, output)); err != nil || string(written) != string(created) {
						return saved{desktop.Failed, "the overwritten library is not the library saved as a new entry", nil}
					}
				}
				return saved{result.State, result.Reason, result.Templates}
			},
		},
	})
}

// replacingSave is one save that writes over the document at its name by
// renaming a complete new file onto it. create writes the document the first
// time, into the folder outside the workspace; prepare, when set, puts what
// else the save needs into the workspace; replace saves over the document. A
// save whose reader refuses a symbolic link before anything is written names
// that refusal in linkRefusal.
type replacingSave struct {
	name        string
	file        string
	create      func(app *desktop.App, folder, file string) saved
	prepare     func(t *testing.T, root string)
	replace     func(app *desktop.App, folder, file string) saved
	linkRefusal string
}

// Every other save that may write over an existing document. Each is handed
// a symbolic link and a hard link, at the name it writes, to a document of
// its own kind outside the workspace that it would really read and replace,
// and must leave that document's bytes as they were: a save that completes has
// replaced the entry at the name with a regular file of its own.
func TestEverySaveThatReplacesADocumentLeavesTheFileALinkLedToUnchanged(t *testing.T) {
	reference := secret.Reference{
		Name: "vault-key", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint,
		Address: "127.0.0.1:2575", Command: "/bin/echo", Arguments: []string{"test-only-credential"}, MaxAge: "720h",
	}
	saveSecret := func(app *desktop.App, folder, file string, change func(*secret.Reference)) saved {
		declared := reference
		if change != nil {
			change(&declared)
		}
		result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: folder, SecretsFile: file, Reference: declared})
		return saved{result.State, result.Reason, result.Document}
	}
	createSecrets := func(app *desktop.App, folder, file string) saved { return saveSecret(app, folder, file, nil) }
	saveTarget := func(app *desktop.App, folder, file, name string) saved {
		opened := app.ReadTarget(folder, "absent-target.json")
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		declared := *opened.Target
		declared.Name, declared.Classification, declared.Address, declared.ApprovedTransport = name, "nonproduction", "127.0.0.1:2575", true
		result := app.SaveTarget(desktop.TargetSaveRequest{Workspace: folder, TargetFile: file, Target: declared})
		return saved{result.State, result.Reason, result.Target}
	}
	savePolicy := func(app *desktop.App, folder, file, destination string) saved {
		result := app.SaveSendPolicy(desktop.SendPolicySaveRequest{Workspace: folder, PolicyFile: file, Policy: sendpolicy.Policy{
			Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{destination},
		}})
		return saved{result.State, result.Reason, result.Policy}
	}
	savePlan := func(app *desktop.App, folder, file, environment string) saved {
		result := app.SaveResetPlan(desktop.ResetPlanSaveRequest{Workspace: folder, PlanFile: file, Plan: fixturereset.Plan{
			Schema: fixturereset.PlanSchema, Environment: environment,
			Actions: []fixturereset.Action{{ID: "confirm", Operator: fixturereset.OperatorConfirms, Authority: fixturereset.NoAuthority, Instructions: "Confirm the reset"}},
		}})
		return saved{result.State, result.Reason, result.Plan}
	}
	saveWindow := func(app *desktop.App, folder, file, declared string) saved {
		opened := app.OpenObservationWindow(folder, "absent-window.json")
		if declared != "" {
			writeDocument(t, folder, "declared-window.json", declared)
			opened = app.OpenObservationWindow(folder, "declared-window.json")
		}
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		result := app.SaveObservationWindow(desktop.ObservationWindowRequest{Workspace: folder, WindowFile: file, Window: opened.Window})
		return saved{result.State, result.Reason, result.Identity}
	}
	saveSource := func(app *desktop.App, folder, file, declared string) saved {
		opened := app.OpenObservationSource(folder, "absent-source.json")
		if declared != "" {
			writeDocument(t, folder, "declared-source.json", declared)
			opened = app.OpenObservationSource(folder, "declared-source.json")
		}
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		result := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: folder, SourceFile: file, Source: opened.Source})
		return saved{result.State, result.Reason, result.Identity}
	}
	saveRegistration := func(app *desktop.App, folder, file, scope string) saved {
		result := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: folder, SourceFile: file, Source: evidencesource.Source{
			Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory, Scope: scope, Root: folder,
			Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
			Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
		}})
		return saved{result.State, result.Reason, result.Source}
	}
	saveResponder := func(app *desktop.App, folder, file, name string) saved {
		result := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: folder, PolicyFile: file, Policy: collection.Policy{
			Schema: collection.PolicySchema, Name: name, SourceLabel: "downstream",
			Acknowledgement:      collection.AckRule{Operator: collection.FixedCodeOperator, Code: collection.AcceptCode},
			AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}},
			Enhanced: &collection.EnhancedRule{Operator: collection.EnhancedFixedCodes, AcceptCode: collection.CommitAcceptCode,
				ApplicationCode: collection.AcceptCode, ApplicationDelivery: collection.SameConnection},
		}})
		return saved{result.State, result.Reason, result.Policy}
	}

	// A project's documents have fixed names in its folder: the project
	// document, the editable revisions document a note and a revision are
	// kept in, and the quota. Each is read through the folder itself, which
	// refuses a symbolic link out of it before anything is written.
	aProject := func(t *testing.T, folder string) { writeProject(t, folder, registeredRegression) }
	createProject := func(app *desktop.App, folder, _ string) saved {
		aProject(t, folder)
		return saved{desktop.Completed, "", nil}
	}
	saveNote := func(app *desktop.App, folder, title string) saved {
		result := app.SaveNote(folder, project.Note{Name: "triage", Subject: "regression", Title: title, Body: "Synthetic working theory."})
		return saved{result.State, result.Reason, nil}
	}
	createNote := func(app *desktop.App, folder, _ string) saved {
		aProject(t, folder)
		return saveNote(app, folder, "Outside theory")
	}
	setQuota := func(app *desktop.App, folder string, files int) saved {
		result := app.SetProjectQuota(desktop.ProjectQuotaChange{Project: folder, MaxBytes: 50_000_000, MaxFiles: files})
		return saved{result.State, result.Reason, result.Quota}
	}
	// A recovery copy of an earlier project document, which recovering
	// writes back over the current one.
	earlier := mustRead(t, filepath.Join(writeProject(t, t.TempDir(), ""), project.DocumentName))
	sum := sha256.Sum256(earlier)
	earlierDigest := hex.EncodeToString(sum[:])
	aRecoveryCopy := func(t *testing.T, root string) {
		writeDocument(t, root, project.DocumentName+".recovery-"+earlierDigest, string(earlier))
	}
	// A registered incident and a reproducer built from it, which
	// registering a revision records in the revisions document.
	aBuiltReproducer := func(t *testing.T, root string) {
		writeProject(t, root, "")
		app := workspaceApp(t)
		incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
		if registered := app.RegisterCase(root, "incident", desktop.CaseRegistration{Title: "Original incident"}); registered.State != desktop.Completed {
			t.Fatalf("register the incident: %+v", registered)
		}
		selected := app.EditReproducer(desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: incident.Identity,
			Step: reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID}})
		if selected.Reproducer == nil {
			t.Fatalf("select: %+v", selected)
		}
		if built := app.BuildReproducer(desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: incident.Identity,
			Plan: selected.Reproducer.Plan, Output: "incident-reproducer"}); built.State != desktop.Completed {
			t.Fatalf("build: %+v", built)
		}
	}
	aCase := func(t *testing.T, folder string) { writeCase(t, folder, "case", framed(gridBooking)) }
	buildIndex := func(app *desktop.App, folder, file string, replace bool) saved {
		result := app.BuildIndex(desktop.BuildIndexRequest{Workspace: folder, Case: "case", Output: file,
			Fields: []string{"PID-5"}, Retention: "values", RetainUntil: "indefinite", Replace: replace})
		return saved{result.State, result.Reason, nil}
	}
	projectUnread := "the folder holds no project document this release reads"
	projectDocumentUnread := "directory holds no readable project document"
	revisionsUnread := "the editable project document cannot be read"

	replacesALinkedDocument(t, []replacingSave{
		{name: "SaveTarget", file: "target.json",
			create: func(app *desktop.App, folder, file string) saved { return saveTarget(app, folder, file, "outside-lab") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveTarget(app, folder, file, "workspace-lab")
			}},
		{name: "SaveSecretReference(add)", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				return saveSecret(app, folder, file, func(r *secret.Reference) { r.Name = "second-key" })
			}},
		{name: "SaveSecretReference(update)", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				address := "127.0.0.1:2576"
				result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: folder, SecretsFile: file, Reference: reference,
					IsUpdate: true, Change: &desktop.SecretChange{Address: &address}})
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "RotateSecretReference", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				result := app.RotateSecretReference(folder, file, "vault-key")
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "RemoveSecretReference", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				result := app.RemoveSecretReference(folder, file, "vault-key")
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "SaveSendPolicy", file: "send-policy.json",
			create: func(app *desktop.App, folder, file string) saved { return savePolicy(app, folder, file, "10.0.0.0/16") },
			replace: func(app *desktop.App, folder, file string) saved {
				return savePolicy(app, folder, file, "127.0.0.1/32")
			}},
		{name: "SaveResetPlan", file: "reset-plan.json",
			create:  func(app *desktop.App, folder, file string) saved { return savePlan(app, folder, file, "outside-lab") },
			replace: func(app *desktop.App, folder, file string) saved { return savePlan(app, folder, file, "workspace-lab") }},
		{name: "SaveObservationWindow", file: "window.json",
			create: func(app *desktop.App, folder, file string) saved { return saveWindow(app, folder, file, "") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveWindow(app, folder, file, facadeWindowDocument)
			}},
		{name: "SaveObservationSource", file: "source.json",
			create: func(app *desktop.App, folder, file string) saved { return saveSource(app, folder, file, "") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveSource(app, folder, file, facadeSourceDocument)
			}},
		{name: "SaveSourceRegistration", file: "registration.json",
			create: func(app *desktop.App, folder, file string) saved {
				return saveRegistration(app, folder, file, "outside")
			},
			replace: func(app *desktop.App, folder, file string) saved {
				return saveRegistration(app, folder, file, "appointments")
			}},
		{name: "SaveReceiverPolicy", file: "responder.json",
			create: func(app *desktop.App, folder, file string) saved {
				return saveResponder(app, folder, file, "outside-sink")
			},
			replace: func(app *desktop.App, folder, file string) saved { return saveResponder(app, folder, file, "sink") }},
		{name: "UpdateProjectSettings", file: project.DocumentName, linkRefusal: projectUnread, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				title := "Retitled in the workspace"
				result := app.UpdateProjectSettings(folder, desktop.SettingsChange{Title: &title})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "UpdateRegisteredCase", file: project.DocumentName, linkRefusal: projectUnread, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				status := project.StatusResolved
				result := app.UpdateRegisteredCase(folder, "regression", desktop.CaseChange{Status: &status})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "RegisterCase", file: project.DocumentName, linkRefusal: projectUnread, prepare: aCase, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				result := app.RegisterCase(folder, "case", desktop.CaseRegistration{Title: "Registered in the workspace"})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "RecoverProjectDocument", file: project.DocumentName, linkRefusal: projectUnread, prepare: aRecoveryCopy, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				result := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: folder, Document: project.DocumentName, Digest: earlierDigest})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "SaveNote", file: project.RevisionsDocumentName, linkRefusal: revisionsUnread, prepare: aProject, create: createNote,
			replace: func(app *desktop.App, folder, _ string) saved { return saveNote(app, folder, "Workspace theory") }},
		{name: "RegisterRevision", file: project.RevisionsDocumentName, linkRefusal: revisionsUnread, prepare: aBuiltReproducer, create: createNote,
			replace: func(app *desktop.App, folder, _ string) saved {
				result := app.RegisterRevision(desktop.RevisionRegistration{Workspace: folder, Source: "incident-reproducer", Name: "incident-revision", Parent: "incident"})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "SetProjectQuota", file: project.QuotaDocumentName, linkRefusal: projectDocumentUnread, prepare: aProject,
			create: func(app *desktop.App, folder, _ string) saved {
				aProject(t, folder)
				return setQuota(app, folder, 10_000)
			},
			replace: func(app *desktop.App, folder, _ string) saved { return setQuota(app, folder, 20_000) }},
		// Rebuilding an index removes the one it replaces and creates the new
		// one exclusively; it refuses anything but a regular file there.
		{name: "BuildIndex(Replace)", file: "case.index.json", linkRefusal: "an index destination must be a regular file", prepare: aCase,
			create: func(app *desktop.App, folder, file string) saved {
				aCase(t, folder)
				return buildIndex(app, folder, file, false)
			},
			replace: func(app *desktop.App, folder, file string) saved { return buildIndex(app, folder, file, true) }},
	})
}

func replacesALinkedDocument(t *testing.T, saves []replacingSave) {
	t.Helper()
	links := []struct {
		how      string
		symbolic bool
		plant    func(target, link string) error
	}{
		{"a symbolic link out of the workspace", true, os.Symlink},
		{"a hard link to a file outside the workspace", false, os.Link},
	}
	for _, s := range saves {
		for _, link := range links {
			call := s.name + " over " + link.how
			root, outside := overwriteWorkspaces(t, s.prepare)
			app := workspaceApp(t)
			if created := s.create(app, outside, s.file); created.state != desktop.Completed {
				t.Fatalf("%s cannot write the document outside, so it cannot show a replacement: %+v", s.name, created)
			}
			if err := link.plant(filepath.Join(outside, s.file), filepath.Join(root, s.file)); err != nil {
				t.Fatal(err)
			}
			before := bytesUnder(t, outside)
			got := answeredWithin(t, call, func() saved { return s.replace(app, root, s.file) })
			if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
				t.Errorf("%s changed the document outside the workspace", call)
			}
			if s.linkRefusal != "" && link.symbolic {
				if got.state != desktop.Failed || got.reason != s.linkRefusal {
					t.Errorf("%s: %+v, want the refusal %q", call, got, s.linkRefusal)
				}
				continue
			}
			if got.state != desktop.Completed {
				t.Errorf("%s: %+v, want the entry replaced", call, got)
				continue
			}
			entry, err := os.Lstat(filepath.Join(root, s.file))
			linked, _ := os.Stat(filepath.Join(outside, s.file))
			if err != nil || !entry.Mode().IsRegular() || os.SameFile(entry, linked) {
				t.Errorf("%s left %v at the name, want a regular file of its own", call, entry)
			}
			if _, err := os.Lstat(filepath.Join(root, s.file+".incomplete")); !os.IsNotExist(err) {
				t.Errorf("%s left its replacement's partial file behind", call)
			}
		}
	}
}

// The shell's own documents are written over the same way: the saved
// filters, the working session, the editor drafts and the operation,
// commercial and hub selections through the replacement a workspace save
// uses, and the recent workspaces through their own rename. A link at each
// file, to the same document another shell wrote outside, is replaced, and the
// document it led to keeps its bytes.
func TestTheShellsOwnDocumentsReplaceALinkAtTheirFile(t *testing.T) {
	workspace, outside, ownState := t.TempDir(), t.TempDir(), t.TempDir()
	documents := []string{"recent.json", "filters.json", "session.json", "drafts.json", "selection.json", "commercial.json", "hub.json"}
	configuration := t.TempDir()
	writeDocument(t, configuration, "destinations.json",
		`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test"}`)
	hubConfig := writeHubClientConfig(t, configuration, "https://hub.example.test")
	completes := func(call string, got desktop.State, reason string) {
		t.Helper()
		// An empty workspace is opened, and remembered, as empty.
		if got != desktop.Completed && got != desktop.Empty {
			t.Fatalf("%s: %s %s", call, got, reason)
		}
	}
	// shell wires a window over the state files in folder and selects the
	// test operation policy, which retains the selection there.
	shell := func(folder string) *desktop.App {
		at := func(name string) string { return filepath.Join(folder, name) }
		choose := &chooser{files: []string{filepath.Join(configuration, "destinations.json")}}
		app := desktop.NewWithOperationSelection(choose, at("recent.json"), at("filters.json"), at("session.json"), at("drafts.json"), at("selection.json"))
		selected := app.SelectOperationPolicy(testlicense.New(t))
		completes("SelectOperationPolicy", selected.State, selected.Reason)
		return app
	}
	// retain writes every document once.
	retain := func(app *desktop.App, region string) {
		filtered := app.SaveFilter(grid.Filter{Name: region, Kinds: []bundle.EventKind{bundle.Message}})
		completes("SaveFilter", filtered.State, filtered.Reason)
		recorded := app.RecordView(desktop.View{Workspace: workspace, Region: "evidence", Case: region})
		completes("RecordView", recorded.State, recorded.Reason)
		drafted := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote))
		completes("SaveEditorDraft", drafted.State, drafted.Reason)
		opened := app.OpenWorkspace(t.TempDir())
		completes("OpenWorkspace", opened.State, opened.Reason)
		chosen := app.ChooseCommercialDestinations()
		completes("ChooseCommercialDestinations", chosen.State, chosen.Reason)
		hub := app.SelectHubConfig(hubConfig)
		completes("SelectHubConfig", hub.State, hub.Reason)
	}
	retain(shell(outside), "outside")
	for _, name := range documents {
		if _, err := os.Lstat(filepath.Join(outside, name)); err != nil {
			t.Fatalf("the shell outside wrote no %s: %v", name, err)
		}
		if err := os.Symlink(filepath.Join(outside, name), filepath.Join(ownState, name)); err != nil {
			t.Fatal(err)
		}
	}
	before := bytesUnder(t, outside)

	retain(shell(ownState), "inside")
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Error("a shell document written through a link changed the document it led to")
	}
	for _, name := range documents {
		if entry, err := os.Lstat(filepath.Join(ownState, name)); err != nil || !entry.Mode().IsRegular() {
			t.Errorf("%s is %v, want the shell's own regular file", name, entry)
		}
	}
}
