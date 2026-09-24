// Package hubadmin prepares customer-run hub maintenance steps. It never opens
// a hub store, contacts the hub, or starts an operator command.
package hubadmin

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/bharm16/readmit/hub"
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

type Result struct {
	State         string   `json:"state"`
	Reason        string   `json:"reason,omitzero"`
	Command       string   `json:"command,omitzero"`
	Prerequisites []string `json:"prerequisites,omitzero"`
	Touches       []string `json:"touches,omitzero"`
	DoesNotTouch  []string `json:"does_not_touch,omitzero"`
	LocalResult   string   `json:"local_result,omitzero"`
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
	info, err := os.Lstat(value)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("local file unavailable")
	}
	f, err := os.Open(value)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("local file changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("local file unavailable")
	}
	return data, nil
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
