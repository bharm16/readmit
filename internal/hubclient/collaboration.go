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
	"github.com/bharm16/readmit/internal/hubprotocol"
)

// ErrConflict reports a stale expected head, duplicate conflicting id, or
// lifecycle tip conflict. Callers must fetch current state and renew the action.
var ErrConflict = errors.New("hub head or revision conflict; fetch current state and renew the action")

// LifecycleWriteResult carries either a recorded event or an audit export body.
type LifecycleWriteResult struct {
	Event  *hubprotocol.LifecycleEvent `json:"event,omitzero"`
	Audit  *hubprotocol.AuditExport    `json:"audit,omitzero"`
	Replay bool                        `json:"replay,omitzero"`
}

// ListHistory reads the collaboration review history for a project.
func (c *Client) ListHistory(ctx context.Context, project string) (hubprotocol.ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "history", nil)
}

// SearchHistory posts a review query against project history.
func (c *Client) SearchHistory(ctx context.Context, project string, query hubprotocol.ReviewQuery) (hubprotocol.ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "history", &query)
}

// ListNotifications reads collaboration events addressed to the current subject.
func (c *Client) ListNotifications(ctx context.Context, project string) (hubprotocol.ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "notifications", nil)
}

// SearchNotifications posts a review query against the subject's notifications.
func (c *Client) SearchNotifications(ctx context.Context, project string, query hubprotocol.ReviewQuery) (hubprotocol.ReviewHistory, error) {
	return c.readReviewRoute(ctx, project, "notifications", &query)
}

// PostReview records a collaboration decision. Retries with the same id and
// actor replay the original event; a stale expected head returns ErrConflict.
func (c *Client) PostReview(ctx context.Context, project string, command hubprotocol.ReviewCommand) (hubprotocol.ReviewEvent, bool, error) {
	var zero hubprotocol.ReviewEvent
	if err := c.requireSession(); err != nil {
		return zero, false, err
	}
	if !hubprotocol.ValidProject(project) {
		return zero, false, errors.New("invalid project identifier")
	}
	if command.Schema == "" {
		command.Schema = hubprotocol.ReviewCommandV1
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
		var event hubprotocol.ReviewEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return zero, false, errors.New("invalid review event response")
		}
		return event, false, nil
	case http.StatusOK:
		var event hubprotocol.ReviewEvent
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
func (c *Client) GetLifecycle(ctx context.Context, project string) (hubprotocol.LifecycleHistory, error) {
	var zero hubprotocol.LifecycleHistory
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !hubprotocol.ValidProject(project) {
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
		var history hubprotocol.LifecycleHistory
		if err := json.Unmarshal(data, &history); err != nil {
			return zero, errors.New("invalid lifecycle history response")
		}
		if history.Warning == "" {
			history.Warning = hubprotocol.CustodyWarning
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
func (c *Client) PostLifecycle(ctx context.Context, project string, command hubprotocol.LifecycleCommand) (LifecycleWriteResult, error) {
	var zero LifecycleWriteResult
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !hubprotocol.ValidProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	if command.Schema == "" {
		command.Schema = hubprotocol.LifecycleCommandSchema
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
			var audit hubprotocol.AuditExport
			if err := json.Unmarshal(data, &audit); err != nil {
				return zero, errors.New("invalid audit export response")
			}
			if audit.Warning == "" {
				audit.Warning = hubprotocol.CustodyWarning
			}
			return LifecycleWriteResult{Audit: &audit, Replay: replay}, nil
		}
		var event hubprotocol.LifecycleEvent
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
	if !hubprotocol.ValidProject(project) {
		return TransferResult{State: "failed"}, errors.New("invalid project identifier")
	}
	if !hubprotocol.ValidDigest(digest) {
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
	custodyWarning := resp.Header.Get(hubprotocol.CustodyHeader)
	if custodyWarning == "" {
		custodyWarning = hubprotocol.CustodyWarning
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

func (c *Client) readReviewRoute(ctx context.Context, project, route string, query *hubprotocol.ReviewQuery) (hubprotocol.ReviewHistory, error) {
	var zero hubprotocol.ReviewHistory
	if err := c.requireSession(); err != nil {
		return zero, err
	}
	if !hubprotocol.ValidProject(project) {
		return zero, errors.New("invalid project identifier")
	}
	var body []byte
	method := http.MethodGet
	if query != nil {
		// A query the contract cannot carry is refused here, before anything
		// is sent, so a mistyped search says what to fix rather than
		// returning the hub's bare refusal; the hub remains the authority for
		// the query's shape and for what it finds.
		if err := query.Refusal(); err != nil {
			return zero, err
		}
		method = http.MethodPost
		if query.Schema == "" {
			query.Schema = hubprotocol.ReviewQuerySchema
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
		var history hubprotocol.ReviewHistory
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
