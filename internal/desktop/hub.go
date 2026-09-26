package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
)

const (
	// hubSelectionSchema is the versioned contract of the document that
	// remembers the hub configuration a person selected, one of the shell
	// document store's remembered selections. Its one path member is named
	// "config".
	hubSelectionSchema = "readmit-desktop-hub-selection/v1"
	custodyNotice      = hubprotocol.CustodyWarning

	// hubSignInWait bounds how long a sign-in waits for the browser to return
	// to the loopback listener.
	hubSignInWait = 3 * time.Minute

	// hubSignInOperation names the sign-in's wait the way the hub panel's
	// cancel does, so that cancel stops the sign-in and nothing else.
	hubSignInOperation = "hub-sign-in"

	// hubSignInStartOperation names the start of a sign-in, which opens the
	// loopback listener the browser returns to.
	hubSignInStartOperation = "hub-sign-in-start"

	// hubRequestOperation names every request this window makes of the hub
	// itself, so the privacy status reports the hub active while one is in
	// progress.
	hubRequestOperation = "hub"
)

// HubProjectInfo describes one project available on the hub.
type HubProjectInfo struct {
	Project      string   `json:"project"`
	Authorized   bool     `json:"authorized"`
	Reason       string   `json:"reason,omitzero"`
	Capabilities []string `json:"capabilities,omitzero"`
	Head         int      `json:"head,omitzero"`
	Warning      string   `json:"warning,omitzero"`
}

// HubResult describes the current hub connection, authentication and project state.
type HubResult struct {
	State          State            `json:"state"`
	Reason         string           `json:"reason,omitzero"`
	Connected      bool             `json:"connected"`
	Authenticated  bool             `json:"authenticated"`
	Subject        string           `json:"subject,omitzero"`
	Issuer         string           `json:"issuer,omitzero"`
	Audience       string           `json:"audience,omitzero"`
	ExpiresAt      string           `json:"expires_at,omitzero"`
	ConfigPath     string           `json:"config_path,omitzero"`
	HubURL         string           `json:"hub_url,omitzero"`
	Projects       []HubProjectInfo `json:"projects,omitzero"`
	CustodyWarning string           `json:"custody_warning,omitzero"`
}

func (r *HubResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubCheckItem describes one prerequisite check item.
type HubCheckItem struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitzero"`
}

// HubDiagnosisResult holds actionable diagnostic checks for customer hub prerequisites.
type HubDiagnosisResult struct {
	State  State          `json:"state"`
	Reason string         `json:"reason,omitzero"`
	Passed bool           `json:"passed"`
	Checks []HubCheckItem `json:"checks,omitzero"`
}

