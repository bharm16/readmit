// Package hubadmin prepares customer-run hub maintenance steps. It never opens
// a hub store, contacts the hub, or starts an operator command.
package hubadmin

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/desktop"
)

// Request separates a host path in the reviewed Linux command from the local
// copy the desktop can read. The two machines need not share a filesystem.
type Request struct {
	Operation           string `json:"operation"`
	ConfigCopy          string `json:"config_copy"`
	ConfigPath          string `json:"config_path"`
	Directory           string `json:"directory"`
	LocalCopy           string `json:"local_copy"`
	OperationPolicyCopy string `json:"operation_policy_copy"`
	OperationPolicyPath string `json:"operation_policy_path"`
	SchedulePolicyCopy  string `json:"schedule_policy_copy"`
	SchedulePolicyPath  string `json:"schedule_policy_path"`
}

// Result carries one operation state, the facade's own, like every result the
// window reads.
type Result struct {
	State         desktop.State `json:"state"`
	Reason        string        `json:"reason,omitzero"`
	Command       string        `json:"command,omitzero"`
	Prerequisites []string      `json:"prerequisites,omitzero"`
	Touches       []string      `json:"touches,omitzero"`
	DoesNotTouch  []string      `json:"does_not_touch,omitzero"`
	LocalResult   string        `json:"local_result,omitzero"`
}

// Admin owns only a cancellable local preview. No method executes the command.
type Admin struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

func (a *Admin) Preview(request Request) Result {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return Result{State: "busy", Reason: "an administration preview is already running"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.running, a.cancel = true, cancel
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.running, a.cancel = false, nil
		a.mu.Unlock()
	}()
	return preview(ctx, request)
}

func (a *Admin) CancelPreview() Result {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return Result{State: "empty", Reason: "no administration preview is running"}
	}
	a.cancel()
	return Result{State: "busy", Reason: "cancellation requested; waiting for the current bounded local read to finish"}
}

