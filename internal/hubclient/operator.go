package hubclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/bharm16/readmit/internal/strictdoc"
)

// OperatorSchema is the strict JSON contract for the configuration of an
// operator-only hub client: the hub's address and the mutual-TLS client
// identity alone. An operator-only hub has no identity provider and no
// projects, so its configuration names neither, and it is a separate contract
// rather than a readmit-hub-client/v1 document with those members left empty.
const OperatorSchema = "readmit-hub-operator-client/v1"

var (
	// ErrUnsupportedOperatorVersion reports a document that is not an
	// operator-only hub configuration this release reads, such as a team
	// hub's client configuration.
	ErrUnsupportedOperatorVersion = errors.New("not an operator-only hub configuration this release reads")

	// ErrInvalidDigest reports an artifact address that is not a whole
	// SHA-256 digest; nothing is asked of the hub.
	ErrInvalidDigest = errors.New("an artifact is named by its whole SHA-256 digest: 64 lowercase hexadecimal characters")

	// ErrHubUnreachable reports that no connection to the hub could be made,
	// or that the hub's certificate did not verify, so nothing reached it.
	ErrHubUnreachable = errors.New("the hub could not be reached; nothing was sent")

	// ErrReadUnanswered reports a read the hub did not answer whole: the
	// connection failed after it was made, or the bytes stopped arriving.
	ErrReadUnanswered = errors.New("the hub did not answer the read whole; nothing was kept")

	// ErrStoreUncertain reports a store whose answer never arrived: the hub
	// may or may not have stored the bytes. Storing the same bytes again is
	// safe, because the hub keeps one immutable object per digest.
	ErrStoreUncertain = errors.New("the hub did not answer, so whether it stored the artifact is unknown; storing the same file again is safe, or read its digest to check")

	// ErrOperatorAccessRefused reports a read the hub refused (403): an
	// operator-only hub refuses its artifact store once its database has
	// served team mode, or when it cannot confirm that it has not.
	ErrOperatorAccessRefused = errors.New("the hub refused operator-only access; a hub that has served team mode reads and stores evidence only through a signed-in project")

	// ErrOperatorStoreRefused reports a store the hub refused (403): its
	// operation policy binds no author to this client certificate, or it has
	// served team mode.
	ErrOperatorStoreRefused = errors.New("the hub refused to store the artifact: its operation policy binds no author to this client certificate, or it has served team mode and stores evidence only through a signed-in project")

	// ErrNoOperatorStore reports a store the hub does not route (404): a hub
	// serving team mode offers no operator-only artifact store.
	ErrNoOperatorStore = errors.New("the hub does not offer the operator-only artifact store; a hub serving team mode stores evidence only through a signed-in project")

	// ErrArtifactAbsent reports a read the hub answered 404. An operator-only
	// hub answers so for a digest it does not hold, and a hub serving team
	// mode for every digest, so the answer names both.
	ErrArtifactAbsent = errors.New("the hub holds no artifact under this digest, or does not offer the operator-only artifact store; a hub serving team mode reads evidence only through a signed-in project")

	// ErrArtifactUnavailable reports a read the hub could not serve (503): it
	// is busy with as many requests as it admits, its storage or metadata is
	// unavailable, or its stored copy no longer matches its digest, which the
	// hub checks before it serves a byte.
	ErrArtifactUnavailable = errors.New("the hub could not serve the artifact: it is busy, its storage or metadata is unavailable, or its stored copy no longer matches its digest")

	// ErrStoreUnavailable reports a store the hub could not complete (503).
	ErrStoreUnavailable = errors.New("the hub could not store the artifact now: it is busy, or its storage or metadata is unavailable; storing the same file again is safe")

	// ErrStoreMismatch reports a store the hub refused under its digest
	// (422). The client sends bytes under their own digest, so the bytes the
	// hub received differ from those sent, or the hub already holds a damaged
	// copy under that digest, which it never replaces.
	ErrStoreMismatch = errors.New("the hub refused the bytes under their digest: what it received did not match, or it already holds a damaged copy under that digest, which storing again cannot replace")

	// ErrOverCapacity reports a store beyond the hub's declared capacity (413).
	ErrOverCapacity = errors.New("the artifact exceeds the hub's declared capacity")
)

// OperatorConfig declares an operator-only hub endpoint and the references to
// the mutual-TLS client identity it admits. Following ADR-0006 the key is a
// reference to the program that reads it, never the key itself.
type OperatorConfig struct {
	Schema      string       `json:"schema"`
	Hub         string       `json:"hub"`
	CA          string       `json:"ca"`
	Certificate string       `json:"certificate"`
	Key         KeyReference `json:"key"`
}

var operatorConfigDoc = strictdoc.Document{
	MaxBytes:    MaxConfigBytes,
	Schema:      OperatorSchema,
	Required:    []string{"hub", "ca", "certificate", "key"},
	Invalid:     "invalid operator-only hub configuration",
	TooLarge:    "operator-only hub configuration exceeds its size limit",
	MustDeclare: "an operator-only hub configuration declares its contract version",
	Unsupported: ErrUnsupportedOperatorVersion,
	Requires:    "an operator-only hub configuration declares all required members",
}

