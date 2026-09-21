package operation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// DefaultTarget returns the starting configuration for a new target file.
func DefaultTarget() replay.Target {
	return replay.Target{
		Schema:         replay.TargetSchemaV3,
		TestEndpoint:   true,
		Transport:      "plain",
		ConnectTimeout: "2s",
		MessageTimeout: "5s",
		MaxACKBytes:    65536,
	}
}

// OpenOrNewTarget reads a target configuration to edit, or provides the default
// starting values if the file does not exist yet. An older version (v1 or v2)
// is refused for editing so it is not overwritten in place.
func OpenOrNewTarget(path string) (replay.Target, error) {
	if path == "" {
		return replay.Target{}, errors.New("target configuration path must not be empty")
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return DefaultTarget(), nil
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	data, err := os.ReadFile(path)
	if err == nil && json.Unmarshal(data, &declared) == nil {
		if declared.Schema != "" && declared.Schema != replay.TargetSchemaV3 {
			return replay.Target{}, errors.New("that file declares " + declared.Schema + " and target editing records " + replay.TargetSchemaV3 + "; record the named environment in a new file and leave this one as it is")
		}
	}
	return replay.ReadDeclaredTarget(path)
}

// SaveTarget writes one target configuration atomically and reads it back to verify.
func SaveTarget(path string, target replay.Target) (replay.Target, error) {
	if path == "" {
		return replay.Target{}, errors.New("target configuration path must not be empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	if _, err := os.Lstat(path); err == nil {
		var declared struct {
			Schema string `json:"schema"`
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && json.Unmarshal(data, &declared) == nil {
			if declared.Schema != "" && declared.Schema != replay.TargetSchemaV3 {
				return replay.Target{}, errors.New("that file declares " + declared.Schema + " and target editing records " + replay.TargetSchemaV3 + "; record the named environment in a new file and leave this one as it is")
			}
		}
	}
	if target.Schema == "" {
		target.Schema = replay.TargetSchemaV3
	}
	target.TestEndpoint = true
	if err := replay.WriteTarget(path, target); err != nil {
		return replay.Target{}, err
	}
	return replay.ReadTarget(path)
}

// ReadTarget reads and validates one target configuration file.
func ReadTarget(path string) (replay.Target, error) {
	if path == "" {
		return replay.Target{}, errors.New("target configuration path must not be empty")
	}
	return replay.ReadTarget(path)
}

// DiagnoseTarget checks send policy and reaches the target without sending HL7 payloads.
func DiagnoseTarget(ctx context.Context, target replay.Target, policy *sendpolicy.Policy, resolver sendpolicy.Resolver) (environment.Report, sendpolicy.Decision, error) {
	if resolver == nil {
		resolver = sendpolicy.SystemResolver
	}
	duration, err := time.ParseDuration(target.ConnectTimeout)
	if err != nil || duration <= 0 {
		duration = 5 * time.Second
	}
	decisionCtx, stop := context.WithTimeout(ctx, duration)
	decision := sendpolicy.Decide(decisionCtx, policy, sendpolicy.Request{
		Address:        target.Address,
		Classification: string(target.Environment().Classification),
		Explicit:       false,
	}, resolver)
	stop()

	report, err := environment.Diagnose(ctx, target)
	return report, decision, err
}

// ResetEnvironment runs the reviewed reset plan against the target and optionally retains
// the outcome in outcomePath.
func ResetEnvironment(ctx context.Context, req fixturereset.Request, outcomePath string, resolver sendpolicy.Resolver) (fixturereset.Result, fixturereset.Plan, error) {
	if resolver == nil {
		resolver = sendpolicy.SystemResolver
	}
	if outcomePath != "" {
		if _, err := artifactpath.Destination(outcomePath); err != nil {
			return fixturereset.Result{}, fixturereset.Plan{}, err
		}
	}
	result, plan := fixturereset.Run(ctx, req, resolver)
	if outcomePath != "" {
		outcome, err := fixturereset.EncodeOutcome(result)
		if err != nil {
			return result, plan, err
		}
		dest, err := artifactpath.Destination(outcomePath)
		if err != nil {
			return result, plan, err
		}
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return result, plan, errors.New("cannot create the reset outcome file; destination must be new and writable")
		}
		_, writeErr := file.Write(outcome)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			os.Remove(dest)
			return result, plan, errors.New("cannot write the reset outcome")
		}
	}
	return result, plan, nil
}

// OpenOrEmptySecrets reads an existing store or returns a clean empty store if the file does not exist.
func OpenOrEmptySecrets(path string) (secret.Document, error) {
	if path == "" {
		return secret.Document{}, errors.New("secrets document path must not be empty")
	}
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return secret.Document{Schema: secret.Schema}, nil
	}
	return secret.ReadStore(path)
}

