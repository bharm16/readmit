package hubclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// ListFiles reads what a project links: each artifact's digest and size, and
// who linked it and when where the hub recorded that. It reads no artifact's
// bytes.
func (c *Client) ListFiles(ctx context.Context, project string) (hubprotocol.ProjectFiles, error) {
	data, err := c.readDirectory(ctx, project, "files")
	if err != nil {
		return hubprotocol.ProjectFiles{}, err
	}
	files, err := hubprotocol.DecodeProjectFiles(data)
	if err != nil || files.Project != project {
		return hubprotocol.ProjectFiles{}, errors.New("invalid project files response")
	}
	return files, nil
}

// ListMembers reads the project's members as its installed access policy
// grants them and its log removed them; the hub answers administrators only.
func (c *Client) ListMembers(ctx context.Context, project string) (hubprotocol.ProjectMembers, error) {
	return c.readMembers(ctx, project, "members")
}

// ListReviewers reads the project's active members who may approve: the
// people a review request may ask.
func (c *Client) ListReviewers(ctx context.Context, project string) (hubprotocol.ProjectMembers, error) {
	return c.readMembers(ctx, project, "reviewers")
}

func (c *Client) readMembers(ctx context.Context, project, route string) (hubprotocol.ProjectMembers, error) {
	data, err := c.readDirectory(ctx, project, route)
	if err != nil {
		return hubprotocol.ProjectMembers{}, err
	}
	members, err := hubprotocol.DecodeProjectMembers(data)
	if err != nil || members.Project != project {
		return hubprotocol.ProjectMembers{}, errors.New("invalid project members response")
	}
	return members, nil
}

func (c *Client) readDirectory(ctx context.Context, project, route string) ([]byte, error) {
	if err := c.requireSession(); err != nil {
		return nil, err
	}
	if !hubprotocol.ValidProject(project) {
		return nil, errors.New("invalid project identifier")
	}
	resp, err := c.doJSON(ctx, http.MethodGet, "/v2/projects/"+project+"/"+route, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := readJSONBody(resp.Body, hubprotocol.MaxDirectoryBytes)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return data, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, ErrAccessDenied
	case http.StatusNotFound:
		return nil, errors.New("this hub does not list project " + route)
	default:
		return nil, fmt.Errorf("%s read refused with status %d", route, resp.StatusCode)
	}
}

// ReadArtifact reads one project artifact's bytes and verifies them against
// its digest. It writes nothing: the caller decides where, if anywhere, the
// verified bytes go. The warning is the hub's custody sentence.
func (c *Client) ReadArtifact(ctx context.Context, project, digest string) ([]byte, string, error) {
	if !hubprotocol.ValidProject(project) || !hubprotocol.ValidDigest(digest) {
		return nil, "", errors.New("invalid project artifact")
	}
	if err := c.requireSession(); err != nil {
		return nil, "", err
	}
	resp, err := c.doJSON(ctx, http.MethodGet, "/v1/projects/"+project+"/artifacts/"+digest, nil)
	if err != nil {
		return nil, "", fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, "", ErrAccessDenied
	case http.StatusNotFound:
		return nil, "", errors.New("artifact not found in project")
	case http.StatusGone:
		return nil, "", errors.New("artifact has been retired")
	default:
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	warning := resp.Header.Get(hubprotocol.CustodyHeader)
	if warning == "" {
		warning = hubprotocol.CustodyWarning
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtifactBytes+1))
	if err != nil {
		return nil, "", errors.New("the download did not complete")
	}
	if len(body) > MaxArtifactBytes {
		return nil, "", errors.New("downloaded artifact exceeds size limit")
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, "", ErrIntegrity
	}
	return body, warning, nil
}

// UploadBytes publishes exactly these bytes to the project and answers their
// digest.
func (c *Client) UploadBytes(ctx context.Context, project string, body []byte) (string, error) {
	if !hubprotocol.ValidProject(project) {
		return "", errors.New("invalid project identifier")
	}
	if err := c.requireSession(); err != nil {
		return "", err
	}
	if !c.session.Allows("evidence.write") {
		return "", ErrAccessDenied
	}
	if len(body) > MaxArtifactBytes {
		return "", errors.New("the file exceeds the artifact size limit")
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.config.Hub+"/v1/projects/"+project+"/artifacts/"+digest, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", c.session.BearerHeader())
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		return digest, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return "", ErrAccessDenied
	case http.StatusUnprocessableEntity:
		return "", ErrIntegrity
	case http.StatusRequestEntityTooLarge:
		return "", fmt.Errorf("%w; artifact exceeds hub storage limit", ErrCommandRefused)
	case http.StatusGone:
		return "", fmt.Errorf("%w; this file was retired in the project", ErrCommandRefused)
	case http.StatusBadRequest, http.StatusNotFound:
		return "", fmt.Errorf("%w with status %d", ErrCommandRefused, resp.StatusCode)
	default:
		return "", fmt.Errorf("upload rejected with status %d", resp.StatusCode)
	}
}