func preview(ctx context.Context, r Request) Result {
	fail := func(reason string) Result { return Result{State: "failed", Reason: reason} }
	if !hostPath(r.ConfigPath) {
		return fail("enter a clean absolute Linux path for the hub configuration")
	}
	data, err := localFile(r.ConfigCopy, 16384)
	if err != nil {
		return fail("a local copy of the hub configuration is unavailable")
	}
	config, err := hub.ReadConfig(data)
	if err != nil {
		return fail("the hub configuration is invalid or has an unsupported schema")
	}
	if ctx.Err() != nil {
		return Result{State: "cancelled", Reason: "administration preview cancelled"}
	}

	command := "readmit-hub -config " + quote(r.ConfigPath)
	result := Result{State: "completed", Prerequisites: []string{
		"Run this reviewed step on the customer-operated Linux hub host as the dedicated hub service identity.",
		"Use the same compatible readmit-hub binary and confirm the host configuration matches the local copy reviewed here.",
		"Stop the hub service and other maintenance processes first; its exclusive PostgreSQL lease refuses concurrent use.",
	}, DoesNotTouch: []string{"The desktop runs no hub command, connects to no hub or database, and writes no host files."}}

	switch r.Operation {
	case "migrate":
		result.Touches = []string{"The host command applies transactional hub metadata migrations; existing artifact bytes are not rewritten."}
		result.Prerequisites = append(result.Prerequisites, "Take the appropriate complete backup before upgrading; refuse an unknown future schema.")
	case "check":
		result.Touches = []string{"The host command checks the metadata schema, database lease and writable artifact root; it is not a retained-content scan."}
	case "backup":
		if !hostPath(r.Directory) {
			return fail("enter a clean absolute Linux path for a new backup directory")
		}
		if !outsideArtifactRoot(config.Root, r.Directory) {
			return fail("the backup directory must be outside the artifact root")
		}
		command += " -directory " + quote(r.Directory)
		result.Touches = []string{"The host command creates a new backup directory and copies verified artifact and metadata bytes; it refuses an existing destination or initialized scheduler."}
		result.DoesNotTouch = append(result.DoesNotTouch, "The backup omits host identities, certificates, keys, policies, configuration and scheduler claims; use a complete stopped-deployment snapshot when scheduling is initialized.")
	case "verify-backup":
		if !hostPath(r.Directory) {
			return fail("enter a clean absolute Linux path for the backup directory")
		}
		verified := verifyCopiedBackup(ctx, r.LocalCopy)
		if verified.State != "completed" {
			return verified
		}
		command += " -directory " + quote(r.Directory)
		result.LocalResult = verified.LocalResult
		result.Touches = []string{"The host command opens the configured hub store and its exclusive lease, then reads and verifies the named backup; it writes no restore state."}
	case "restore":
		if !hostPath(r.Directory) {
			return fail("enter a clean absolute Linux path for the backup directory")
		}
		if !outsideArtifactRoot(config.Root, r.Directory) {
			return fail("the restore source must be outside the artifact root")
		}
		verified := verifyCopiedBackup(ctx, r.LocalCopy)
		if verified.State != "completed" {
			return verified
		}
		command += " -directory " + quote(r.Directory)
		result.LocalResult = verified.LocalResult
		result.Prerequisites = append(result.Prerequisites, "Provision a new empty dedicated database and private artifact root, restore configuration and secrets separately, and verify the backup before restoring.")
		result.Touches = []string{"The host command verifies the source backup, copies artifacts and inserts metadata into the empty destination."}
		result.DoesNotTouch = append(result.DoesNotTouch, "It does not overwrite a nonempty catalogue or recover customer certificates, keys or policy files.")
	case "schedule-init":
		if !hostPath(r.OperationPolicyPath) || !hostPath(r.SchedulePolicyPath) {
			return fail("enter clean absolute Linux paths for operation and schedule policies")
		}
		operation, err := localFile(r.OperationPolicyCopy, 1<<20)
		if err != nil {
			return fail("a local copy of the operation policy is unavailable")
		}
		if _, err = hub.ReadOperationPolicy(operation); err != nil {
			return fail("the operation policy is invalid or has an unsupported schema")
		}
		schedule, err := localFile(r.SchedulePolicyCopy, 1<<20)
		if err != nil {
			return fail("a local copy of the schedule policy is unavailable")
		}
		if _, err = hub.DecodeSchedules(schedule); err != nil {
			return fail("the schedule policy is invalid or has an unsupported schema")
		}
		command += " -operation-policy " + quote(r.OperationPolicyPath) + " -schedule-policy " + quote(r.SchedulePolicyPath)
		result.Prerequisites = append(result.Prerequisites, "Local author admission must pass under the selected operation policy; confirm all pinned inputs and runner authority before one-time initialization.")
		result.Touches = []string{"The host command initializes private scheduler history under the artifact root once; it does not run scheduled work."}
	case "schedule-pin":
		if !hostPath(r.Directory) {
			return fail("enter a clean absolute Linux path for the test specification")
		}
		if !localPath(r.LocalCopy) {
			return fail("choose a clean absolute path to a local copy of the test specification")
		}
		identity, err := hub.ScheduleInputIdentityContext(ctx, r.LocalCopy)
		if err != nil {
			if ctx.Err() != nil {
				return Result{State: "cancelled", Reason: "administration preview cancelled"}
			}
			return fail("the copied test inputs cannot be pinned by the hub's schedule reader")
		}
		command += " -directory " + quote(r.Directory)
		result.LocalResult = identity
		result.Prerequisites = append(result.Prerequisites, "The host must hold byte-identical test inputs and referenced files; compare its printed identity with the local value before approving the schedule.")
		result.Touches = []string{"The host command opens the configured hub store and its exclusive lease, reads the test inputs and prints their identity; it does not create a schedule or execute the test."}
	default:
		return fail("choose one supported hub administration command")
	}
	if ctx.Err() != nil {
		return Result{State: "cancelled", Reason: "administration preview cancelled"}
	}
	result.Command = command + " " + r.Operation
	return result
}