// ReadSecrets reads and validates one secret reference document.
func ReadSecrets(path string) (secret.Document, error) {
	if path == "" {
		return secret.Document{}, errors.New("secrets document path must not be empty")
	}
	return secret.ReadStore(path)
}

// AddSecretReference adds one credential reference and saves the updated store.
func AddSecretReference(path string, entry secret.Reference) (secret.Document, secret.Reference, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	doc, err := OpenOrEmptySecrets(path)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	if entry.Generation == 0 {
		entry.Generation = 1
	}
	if entry.RotatedAt.IsZero() {
		entry.RotatedAt = time.Now().UTC().Truncate(time.Second)
	}
	updated, stored, err := secret.Add(doc, entry)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	if err := secret.WriteStore(path, updated); err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	return updated, stored, nil
}

// UpdateSecretReference modifies configuration of one existing reference and saves the store.
func UpdateSecretReference(path string, name string, change secret.Change) (secret.Document, secret.Reference, error) {
	doc, err := ReadSecrets(path)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	updated, stored, err := secret.Update(doc, name, change)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	if err := secret.WriteStore(path, updated); err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	return updated, stored, nil
}

// RotateSecretReference verifies the credential resolves, updates generation and rotation timestamp, and saves.
func RotateSecretReference(ctx context.Context, path string, name string) (secret.Document, secret.Reference, error) {
	doc, err := ReadSecrets(path)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	entry, err := secret.Find(doc, name)
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	if _, err := secret.Resolve(ctx, entry); err != nil {
		return secret.Document{}, secret.Reference{}, errors.New("the credential did not resolve from its declared store; the recorded rotation is unchanged")
	}
	updated, stored, err := secret.Rotate(doc, name, time.Now())
	if err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	if err := secret.WriteStore(path, updated); err != nil {
		return secret.Document{}, secret.Reference{}, err
	}
	return updated, stored, nil
}

// RemoveSecretReference removes one reference by name and saves the store.
func RemoveSecretReference(path string, name string) (secret.Document, error) {
	doc, err := ReadSecrets(path)
	if err != nil {
		return secret.Document{}, err
	}
	updated, err := secret.Remove(doc, name)
	if err != nil {
		return secret.Document{}, err
	}
	if err := secret.WriteStore(path, updated); err != nil {
		return secret.Document{}, err
	}
	return updated, nil
}

// TestSecretReference checks if the declared locator resolves the credential without storing or logging it.
func TestSecretReference(ctx context.Context, ref secret.Reference) error {
	_, err := secret.Resolve(ctx, ref)
	return err
}

// ScanSecrets scans the given files/directories for leakages of registered credentials.
func ScanSecrets(ctx context.Context, secretsPath string, checkPaths []string, name string) (exportreview.Scan, int, error) {
	doc, err := ReadSecrets(secretsPath)
	if err != nil {
		return exportreview.Scan{}, 0, err
	}
	references := doc.References
	if name != "" {
		entry, err := secret.Find(doc, name)
		if err != nil {
			return exportreview.Scan{}, 0, err
		}
		references = []secret.Reference{entry}
	}
	if len(references) == 0 {
		return exportreview.Scan{}, 0, errors.New("the secret reference document registers nothing to check for")
	}
	terms := make([][]byte, 0, len(references))
	for _, entry := range references {
		resolved, err := secret.Resolve(ctx, entry)
		if err != nil {
			return exportreview.Scan{}, 0, errors.New("a registered credential did not resolve, so this scan checked nothing")
		}
		terms = append(terms, resolved.Expose())
	}
	files, skipped, err := secret.Collect(append([]string{secretsPath}, checkPaths...))
	if err != nil {
		return exportreview.Scan{}, 0, err
	}
	scan := exportreview.Residual(files, terms)
	return scan, skipped, nil
}