func (r *HubDiagnosisResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubAuthUrlResult carries the authorization URL and loopback listener port for PKCE.
type HubAuthUrlResult struct {
	State   State  `json:"state"`
	Reason  string `json:"reason,omitzero"`
	AuthURL string `json:"auth_url,omitzero"`
	Port    int    `json:"port,omitzero"`
}

func (r *HubAuthUrlResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubArtifactMetadata carries metadata for one artifact recorded on the hub.
type HubArtifactMetadata struct {
	Digest   string `json:"digest"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	Actor    string `json:"actor"`
	At       string `json:"at"`
	Reason   string `json:"reason,omitzero"`
}

// HubArtifactsResult carries the list of artifacts available in an authorized project.
type HubArtifactsResult struct {
	State     State                 `json:"state"`
	Reason    string                `json:"reason,omitzero"`
	Project   string                `json:"project,omitzero"`
	Artifacts []HubArtifactMetadata `json:"artifacts,omitzero"`
	Head      int                   `json:"head,omitzero"`
	Warning   string                `json:"warning,omitzero"`
}

func (r *HubArtifactsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubTransferResult carries the outcome of an artifact download or upload.
type HubTransferResult struct {
	State         State  `json:"state"`
	Reason        string `json:"reason,omitzero"`
	TransferState string `json:"transfer_state,omitzero"`
	Digest        string `json:"digest,omitzero"`
	Size          int64  `json:"size,omitzero"`
	Path          string `json:"path,omitzero"`
	Warning       string `json:"warning,omitzero"`
}

func (r *HubTransferResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// HubDownloadRequest specifies the project, artifact digest, and destination path.
type HubDownloadRequest struct {
	Project         string `json:"project"`
	Digest          string `json:"digest"`
	DestinationPath string `json:"destination_path"`
}

// HubUploadRequest specifies the project and local source path to upload.
type HubUploadRequest struct {
	Project    string `json:"project"`
	SourcePath string `json:"source_path"`
}

// restoreHubSelection retains the configuration an earlier session selected,
// remembered in the shell document store's hub selection document. Restoring
// reads the selection and the configuration it names, two local files, and
// nothing else (hubclient.RestoreConnection): a restored configuration is
// selected and offline, and one that can no longer be restored is shown with
// why until a configuration is selected again.
func (a *App) restoreHubSelection() {
	a.hub = hubclient.RestoreConnection(a.selections.hub)
}

// ChooseHubConfig presents a dialog to select the customer hub configuration file.
func (a *App) ChooseHubConfig() HubResult {
	return run(a, true, false, func(ctx context.Context) HubResult {
		// If chooser supports FileChooser, prefer file chooser
		if fc, ok := a.chooser.(interface {
			ChooseFile(string, string) (string, error)
		}); ok {
			file, err := fc.ChooseFile("Choose customer hub configuration", "*.json")
			if err != nil {
				return HubResult{State: Failed, Reason: "the file dialog is unavailable"}
			}
			if file == "" {
				return HubResult{State: Cancelled, Reason: "no configuration file was chosen"}
			}
			return a.selectHubConfig(file)
		}

		folder, declined := a.chooseFolder(ctx, "Choose customer hub configuration folder")
		if folder == "" {
			return HubResult{State: declined.state, Reason: declined.reason}
		}

		candidates := []string{
			filepath.Join(folder, "hub-client.json"),
			filepath.Join(folder, "hub.json"),
		}
		if strings.HasSuffix(folder, ".json") {
			candidates = append([]string{folder}, candidates...)
		}

		// The dialog already holds the operation slot, so the chosen file is
		// selected inside it rather than through SelectHubConfig, which would
		// claim the slot again and refuse the person's own choice as busy.
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return a.selectHubConfig(candidate)
			}
		}
		return HubResult{State: Failed, Reason: "the chosen folder does not contain hub-client.json"}
	})
}

// SelectHubConfig validates and stores the explicit path to a customer hub
// client configuration. No component calls it: the hub panel chooses a
// configuration through ChooseHubConfig's native dialog, which makes the same
// selection (selectHubConfig); this binding stays for a caller that already
// holds the path.
func (a *App) SelectHubConfig(path string) HubResult {
	return run(a, false, false, func(context.Context) HubResult { return a.selectHubConfig(path) })
}

// selectHubConfig is the selection itself, for a caller that already holds
// the operation slot. The connection remembers the selection before it makes
// it, so a choice this window cannot remember is refused and changes nothing:
// the window never says a configuration is selected that the next one will
// not restore.
func (a *App) selectHubConfig(path string) HubResult {
	cfg, err := a.hub.Select(path)
	if err != nil {
		return HubResult{State: Failed, Reason: err.Error()}
	}
	// Another configuration is another hub: an audit export of this one is
	// not held for it.
	a.forgetHubAudit()
	return HubResult{
		State:         Completed,
		Connected:     false,
		Authenticated: false,
		ConfigPath:    path,
		HubURL:        cfg.Hub,
	}
}

// DiagnoseHub runs actionable prerequisite diagnostics against the configured customer hub.
func (a *App) DiagnoseHub() HubDiagnosisResult {
	return runNamed[HubDiagnosisResult, *HubDiagnosisResult](a, profiles["DiagnoseHub"], func(ctx context.Context) HubDiagnosisResult {
		cfg := a.hub.Status().Config
		if cfg == nil {
			return HubDiagnosisResult{State: Failed, Reason: hubclient.ErrNotSelected.Error()}
		}

		prereqs := hubclient.DiagnosePrerequisites(ctx, *cfg)
		checks := make([]HubCheckItem, len(prereqs.Checks))
		for i, c := range prereqs.Checks {
			checks[i] = HubCheckItem{
				Name:    c.Name,
				Passed:  c.Passed,
				Message: c.Message,
				Detail:  c.Detail,
			}
		}

		return HubDiagnosisResult{
			State:  Completed,
			Passed: prereqs.Passed,
			Checks: checks,
		}
	})
}

// ConnectHub establishes a mutual TLS connection to the customer hub and checks health probes.
func (a *App) ConnectHub() HubResult {
	return runNamed[HubResult, *HubResult](a, profiles["ConnectHub"], func(ctx context.Context) HubResult {
		if err := a.hub.Connect(ctx); err != nil {
			return HubResult{State: Failed, Reason: err.Error()}
		}
		return a.hubStatus(ctx)
	})
}

// DisconnectHub disconnects from the hub, clears all in-memory credentials, and displays the custody notice.
func (a *App) DisconnectHub() HubResult {
	return run(a, false, false, func(ctx context.Context) HubResult {
		a.hub.Disconnect()
		a.forgetHubAudit()
		status := a.hub.Status()
		hubURL := ""
		if status.Config != nil {
			hubURL = status.Config.Hub
		}
		return HubResult{
			State:          Completed,
			Connected:      false,
			Authenticated:  false,
			ConfigPath:     status.ConfigPath,
			HubURL:         hubURL,
			CustodyWarning: custodyNotice,
		}
	})
}

// StartHubAuth begins an RFC 9068 PKCE authorization flow on a local loopback server.
func (a *App) StartHubAuth() HubAuthUrlResult {
	return runNamed[HubAuthUrlResult, *HubAuthUrlResult](a, profiles["StartHubAuth"], func(ctx context.Context) HubAuthUrlResult {
		authURL, port, err := a.hub.StartSignIn()
		if err != nil {
			return HubAuthUrlResult{State: Failed, Reason: err.Error()}
		}
		return HubAuthUrlResult{
			State:   Completed,
			AuthURL: authURL,
			Port:    port,
		}
	})
}

// CompleteHubAuth exchanges an authorization code for an RFC 9068 access token.
// When code is empty, it waits for the browser redirect on the loopback
// listener, holding the operation slot as the sign-in the hub panel's cancel
// names. The completion takes the attempt StartHubAuth opened, so however it
// ends — refused, forged, cancelled, timed out or answered — the listener is
// closed before anything else happens, and no late browser or later call can
// complete the attempt; nothing is retried. A completion refused because
// another operation holds the slot ends the pending attempt the same way.
func (a *App) CompleteHubAuth(code, state string) HubResult {
	return a.completeHubAuth(code, state, hubSignInWait)
}

func (a *App) completeHubAuth(code, state string, wait time.Duration) HubResult {
	result := runNamed[HubResult, *HubResult](a, profiles["CompleteHubAuth"], func(ctx context.Context) HubResult {
		if err := a.hub.CompleteSignIn(ctx, code, state, wait); err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return HubResult{State: Cancelled, Reason: "sign-in was cancelled"}
			}
			return HubResult{State: Failed, Reason: err.Error()}
		}
		return a.hubStatus(ctx)
	})
	// Refused by another operation before it could take the attempt, the
	// completion leaves nothing that could complete it now, so the attempt
	// ends here rather than listening until the next sign-in. A duplicate
	// completion refused by a sign-in that is waiting ends nothing: that
	// sign-in holds its attempt.
	if result.State == Busy && !a.operating(hubSignInOperation) {
		a.hub.EndSignIn()
	}
	return result
}

// HubStatus reports the current connection, authentication, and authorized project states.
func (a *App) HubStatus() HubResult {
	return runNamed[HubResult, *HubResult](a, profiles["HubStatus"], func(ctx context.Context) HubResult {
		return a.hubStatus(ctx)
	})
}

func (a *App) hubStatus(ctx context.Context) HubResult {
	status := a.hub.Status()
	cfg, client, session, configPath := status.Config, status.Client, status.Session, status.ConfigPath

	if cfg == nil && status.Refusal != "" {
		return HubResult{State: Failed, Reason: status.Refusal, ConfigPath: configPath}
	}
	if cfg == nil {
		return HubResult{
			State:     Empty,
			Connected: false,
			Reason:    "no customer hub configured",
		}
	}

	res := HubResult{
		State:      Completed,
		Connected:  client != nil,
		ConfigPath: configPath,
		HubURL:     cfg.Hub,
	}

	if session != nil {
		if session.IsExpired(time.Now()) {
			res.Authenticated = false
			res.Reason = "hub session expired; sign in again"
		} else {
			res.Authenticated = true
			res.Subject = session.Subject
			res.Issuer = session.Issuer
			res.Audience = session.Audience
			res.ExpiresAt = session.Expires.Format(time.RFC3339)
			res.CustodyWarning = custodyNotice
		}
	}

	if client != nil && res.Authenticated {
		res.Projects = probeAllProjects(ctx, client, cfg.Projects)
	}

	return res
}

func probeAllProjects(ctx context.Context, client *hubclient.Client, projects []string) []HubProjectInfo {
	infos := make([]HubProjectInfo, 0, len(projects))
	for _, p := range projects {
		status, err := client.ProbeProject(ctx, p)
		if err != nil && status.Reason == "" {
			status.Reason = err.Error()
		}
		infos = append(infos, HubProjectInfo{
			Project:      p,
			Authorized:   status.Authorized,
			Reason:       status.Reason,
			Capabilities: status.Capabilities,
			Head:         status.Head,
			Warning:      status.Warning,
		})
	}
	return infos
}

// ListHubProjectArtifacts retrieves the artifacts and lifecycle metadata for an authorized project.
func (a *App) ListHubProjectArtifacts(project string) HubArtifactsResult {
	return runNamed[HubArtifactsResult, *HubArtifactsResult](a, profiles["ListHubProjectArtifacts"], func(ctx context.Context) HubArtifactsResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubArtifactsResult{State: errRes.State, Reason: errRes.Reason}
		}

		status, err := client.ProbeProject(ctx, project)
		if err != nil {
			if errors.Is(err, hubclient.ErrExpired) {
				return HubArtifactsResult{State: PermissionDenied, Reason: err.Error()}
			}
			return HubArtifactsResult{State: Failed, Reason: err.Error()}
		}
		if !status.Authorized {
			return HubArtifactsResult{State: PermissionDenied, Reason: status.Reason}
		}

		artifacts := make([]HubArtifactMetadata, len(status.Artifacts))
		for i, art := range status.Artifacts {
			artifacts[i] = HubArtifactMetadata{
				Digest:   art.Digest,
				Resource: art.Resource,
				Kind:     art.Kind,
				Actor:    art.Actor,
				At:       art.At,
				Reason:   art.Reason,
			}
		}

		return HubArtifactsResult{
			State:     Completed,
			Project:   project,
			Artifacts: artifacts,
			Head:      status.Head,
			Warning:   status.Warning,
		}
	})
}

// DownloadHubArtifact downloads an artifact from a customer hub project with complete verification.
func (a *App) DownloadHubArtifact(request HubDownloadRequest) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["DownloadHubArtifact"], func(ctx context.Context) HubTransferResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubTransferResult{State: errRes.State, Reason: errRes.Reason}
		}

		res, err := client.DownloadArtifact(ctx, request.Project, request.Digest, request.DestinationPath)
		if err != nil {
			if errors.Is(err, hubclient.ErrAccessDenied) || errors.Is(err, hubclient.ErrExpired) {
				return HubTransferResult{State: PermissionDenied, Reason: err.Error(), TransferState: res.State}
			}
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: res.State}
		}

		return HubTransferResult{
			State:         Completed,
			TransferState: res.State,
			Digest:        res.Digest,
			Size:          res.Size,
			Path:          res.Path,
			Warning:       res.Warning,
		}
	})
}

// UploadHubArtifact publishes an artifact to the customer hub project. Requires author admission.
func (a *App) UploadHubArtifact(request HubUploadRequest) HubTransferResult {
	return runNamed[HubTransferResult, *HubTransferResult](a, profiles["UploadHubArtifact"], func(ctx context.Context) HubTransferResult {
		client, errRes := a.requireHubSession()
		if errRes != nil {
			return HubTransferResult{State: errRes.State, Reason: errRes.Reason}
		}

		res, err := client.UploadArtifact(ctx, request.Project, request.SourcePath)
		if err != nil {
			if errors.Is(err, hubclient.ErrAccessDenied) || errors.Is(err, hubclient.ErrExpired) {
				return HubTransferResult{State: PermissionDenied, Reason: err.Error(), TransferState: res.State}
			}
			return HubTransferResult{State: Failed, Reason: err.Error(), TransferState: res.State}
		}

		return HubTransferResult{
			State:         Completed,
			TransferState: res.State,
			Digest:        res.Digest,
			Size:          res.Size,
			Path:          res.Path,
		}
	})
}