// hostPath validates a path as the Linux hub will interpret it, even when this
// desktop is running on Windows. Local copies use the desktop's native rules.
func hostPath(value string) bool {
	if !path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func localPath(value string) bool { return filepath.IsAbs(value) && filepath.Clean(value) == value }

func outsideArtifactRoot(root, target string) bool {
	return root != "/" && target != root && !strings.HasPrefix(target, root+"/")
}

func verifyCopiedBackup(ctx context.Context, local string) Result {
	if !localPath(local) {
		return Result{State: "failed", Reason: "choose a clean absolute path to a local copy of the backup"}
	}
	if err := hub.VerifyBackup(ctx, local); err != nil {
		if ctx.Err() != nil {
			return Result{State: "cancelled", Reason: "administration preview cancelled"}
		}
		return Result{State: "failed", Reason: "the copied backup failed the hub's offline verification, including its schema and retained bytes"}
	}
	return Result{State: "completed", LocalResult: "The local copy passed readmit-hub's offline backup verifier; hashes establish consistency, not authenticity of the source."}
}

func localFile(value string, limit int64) ([]byte, error) {
	if !localPath(value) {
		return nil, errors.New("absolute local file required")
	}
	unavailable := errors.New("local file unavailable")
	return artifactdir.Document{
		MaxBytes: int(limit),
		Refusals: artifactdir.DocumentRefusals{
			Irregular: unavailable,
			Open:      artifactdir.FilesystemReport,
			Changed:   errors.New("local file changed"),
			Read:      unavailable,
		},
	}.Read(value)
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// MembershipRequest is one membership change over a local copy of the host's
// access policy: add a member to a project, or change a member's role. The
// installed policy stays the host operator's to replace.
type MembershipRequest struct {
	Operation  string `json:"operation"`
	PolicyCopy string `json:"policy_copy"`
	PolicyPath string `json:"policy_path"`
	Project    string `json:"project"`
	Subject    string `json:"subject"`
	Role       string `json:"role"`
}

// MembershipResult is a prepared membership change: the exact grant before
// and after, the complete new policy the hub's own reader accepted, and the
// command that installs it on the host. Preparing changes nobody's access.
type MembershipResult struct {
	State         desktop.State `json:"state"`
	Reason        string        `json:"reason,omitzero"`
	Project       string        `json:"project,omitzero"`
	Subject       string        `json:"subject,omitzero"`
	Before        string        `json:"before,omitzero"`
	After         string        `json:"after,omitzero"`
	Command       string        `json:"command,omitzero"`
	Prerequisites []string      `json:"prerequisites,omitzero"`
	Touches       []string      `json:"touches,omitzero"`
	DoesNotTouch  []string      `json:"does_not_touch,omitzero"`
	// Policy is the complete new access policy, and PolicyName the file
	// name the command installs it from.
	Policy     string `json:"policy,omitzero"`
	PolicyName string `json:"policy_name,omitzero"`
}

// membershipFile is the name a prepared policy is exported and installed
// under.
const membershipFile = "access.json"

// PrepareMembership reads a local copy of the access policy with the hub's
// own reader, applies one membership change and reads the result again the
// same way, so a policy the hub would refuse is never handed over. It writes
// nothing, runs nothing and contacts no hub.
func (a *Admin) PrepareMembership(request MembershipRequest) MembershipResult {
	fail := func(reason string) MembershipResult { return MembershipResult{State: "failed", Reason: reason} }
	if !hostPath(request.PolicyPath) {
		return fail("enter a clean absolute Linux path for the installed access policy")
	}
	subject := strings.TrimSpace(request.Subject)
	if subject == "" || len(subject) > 256 || strings.IndexFunc(subject, unicode.IsControl) >= 0 {
		return fail("enter the person's identity provider subject")
	}
	data, err := localFile(request.PolicyCopy, 1<<20)
	if err != nil {
		return fail("a local copy of the access policy is unavailable")
	}
	policy, err := hub.ReadAccessPolicy(data)
	if err != nil {
		return fail("the local copy is not an access policy the hub reads")
	}
	at := -1
	for i, grant := range policy.Grants {
		if grant.Project == request.Project && grant.Subject == subject {
			at = i
		}
	}
	result := MembershipResult{State: "completed", Project: request.Project, Subject: subject, After: request.Role, PolicyName: membershipFile}
	switch request.Operation {
	case "add-member":
		if at >= 0 {
			return fail(subject + " is already a member of " + request.Project + " as " + policy.Grants[at].Role + "; change their role instead")
		}
		policy.Grants = append(policy.Grants, hub.ProjectGrant{Project: request.Project, Subject: subject, Role: request.Role})
	case "change-role":
		if at < 0 {
			return fail(subject + " is not a member of " + request.Project + "; add them instead")
		}
		if policy.Grants[at].Role == request.Role {
			return fail(subject + " already has the " + request.Role + " role")
		}
		result.Before = policy.Grants[at].Role
		policy.Grants[at].Role = request.Role
	default:
		return fail("choose Add member or Change role")
	}
	changed, err := json.Marshal(policy)
	if err == nil {
		_, err = hub.ReadAccessPolicy(changed)
	}
	if err != nil {
		return fail("the changed policy is one the hub would refuse; check the project, the role and any tokens registered for this person")
	}
	staged := path.Join(path.Dir(request.PolicyPath), ".access.json.new")
	result.Policy = string(changed) + "\n"
	result.Command = "sudo install -o readmit-hub -g readmit-hub -m 0600 " + membershipFile + " " + quote(staged) + " && sudo mv -f " + quote(staged) + " " + quote(request.PolicyPath)
	result.Prerequisites = []string{
		"Copy the exported " + membershipFile + " to the hub host and run the command from the folder that holds it.",
		"Confirm the installed policy still matches the copy prepared here; installing replaces it whole.",
	}
	result.Touches = []string{"Replaces the installed access policy in one rename; the hub reads it on its next request."}
	result.DoesNotTouch = []string{"Nobody's access changes until the host operator installs this policy.", "The desktop writes no host file and contacts no hub."}
	return result
}
