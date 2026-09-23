package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
)

const (
	hubSelectionSchema = "readmit-desktop-hub-selection/v1"
	custodyNotice      = "Downloaded copies remain under local custody and cannot be revoked."

	// hubSignInWait bounds how long a sign-in waits for the browser to return
	// to the loopback listener.
	hubSignInWait = 3 * time.Minute

	// hubSignInOperation names the sign-in's wait the way the hub panel's
	// cancel does, so that cancel stops the sign-in and nothing else.
	hubSignInOperation = "hub-sign-in"
)

type hubSelection struct {
	Schema string `json:"schema"`
	Config string `json:"config"`
}

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

// DefaultHubSelectionPath returns the path to the hub configuration selection file.
func DefaultHubSelectionPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("cannot locate hub selection directory")
	}
	return filepath.Join(root, "readmit", "hub.json"), nil
}

func (a *App) restoreHubSelection(selectionPath string) {
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	a.hubSelectionPath = selectionPath
	data, err := readOperationFile(selectionPath)
	if err != nil {
		return
	}
	var sel hubSelection
	if json.Unmarshal(data, &sel, json.RejectUnknownMembers(true)) != nil || sel.Schema != hubSelectionSchema || !filepath.IsAbs(sel.Config) {
		return
	}
	cfg, err := hubclient.ReadConfig(sel.Config)
	if err == nil {
		a.hubConfigPath = sel.Config
		a.hubConfig = &cfg
	}
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

// SelectHubConfig validates and stores the explicit path to a customer hub client configuration.
func (a *App) SelectHubConfig(path string) HubResult {
	return run(a, false, false, func(context.Context) HubResult { return a.selectHubConfig(path) })
}

// selectHubConfig is the selection itself, for a caller that already holds
// the operation slot.
func (a *App) selectHubConfig(path string) HubResult {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return HubResult{State: Failed, Reason: "select a cleaned absolute configuration file path"}
	}
	cfg, err := hubclient.ReadConfig(path)
	if err != nil {
		return HubResult{State: Failed, Reason: err.Error()}
	}

	a.hubMu.Lock()
	defer a.hubMu.Unlock()

	// Persist selection
	if a.hubSelectionPath != "" {
		encoded, err := json.Marshal(hubSelection{Schema: hubSelectionSchema, Config: path})
		if err == nil {
			writeShellDocument(a.hubSelectionPath, append(encoded, '\n'))
		}
	}

	// Disconnect previous connection and reset credentials
	if a.hubAuthFlow != nil {
		a.hubAuthFlow.Close()
		a.hubAuthFlow = nil
	}
	a.hubClient = nil
	a.hubSession = nil
	a.hubConfigPath = path
	a.hubConfig = &cfg

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
	return run(a, false, false, func(ctx context.Context) HubDiagnosisResult {
		a.hubMu.Lock()
		cfg := a.hubConfig
		a.hubMu.Unlock()

		if cfg == nil {
			return HubDiagnosisResult{State: Failed, Reason: "no customer hub configuration selected"}
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
	return run(a, false, false, func(ctx context.Context) HubResult {
		a.hubMu.Lock()
		cfg := a.hubConfig
		sess := a.hubSession
		a.hubMu.Unlock()

		if cfg == nil {
			return HubResult{State: Failed, Reason: "no customer hub configuration selected"}
		}

		client, err := hubclient.New(ctx, *cfg, sess)
		if err != nil {
			return HubResult{State: Failed, Reason: fmt.Sprintf("cannot create mutual TLS client: %v", err)}
		}

		if err := client.CheckHealth(ctx); err != nil {
			return HubResult{State: Failed, Reason: fmt.Sprintf("hub connection failed: %v", err)}
		}

		a.hubMu.Lock()
		a.hubClient = client
		a.hubMu.Unlock()

		return a.hubStatus(ctx)
	})
}

// DisconnectHub disconnects from the hub, clears all in-memory credentials, and displays the custody notice.
func (a *App) DisconnectHub() HubResult {
	return run(a, false, false, func(ctx context.Context) HubResult {
		a.hubMu.Lock()
		if a.hubAuthFlow != nil {
			a.hubAuthFlow.Close()
			a.hubAuthFlow = nil
		}
		a.hubClient = nil
		a.hubSession = nil
		configPath := a.hubConfigPath
		hubURL := ""
		if a.hubConfig != nil {
			hubURL = a.hubConfig.Hub
		}
		a.hubMu.Unlock()

		return HubResult{
			State:          Completed,
			Connected:      false,
			Authenticated:  false,
			ConfigPath:     configPath,
			HubURL:         hubURL,
			CustodyWarning: custodyNotice,
		}
	})
}

// StartHubAuth begins an RFC 9068 PKCE authorization flow on a local loopback server.
func (a *App) StartHubAuth() HubAuthUrlResult {
	return run(a, false, false, func(ctx context.Context) HubAuthUrlResult {
		a.hubMu.Lock()
		cfg := a.hubConfig
		if a.hubAuthFlow != nil {
			a.hubAuthFlow.Close()
			a.hubAuthFlow = nil
		}
		a.hubMu.Unlock()

		if cfg == nil {
			return HubAuthUrlResult{State: Failed, Reason: "no customer hub configuration selected"}
		}

		flow, err := hubclient.StartAuthFlow(*cfg)
		if err != nil {
			return HubAuthUrlResult{State: Failed, Reason: err.Error()}
		}

		a.hubMu.Lock()
		a.hubAuthFlow = flow
		a.hubMu.Unlock()

		return HubAuthUrlResult{
			State:   Completed,
			AuthURL: flow.AuthURL,
			Port:    flow.Port,
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
	result := runNamed[HubResult, *HubResult](a, hubSignInOperation, true, false, func(ctx context.Context) HubResult {
		cancelled := HubResult{State: Cancelled, Reason: "sign-in was cancelled"}
		a.hubMu.Lock()
		cfg := a.hubConfig
		flow := a.hubAuthFlow
		a.hubAuthFlow = nil
		a.hubMu.Unlock()

		if cfg == nil {
			return HubResult{State: Failed, Reason: "no customer hub configuration selected"}
		}
		if flow == nil {
			return HubResult{State: Failed, Reason: "no authentication flow in progress; start sign-in first"}
		}

		authCode, authState := code, state
		var err error
		if authCode == "" {
			waitCtx, cancel := context.WithTimeout(ctx, wait)
			authCode, err = flow.WaitForCallback(waitCtx)
			cancel()
			authState = flow.State
		}
		flow.Close()
		switch {
		case ctx.Err() != nil:
			return cancelled
		case errors.Is(err, context.DeadlineExceeded):
			return HubResult{State: Failed, Reason: "sign-in timed out: the browser did not return; sign in again"}
		case err != nil:
			return HubResult{State: Failed, Reason: "authentication callback failed: " + err.Error()}
		case authState != flow.State:
			return HubResult{State: Failed, Reason: "state mismatch in authentication response"}
		}

		session, err := hubclient.ExchangeCode(ctx, *cfg, authCode, flow.Verifier, flow.RedirectURI)
		if err != nil {
			if ctx.Err() != nil {
				return cancelled
			}
			return HubResult{State: Failed, Reason: "token exchange failed: " + err.Error()}
		}

		a.hubMu.Lock()
		client := a.hubClient
		a.hubMu.Unlock()
		// Re-initialize client with new session if connected
		if client != nil {
			if updatedClient, err := hubclient.New(ctx, *cfg, session); err == nil {
				client = updatedClient
			}
		}
		// A cancel that arrives before the session is kept keeps nothing.
		if ctx.Err() != nil {
			return cancelled
		}
		a.hubMu.Lock()
		a.hubSession = session
		a.hubClient = client
		a.hubMu.Unlock()

		return a.hubStatus(ctx)
	})
	// Refused by another operation before it could take the attempt, the
	// completion leaves nothing that could complete it now, so the attempt
	// ends here rather than listening until the next sign-in. A duplicate
	// completion refused by a sign-in that is waiting ends nothing: that
	// sign-in holds its attempt.
	if result.State == Busy && !a.operating(hubSignInOperation) {
		a.hubMu.Lock()
		if a.hubAuthFlow != nil {
			a.hubAuthFlow.Close()
			a.hubAuthFlow = nil
		}
		a.hubMu.Unlock()
	}
	return result
}

// HubStatus reports the current connection, authentication, and authorized project states.
func (a *App) HubStatus() HubResult {
	return run(a, false, false, func(ctx context.Context) HubResult {
		return a.hubStatus(ctx)
	})
}

func (a *App) hubStatus(ctx context.Context) HubResult {
	a.hubMu.Lock()
	cfg := a.hubConfig
	client := a.hubClient
	session := a.hubSession
	configPath := a.hubConfigPath
	a.hubMu.Unlock()

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
	return run(a, false, false, func(ctx context.Context) HubArtifactsResult {
		a.hubMu.Lock()
		client := a.hubClient
		session := a.hubSession
		a.hubMu.Unlock()

		if client == nil {
			return HubArtifactsResult{State: Failed, Reason: "not connected to customer hub"}
		}
		if session == nil || session.IsExpired(time.Now()) {
			return HubArtifactsResult{State: PermissionDenied, Reason: "sign-in required or session expired"}
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
	return run(a, false, false, func(ctx context.Context) HubTransferResult {
		a.hubMu.Lock()
		client := a.hubClient
		session := a.hubSession
		a.hubMu.Unlock()

		if client == nil {
			return HubTransferResult{State: Failed, Reason: "not connected to customer hub"}
		}
		if session == nil || session.IsExpired(time.Now()) {
			return HubTransferResult{State: PermissionDenied, Reason: "sign-in required or session expired"}
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
	return run(a, false, true, func(ctx context.Context) HubTransferResult {
		a.hubMu.Lock()
		client := a.hubClient
		session := a.hubSession
		a.hubMu.Unlock()

		if client == nil {
			return HubTransferResult{State: Failed, Reason: "not connected to customer hub"}
		}
		if session == nil || session.IsExpired(time.Now()) {
			return HubTransferResult{State: PermissionDenied, Reason: "sign-in required or session expired"}
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