// DecodeOperatorConfig reads and validates a strict operator-only hub
// configuration document: every member present and not null, nothing else,
// and the key reference exactly its command and arguments.
func DecodeOperatorConfig(data []byte) (OperatorConfig, error) {
	var c OperatorConfig
	if err := operatorConfigDoc.Decode(data, &c); err != nil {
		return c, err
	}
	var raw struct {
		Key jsontext.Value `json:"key"`
	}
	if json.Unmarshal(data, &raw) != nil || requireExactMembers(raw.Key, "command", "arguments") != nil {
		return c, ErrRefused
	}
	if err := ValidateOperator(c); err != nil {
		return c, err
	}
	return c, nil
}

// ReadOperatorConfig reads an operator-only hub configuration file strictly.
func ReadOperatorConfig(path string) (OperatorConfig, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return OperatorConfig{}, err
	}
	return DecodeOperatorConfig(data)
}

// ValidateOperator verifies an operator-only configuration by the rules a
// team configuration's endpoint and client identity are held to.
func ValidateOperator(c OperatorConfig) error {
	if c.Schema != OperatorSchema {
		return ErrUnsupportedOperatorVersion
	}
	return validateIdentity(c.Hub, c.CA, c.Certificate, c.Key)
}

// CheckDigest refuses an artifact address that is not a whole lowercase
// SHA-256 digest, the only address the hub's artifact store routes.
func CheckDigest(digest string) error {
	if !validDigest(digest) {
		return ErrInvalidDigest
	}
	return nil
}

// OperatorClient reads and stores opaque artifacts by digest in an
// operator-only hub: its GET and PUT /v1/artifacts/{digest} store, reached
// over the same mutual-TLS transport a team client uses, with the client
// certificate alone. It carries no session and names no project, because an
// operator-only hub has neither; the hub admits the certificate and binds a
// store to a signed author through its own operation policy.
type OperatorClient struct {
	hub        string
	httpClient *http.Client
}

// NewOperator creates an operator-only client, resolving the client key
// through its declared reference.
func NewOperator(ctx context.Context, c OperatorConfig) (*OperatorClient, error) {
	if err := ValidateOperator(c); err != nil {
		return nil, err
	}
	httpClient, err := mutualTLSClient(ctx, c.Hub, c.CA, c.Certificate, c.Key)
	if err != nil {
		return nil, err
	}
	// A configured trailing slash would double the one every route begins with.
	return &OperatorClient{hub: strings.TrimSuffix(c.Hub, "/"), httpClient: httpClient}, nil
}

// CheckHealth verifies that the hub is live and ready via its operational
// probes, which both of the hub's modes answer.
func (c *OperatorClient) CheckHealth(ctx context.Context) error {
	return checkHealth(ctx, c.httpClient, c.hub)
}

// ReadArtifact reads the artifact stored under digest and returns its bytes
// only once they hash to that digest. A refusal names what the hub's answer
// means for an operator-only store, and nothing of an answer that failed its
// check is returned.
func (c *OperatorClient) ReadArtifact(ctx context.Context, digest string) ([]byte, error) {
	if err := CheckDigest(digest); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.hub+"/v1/artifacts/"+digest, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if notSent(err) {
			return nil, ErrHubUnreachable
		}
		return nil, ErrReadUnanswered
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return nil, ErrOperatorAccessRefused
	case http.StatusNotFound:
		return nil, ErrArtifactAbsent
	case http.StatusServiceUnavailable:
		return nil, ErrArtifactUnavailable
	default:
		return nil, fmt.Errorf("the hub answered the read with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxArtifactBytes+1))
	if err != nil {
		return nil, ErrReadUnanswered
	}
	if len(body) > MaxArtifactBytes {
		return nil, errors.New("the hub's answer exceeds the artifact size limit; nothing was kept")
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, ErrIntegrity
	}
	return body, nil
}

// StoreArtifact stores body under its SHA-256 digest and returns that
// digest once the hub has answered that the object is durable. The hub keeps
// one immutable object per digest, so storing the same bytes again is safe
// and never replaces anything.
func (c *OperatorClient) StoreArtifact(ctx context.Context, body []byte) (string, error) {
	if len(body) > MaxArtifactBytes {
		return "", errors.New("the file exceeds the artifact size limit")
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.hub+"/v1/artifacts/"+digest, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if notSent(err) {
			return "", ErrHubUnreachable
		}
		return "", ErrStoreUncertain
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
		return digest, nil
	case http.StatusForbidden:
		return "", ErrOperatorStoreRefused
	case http.StatusNotFound:
		return "", ErrNoOperatorStore
	case http.StatusUnprocessableEntity:
		return "", ErrStoreMismatch
	case http.StatusRequestEntityTooLarge:
		return "", ErrOverCapacity
	case http.StatusServiceUnavailable:
		return "", ErrStoreUnavailable
	default:
		return "", fmt.Errorf("the hub answered the store with status %d", resp.StatusCode)
	}
}

// notSent reports a request that failed before any of it could reach the
// hub: every request dials anew (keep-alives are off), so a refused or
// unroutable connection is a dial error, and a hub certificate that does not
// verify ends the handshake before the request is written. Any other failure
// may have followed the request, so it is not reported as unsent.
func notSent(err error) bool {
	var op *net.OpError
	var certificate *tls.CertificateVerificationError
	return errors.As(err, &op) && op.Op == "dial" || errors.As(err, &certificate)
}
