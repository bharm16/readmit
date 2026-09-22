package hubclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// ErrConflict reports a stale expected head, duplicate conflicting id, or
// lifecycle tip conflict. Callers must fetch current state and renew the action.
var ErrConflict = errors.New("hub head or revision conflict; fetch current state and renew the action")

// ReviewCommand is the complete strict document posted to project reviews.
// Actor identity always comes from the authenticated session, never from Text.
type ReviewCommand struct {
	Schema    string `json:"schema"`
	ID        string `json:"id"`
	Expected  int    `json:"expected"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Parent    string `json:"parent"`
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
	Release   string `json:"release"`
}

// ReviewEvent is one authenticated collaboration decision retained by the hub.
type ReviewEvent struct {
	Schema   string        `json:"schema"`
	Project  string        `json:"project"`
	Sequence int           `json:"sequence"`
	Issuer   string        `json:"issuer"`
	Actor    string        `json:"actor"`
	At       string        `json:"at"`
	Command  ReviewCommand `json:"command"`
}

// ReviewHistory is the head and events returned by history or notifications.
type ReviewHistory struct {
	Schema  string        `json:"schema"`
	Head    int           `json:"head"`
	Events  []ReviewEvent `json:"events"`
	Warning string        `json:"warning,omitzero"`
}

// ReviewQuery searches history or notifications after a sequence cursor.
type ReviewQuery struct {
	Schema   string `json:"schema"`
	After    int    `json:"after"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

// LifecycleCommand is the complete strict document posted to project lifecycle.
type LifecycleCommand struct {
	Schema   string   `json:"schema"`
	ID       string   `json:"id"`
	Expected int      `json:"expected"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Artifact string   `json:"artifact"`
	Parents  []string `json:"parents"`
	Subject  string   `json:"subject"`
	Until    string   `json:"until"`
	Reason   string   `json:"reason"`
}

// LifecycleEvent is one authenticated lifecycle decision retained by the hub.
type LifecycleEvent struct {
	Schema     string           `json:"schema"`
	Project    string           `json:"project"`
	Sequence   int              `json:"sequence"`
	Issuer     string           `json:"issuer"`
	Actor      string           `json:"actor"`
	At         string           `json:"at"`
	ReviewHead int              `json:"review_head,omitzero"`
	Command    LifecycleCommand `json:"command"`
}

// LifecycleHistory carries events, unresolved revision tips, and the custody warning.
type LifecycleHistory struct {
	Schema  string              `json:"schema"`
	Head    int                 `json:"head"`
	Events  []LifecycleEvent    `json:"events"`
	Tips    map[string][]string `json:"tips"`
	Warning string              `json:"warning"`
}

// AuditExport is the body returned by an audit-export lifecycle command.
type AuditExport struct {
	Schema     string           `json:"schema"`
	Project    string           `json:"project"`
	Lifecycle  []LifecycleEvent `json:"lifecycle"`
	ReviewHead int              `json:"review_head"`
	Reviews    []ReviewEvent    `json:"reviews"`
	Warning    string           `json:"warning"`
}

// LifecycleWriteResult carries either a recorded event or an audit export body.
type LifecycleWriteResult struct {
	Event  *LifecycleEvent `json:"event,omitzero"`
	Audit  *AuditExport    `json:"audit,omitzero"`
	Replay bool            `json:"replay,omitzero"`
}

// Removed reports whether the project's administration log records a
// remove-user command for this issuer and subject. It is the same one rule
// the hub applies to recipients and runners at every admission
// (hub: deriveLifecycle().isRemoved); the application consults it before
// offering a runner lifecycle action, and the hub remains the authority that
// enforces it again when admission is asked.
func (h LifecycleHistory) Removed(issuer, subject string) bool {
	for _, event := range h.Events {
		if event.Command.Kind == "remove-user" && event.Issuer == issuer && event.Command.Subject == subject {
			return true
		}
	}
	return false
}

// ListHistory reads the collaboration review history for a project.
func (c *Client) ListHistory(ctx context.Context, project string) (ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "history", nil)
}

// SearchHistory posts a review query against project history.
func (c *Client) SearchHistory(ctx context.Context, project string, query ReviewQuery) (ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "history", &query)
}

// ListNotifications reads collaboration events addressed to the current subject.
func (c *Client) ListNotifications(ctx context.Context, project string) (ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "notifications", nil)
}

// SearchNotifications posts a review query against the subject's notifications.
func (c *Client) SearchNotifications(ctx context.Context, project string, query ReviewQuery) (ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "notifications", &query)
}

// PostReview records a collaboration decision. Retries with the same id and
// actor replay the original event; a stale expected head returns ErrConflict.
func (c *Client) PostReview(ctx context.Context, project string, command ReviewCommand) (ReviewEvent, bool, error) {
	var zero ReviewEvent
	if err := c.requireSession(); err != nil {
		return zero, false, err
	}
	if !validProject(project) {
		return zero, false, errors.New("invalid project identifier")
	}
	if command.Schema == "" {
		command.Schema = "readmit-hub-review-command/v1"
	}
	body, err := json.Marshal(command)
	if err != nil {
		return zero, false, err
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/v2/projects/"+project+"/reviews", body)
	if err != nil {
		return zero, false, err
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 1<<20)
	if err != nil {
		return zero, false, err
	}
	switch resp.StatusCode {
	case http.StatusCreated:
		var event ReviewEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return zero, false, errors.New("invalid review event response")
		}
		return event, false, nil
	case http.StatusOK:
		var event ReviewEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return zero, false, errors.New("invalid review event response")
		}
		return event, true, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, false, ErrAccessDenied
	case http.StatusConflict:
		return zero, false, ErrConflict
	case http.StatusNotFound:
		return zero, false, errors.New("review refused; evidence or parent missing")
	case http.StatusBadRequest:
		return zero, false, errors.New("review refused; command shape rejected by the hub")
	default:
		return zero, false, fmt.Errorf("review refused with status %d", resp.StatusCode)
	}
}

// GetLifecycle reads the project lifecycle log, unresolved tips and custody warning.
func (c *Client) GetLifecycle(ctx context.Context, project string) (LifecycleHistory, error) {
	var zero LifecycleHistory
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !validProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	resp, err := c.doJSON(ctx, http.MethodGet, "/v2/projects/"+project+"/lifecycle", nil)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 1<<20)
	if err != nil {
		return zero, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var history LifecycleHistory
		if err := json.Unmarshal(data, &history); err != nil {
			return zero, errors.New("invalid lifecycle history response")
		}
		if history.Warning == "" {
			history.Warning = "Downloaded copies remain under local custody and cannot be revoked."
		}
		if history.Tips == nil {
			history.Tips = map[string][]string{}
		}
		return history, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, ErrAccessDenied
	case http.StatusGone:
		return zero, errors.New("project retired")
	default:
		return zero, fmt.Errorf("lifecycle read refused with status %d", resp.StatusCode)
	}
}

// PostLifecycle records a lifecycle command. audit-export returns Audit instead of Event.
func (c *Client) PostLifecycle(ctx context.Context, project string, command LifecycleCommand) (LifecycleWriteResult, error) {
	var zero LifecycleWriteResult
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !validProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	if command.Schema == "" {
		command.Schema = "readmit-hub-lifecycle-command/v1"
	}
	if command.Parents == nil {
		command.Parents = []string{}
	}
	body, err := json.Marshal(command)
	if err != nil {
		return zero, err
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/v2/projects/"+project+"/lifecycle", body)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 8<<20)
	if err != nil {
		return zero, err
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		replay := resp.StatusCode == http.StatusOK
		if command.Kind == "audit-export" {
			var audit AuditExport
			if err := json.Unmarshal(data, &audit); err != nil {
				return zero, errors.New("invalid audit export response")
			}
			if audit.Warning == "" {
				audit.Warning = "Downloaded copies remain under local custody and cannot be revoked."
			}
			return LifecycleWriteResult{Audit: &audit, Replay: replay}, nil
		}
		var event LifecycleEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return zero, errors.New("invalid lifecycle event response")
		}
		return LifecycleWriteResult{Event: &event, Replay: replay}, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, ErrAccessDenied
	case http.StatusConflict:
		return zero, ErrConflict
	default:
		return zero, fmt.Errorf("lifecycle command refused with status %d", resp.StatusCode)
	}
}

// DownloadExport retrieves an authorized support export and verifies its digest.
func (c *Client) DownloadExport(ctx context.Context, project, digest, destinationPath string) (TransferResult, error) {
	if !validProject(project) {
		return TransferResult{State: "failed"}, errors.New("invalid project identifier")
	}
	if !validDigest(digest) {
		return TransferResult{State: "failed"}, errors.New("invalid artifact digest")
	}
	if err := c.requireSession(); err != nil {
		return TransferResult{State: "failed"}, err
	}
	dest, err := artifactpath.Destination(destinationPath)
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("invalid destination path: %w", err)
	}
	resp, err := c.doJSON(ctx, http.MethodGet, "/v2/projects/"+project+"/exports/"+digest, nil)
	if err != nil {
		return TransferResult{State: "failed"}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return TransferResult{State: "permission_denied"}, ErrAccessDenied
	}
	if resp.StatusCode == http.StatusNotFound {
		return TransferResult{State: "failed"}, errors.New("export not found in project")
	}
	if resp.StatusCode == http.StatusGone {
		return TransferResult{State: "failed"}, errors.New("export has been retired")
	}
	if resp.StatusCode != http.StatusOK {
		return TransferResult{State: "failed"}, fmt.Errorf("export download failed with status %d", resp.StatusCode)
	}
	custodyWarning := resp.Header.Get("Readmit-Custody-Warning")
	if custodyWarning == "" {
		custodyWarning = "Downloaded copies remain under local custody and cannot be revoked."
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtifactBytes+1))
	if err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("error reading response body: %w", err)
	}
	if len(body) > MaxArtifactBytes {
		return TransferResult{State: "failed"}, errors.New("downloaded export exceeds size limit")
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != digest {
		return TransferResult{State: "failed"}, ErrIntegrity
	}
	if err := writeAtomic(dest, body); err != nil {
		return TransferResult{State: "failed"}, fmt.Errorf("cannot write export to destination: %w", err)
	}
	return TransferResult{
		State:   "completed",
		Digest:  digest,
		Size:    int64(len(body)),
		Path:    dest,
		Warning: custodyWarning,
	}, nil
}

func (c *Client) readReviewRoute(ctx context.Context, project, route string, query *ReviewQuery) (ReviewHistory, error) {
	var zero ReviewHistory
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !validProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	var body []byte
	method := http.MethodGet
	if query != nil {
		method = http.MethodPost
		if query.Schema == "" {
			query.Schema = "readmit-hub-review-query/v1"
		}
		encoded, err := json.Marshal(query)
		if err != nil {
			return zero, err
		}
		body = encoded
	}
	resp, err := c.doJSON(ctx, method, "/v2/projects/"+project+"/"+route, body)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, 1<<20)
	if err != nil {
		return zero, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		var history ReviewHistory
		if err := json.Unmarshal(data, &history); err != nil {
			return zero, errors.New("invalid review history response")
		}
		return history, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return zero, ErrAccessDenied
	case http.StatusConflict:
		return zero, ErrConflict
	default:
		return zero, fmt.Errorf("%s refused with status %d", route, resp.StatusCode)
	}
}

func (c *Client) requireSession() error {
	if c.session == nil || c.session.token == "" {
		return errors.New("authentication required")
	}
	if c.session.IsExpired(time.Now()) {
		return ErrExpired
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.config.Hub+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.session.BearerHeader())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func readJSONBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("hub response exceeds size limit")
	}
	return data, nil
}
