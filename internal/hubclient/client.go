package hubclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

const (
	// MaxArtifactBytes is the single artifact size bound (64 MiB), matching the hub.
	MaxArtifactBytes = 64 << 20

	incompleteSuffix = ".incomplete"
)

var (
	// ErrIntegrity reports a transport or digest verification failure.
	ErrIntegrity = errors.New("artifact integrity verification failed; payload digest mismatch")

	// ErrExpired reports that an operation was attempted on an expired session.
	ErrExpired = errors.New("the hub session has expired; please sign in again")

	// ErrAccessDenied reports that the hub refused the action or role.
	ErrAccessDenied = errors.New("hub access refused; insufficient permissions or role revoked")
)

// ArtifactMetadata describes an artifact referenced in project lifecycle events.
type ArtifactMetadata struct {
	Digest   string `json:"digest"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	Actor    string `json:"actor"`
	At       string `json:"at"`
	Reason   string `json:"reason,omitzero"`
}

// ProjectStatus reports the current authorization state, effective capabilities,
// and artifact metadata for a single project on the hub.
type ProjectStatus struct {
	Project      string             `json:"project"`
	Authorized   bool               `json:"authorized"`
	Reason       string             `json:"reason,omitzero"`
	Capabilities []string           `json:"capabilities"`
	Artifacts    []ArtifactMetadata `json:"artifacts"`
	Head         int                `json:"head"`
	Warning      string             `json:"warning,omitzero"`
}

// TransferResult describes the outcome of an artifact download or upload.
type TransferResult struct {
	State   string `json:"state"`
	Digest  string `json:"digest"`
	Size    int64  `json:"size"`
	Path    string `json:"path,omitzero"`
	Warning string `json:"warning,omitzero"`
}

// Client communicates with the customer hub over mutual TLS.
type Client struct {
	config     Config
	session    *Session
	httpClient *http.Client
}

// New creates a new Client configured with mTLS certificate references.
func New(ctx context.Context, c Config, s *Session) (*Client, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}

	httpClient, err := mutualTLSClient(ctx, c.Hub, c.CA, c.Certificate, c.Key)
	if err != nil {
		return nil, err
	}

	return &Client{
		config:     c,
		session:    s,
		httpClient: httpClient,
	}, nil
}

// mutualTLSClient is the one transport every hub client uses: TLS 1.3 to the
// hub the configuration names, verified against its CA, presenting the client
// certificate with the key its reference reads, with no keep-alives and no
// redirects.
func mutualTLSClient(ctx context.Context, hub, ca, certificate string, key KeyReference) (*http.Client, error) {
	caBytes, err := readLocalPEM(ca)
	if err != nil {
		return nil, fmt.Errorf("cannot read CA: %w", err)
	}

	certBytes, err := readLocalPEM(certificate)
	if err != nil {
		return nil, fmt.Errorf("cannot read client certificate: %w", err)
	}

	keyVal, err := key.Locator().Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve client key reference: %w", err)
	}

	pair, err := tls.X509KeyPair(certBytes, keyVal.Expose())
	if err != nil {
		return nil, errors.New("client certificate and private key do not form a pair")
	}

	u, err := url.Parse(hub)
	if err != nil {
		return nil, err
	}

	tc, err := transportsecurity.ClientConfig(u.Hostname(), caBytes)
	if err != nil {
		return nil, err
	}
	tc.MinVersion = tls.VersionTLS13
	tc.Certificates = []tls.Certificate{pair}

	tr := &http.Transport{
		TLSClientConfig:        tc,
		DisableKeepAlives:      true,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 8192,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are refused by the hub client")
		},
	}, nil
}

// CheckHealth verifies that the hub is live and ready via its operational probes.
func (c *Client) CheckHealth(ctx context.Context) error {
	return checkHealth(ctx, c.httpClient, c.config.Hub)
}

func checkHealth(ctx context.Context, httpClient *http.Client, hub string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", hub+"/health/live", nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("hub liveness check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("hub liveness check returned status %d", resp.StatusCode)
	}

	reqReady, err := http.NewRequestWithContext(ctx, "GET", hub+"/health/ready", nil)
	if err != nil {
		return err
	}
	respReady, err := httpClient.Do(reqReady)
	if err != nil {
		return fmt.Errorf("hub readiness check failed: %w", err)
	}
	respReady.Body.Close()
	if respReady.StatusCode != http.StatusNoContent {
		return fmt.Errorf("hub readiness check returned status %d", respReady.StatusCode)
	}
	return nil
}

// ProbeProject queries the hub for project lifecycle and artifacts, assessing effective permissions.
func (c *Client) ProbeProject(ctx context.Context, project string) (ProjectStatus, error) {
	if !validProject(project) {
		return ProjectStatus{Project: project, Authorized: false, Reason: "invalid project identifier"}, nil
	}

	if c.session == nil || c.session.token == "" {
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     "sign-in required",
		}, nil
	}

	if c.session.IsExpired(time.Now()) {
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     "session expired",
		}, ErrExpired
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.config.Hub+"/v1/projects/"+project+"/lifecycle", nil)
	if err != nil {
		return ProjectStatus{Project: project, Authorized: false, Reason: err.Error()}, err
	}
	req.Header.Set("Authorization", c.session.BearerHeader())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ProjectStatus{Project: project, Authorized: false, Reason: "hub connection failed"}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return ProjectStatus{Project: project, Authorized: false, Reason: "cannot read lifecycle response"}, err
		}

		var history struct {
			Schema string `json:"schema"`
			Head   int    `json:"head"`
			Events []struct {
				Schema  string `json:"schema"`
				Project string `json:"project"`
				Actor   string `json:"actor"`
				At      string `json:"at"`
				Command struct {
					Kind     string `json:"kind"`
					Resource string `json:"resource"`
					Artifact string `json:"artifact"`
					Reason   string `json:"reason"`
				} `json:"command"`
			} `json:"events"`
			Warning string `json:"warning"`
		}

		if err := json.Unmarshal(data, &history); err != nil {
			return ProjectStatus{Project: project, Authorized: false, Reason: "invalid lifecycle history response"}, err
		}

		artifactsMap := make(map[string]ArtifactMetadata)
		for _, e := range history.Events {
			if e.Command.Artifact != "" && validDigest(e.Command.Artifact) {
				artifactsMap[e.Command.Artifact] = ArtifactMetadata{
					Digest:   e.Command.Artifact,
					Resource: e.Command.Resource,
					Kind:     e.Command.Kind,
					Actor:    e.Actor,
					At:       e.At,
					Reason:   e.Command.Reason,
				}
			}
		}

		artifacts := make([]ArtifactMetadata, 0, len(artifactsMap))
		for _, a := range artifactsMap {
			artifacts = append(artifacts, a)
		}

		// Effective capabilities based on token scopes
		var caps []string
		for _, action := range []string{"evidence.read", "evidence.write", "execution", "approval", "export", "admin"} {
			if c.session.Allows(action) {
				caps = append(caps, action)
			}
		}

		return ProjectStatus{
			Project:      project,
			Authorized:   true,
			Capabilities: caps,
			Artifacts:    artifacts,
			Head:         history.Head,
			Warning:      history.Warning,
		}, nil

	case http.StatusForbidden, http.StatusUnauthorized:
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     "access refused; role or grant denied",
		}, nil

	case http.StatusNotFound:
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     "project not found on hub",
		}, nil

	case http.StatusGone:
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     "project retired",
		}, nil

	default:
		return ProjectStatus{
			Project:    project,
			Authorized: false,
			Reason:     fmt.Sprintf("hub returned status %d", resp.StatusCode),
		}, fmt.Errorf("unexpected status %d from hub", resp.StatusCode)
	}
}

// DownloadArtifact retrieves an artifact from the project and verifies its digest and custody warning.
func (c *Client) DownloadArtifact(ctx context.Context, project, digest, destinationPath string) (TransferResult, error) {
	if !validProject(project) {
		return TransferResult{State: "failed"}, errors.New("invalid project identifier")
	}
	if !validDigest(digest) {
		return TransferResult{State: "failed"}, errors.New("invalid artifact digest")
	}
	if c.session == nil || c.session.token == "" {
		return TransferResult{State: "failed"}, errors.New("authentication required")
	}
	if c.session.IsExpired(time.Now()) {
		return TransferResult{State: "failed"}, ErrExpired
	}

	dest, err := artifactpath.Destination(destinationPath)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("invalid destination path: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.config.Hub+"/v1/projects/"+project+"/artifacts/"+digest, nil)
	if err != nil {
		return TransferResult{State: "failed"}, err
	}
	req.Header.Set("Authorization", c.session.BearerHeader())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return TransferResult{State: "permission_denied"}, ErrAccessDenied
	}
	if resp.StatusCode == http.StatusNotFound {
		return TransferResult{State: "failed"}, errors.New("artifact not found in project")
	}
	if resp.StatusCode == http.StatusGone {
		return TransferResult{State: "failed"}, errors.New("artifact has been retired")
	}
	if resp.StatusCode != http.StatusOK {
		return TransferResult{State: "failed"}, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	custodyWarning := resp.Header.Get("Readmit-Custody-Warning")
	if custodyWarning == "" {
		custodyWarning = "Downloaded copies remain under local custody and cannot be revoked."
	}

	// Read body bounded
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtifactBytes+1))
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("error reading response body: %w", err)
	}
	if len(body) > MaxArtifactBytes {
		return TransferResult{State: "failed"}, errors.New("downloaded artifact exceeds size limit")
	}

	// Transport hash verification
	h := sha256.Sum256(body)
	actualDigest := hex.EncodeToString(h[:])
	if actualDigest != digest {
		return TransferResult{State: "failed"}, ErrIntegrity
	}

	// Write atomically with owner-only permissions (0600)
	if err := writeAtomic(dest, body); err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("cannot write artifact to destination: %w", err)
	}

	return TransferResult{
		State:   "completed",
		Digest:  digest,
		Size:    int64(len(body)),
		Path:    dest,
		Warning: custodyWarning,
	}, nil
}

// UploadArtifact publishes an artifact to the project on the hub.
func (c *Client) UploadArtifact(ctx context.Context, project, sourcePath string) (TransferResult, error) {
	if !validProject(project) {
		return TransferResult{State: "failed"}, errors.New("invalid project identifier")
	}
	if c.session == nil || c.session.token == "" {
		return TransferResult{State: "failed"}, errors.New("authentication required")
	}
	if c.session.IsExpired(time.Now()) {
		return TransferResult{State: "failed"}, ErrExpired
	}
	if !c.session.Allows("evidence.write") {
		return TransferResult{State: "permission_denied"}, ErrAccessDenied
	}

	resolved, err := artifactpath.Resolve(sourcePath)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("cannot resolve source file: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return TransferResult{State: "failed"}, errors.New("source must be a regular file")
	}
	if info.Size() > MaxArtifactBytes {
		return TransferResult{State: "failed"}, errors.New("source file exceeds artifact size limit")
	}

	body, err := os.ReadFile(resolved)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("cannot read source file: %w", err)
	}

	h := sha256.Sum256(body)
	digest := hex.EncodeToString(h[:])

	req, err := http.NewRequestWithContext(ctx, "PUT", c.config.Hub+"/v1/projects/"+project+"/artifacts/"+digest, bytes.NewReader(body))
	if err != nil {
		return TransferResult{State: "failed"}, err
	}
	req.Header.Set("Authorization", c.session.BearerHeader())
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return TransferResult{State: "permission_denied"}, ErrAccessDenied
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return TransferResult{State: "failed"}, ErrIntegrity
	}
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return TransferResult{State: "failed"}, errors.New("artifact exceeds hub storage limit")
	}
	if resp.StatusCode != http.StatusCreated {
		return TransferResult{State: "failed"}, fmt.Errorf("upload rejected with status %d", resp.StatusCode)
	}

	return TransferResult{
		State:  "completed",
		Digest: digest,
		Size:   int64(len(body)),
		Path:   resolved,
	}, nil
}

func writeAtomic(destination string, data []byte) error {
	incomplete := destination + incompleteSuffix
	f, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("write failed")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return err
	}
	return nil
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