// ReadSendPolicy reads and decodes an approved-destination policy document.
func ReadSendPolicy(path string) (sendpolicy.Policy, error) {
	if path == "" {
		return sendpolicy.Policy{}, errors.New("policy path must not be empty")
	}
	data, err := readBoundedFile(path, sendpolicy.MaxPolicyBytes)
	if err != nil {
		return sendpolicy.Policy{}, err
	}
	return sendpolicy.DecodePolicy(data)
}

// SaveSendPolicy writes an approved-destination policy atomically and reads it back to verify.
func SaveSendPolicy(path string, policy sendpolicy.Policy) (sendpolicy.Policy, error) {
	if path == "" {
		return sendpolicy.Policy{}, errors.New("policy path must not be empty")
	}
	if policy.Schema == "" {
		policy.Schema = sendpolicy.PolicySchema
	}
	data, err := json.Marshal(policy, json.Deterministic(true))
	if err != nil {
		return sendpolicy.Policy{}, errors.New("cannot encode send policy")
	}
	data = append(data, '\n')
	if _, err := sendpolicy.DecodePolicy(data); err != nil {
		return sendpolicy.Policy{}, err
	}
	if err := atomicWrite(path, data); err != nil {
		return sendpolicy.Policy{}, err
	}
	return ReadSendPolicy(path)
}

// EvaluateSendPolicy evaluates whether an address and classification is approved under the given policy.
func EvaluateSendPolicy(ctx context.Context, policy *sendpolicy.Policy, address, classification string, explicit bool, resolver sendpolicy.Resolver) sendpolicy.Decision {
	if resolver == nil {
		resolver = sendpolicy.SystemResolver
	}
	return sendpolicy.Decide(ctx, policy, sendpolicy.Request{
		Address:        address,
		Classification: classification,
		Explicit:       explicit,
	}, resolver)
}

// ReadResetPlan reads and decodes a fixture reset plan document.
func ReadResetPlan(path string) (fixturereset.Plan, error) {
	if path == "" {
		return fixturereset.Plan{}, errors.New("plan path must not be empty")
	}
	data, err := readBoundedFile(path, fixturereset.MaxPlanBytes)
	if err != nil {
		return fixturereset.Plan{}, err
	}
	return fixturereset.DecodePlan(data)
}

// SaveResetPlan writes a fixture reset plan atomically and reads it back to verify.
func SaveResetPlan(path string, plan fixturereset.Plan) (fixturereset.Plan, error) {
	if path == "" {
		return fixturereset.Plan{}, errors.New("plan path must not be empty")
	}
	if plan.Schema == "" {
		plan.Schema = fixturereset.PlanSchema
	}
	data, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return fixturereset.Plan{}, errors.New("cannot encode reset plan")
	}
	data = append(data, '\n')
	if _, err := fixturereset.DecodePlan(data); err != nil {
		return fixturereset.Plan{}, err
	}
	if err := atomicWrite(path, data); err != nil {
		return fixturereset.Plan{}, err
	}
	return ReadResetPlan(path)
}

func readBoundedFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a readable regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open input file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read input file")
	}
	if len(data) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return data, nil
}

func atomicWrite(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	dest, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	incomplete, err := artifactpath.Destination(path + ".incomplete")
	if err != nil {
		return errors.New("cannot write file here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create file; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write file")
	}
	if err := os.Rename(incomplete, dest); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace file")
	}
	return nil
}
