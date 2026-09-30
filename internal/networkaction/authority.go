// Package connectedtransport admits explicit connected actions without granting
// authority to imported plans or changing the legacy loopback defaults.
package networkaction

import (
	"context"
	"encoding/json/v2"
	"errors"
	"runtime"
	"time"

	"crypto/sha256"
	"encoding/hex"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const GrantSchema = "readmit-connected-runner-grant/v1"

var refused = errors.New("connected transport refused; verify current configuration and scoped authority")

// Binding is constructed from sealed local inputs, never from grant contents.
type Binding struct {
	Plan          string               `json:"plan"`
	Configuration string               `json:"configuration"`
	Policy        string               `json:"policy"`
	Credentials   string               `json:"credentials"`
	Source        string               `json:"source"`
	Project       string               `json:"project"`
	Environment   string               `json:"environment"`
	Revision      string               `json:"revision"`
	Endpoint      string               `json:"endpoint"`
	Operation     sendpolicy.Operation `json:"operation"`
}
type Actor struct {
	EvidenceIdentity string    `json:"evidence_identity"`
	Expires          time.Time `json:"expires"`
	Kind             string    `json:"kind"`
	ID               string    `json:"id"`
	Generation       string    `json:"generation"`
}

// Authority is the adapter seam for a live runner grant or the desktop owner's
// existing one-action review. Check must revalidate revocation and generation;
// it is called before DNS, secrets, dialing and every message write.
type Authority interface {
	Check(context.Context, Binding) (Actor, error)
}
type RunnerGrant struct {
	Schema     string    `json:"schema"`
	Actor      string    `json:"actor"`
	Generation string    `json:"generation"`
	Binding    Binding   `json:"binding"`
	IssuedAt   time.Time `json:"issued_at"`
	Expires    time.Time `json:"expires"`
}

// FileAuthority reads the separately configured owner-only grant on every use.
// Imported tests cannot select this file or manufacture a desktop approval.
type FileAuthority struct{ Path, Actor, Generation string }

func (f FileAuthority) Check(ctx context.Context, b Binding) (Actor, error) {
	if ctx.Err() != nil || !runnerprotocol.ID(f.Actor) || !runnerprotocol.ID(f.Generation) {
		return Actor{}, refused
	}
	if err := checkAdmission(ctx); err != nil {
		return Actor{}, err
	}
	if authority, ok := ctx.Value(authorityKey{}).(Authority); ok {
		// A runtime authority can delegate to an ordinary FileAuthority.
		// Consume this override at the boundary so that delegation cannot
		// recursively select itself; enclosing admission checks stay intact.
		actor, err := authority.Check(context.WithValue(ctx, authorityKey{}, nil), b)
		if err != nil {
			return Actor{}, err
		}
		if !CurrentActor(actor) {
			return Actor{}, refused
		}
		return actor, nil
	}
	raw, err := (artifactdir.Document{MaxBytes: 64 << 10, OwnerOnly: runtime.GOOS != "windows"}).Read(f.Path)
	var g RunnerGrant
	if err != nil || json.Unmarshal(raw, &g, json.RejectUnknownMembers(true)) != nil {
		return Actor{}, refused
	}
	now := time.Now()
	if g.Schema != GrantSchema || g.Actor != f.Actor || g.Generation != f.Generation || g.Binding != b || g.IssuedAt.After(now) || !g.Expires.After(now) || !g.Expires.After(g.IssuedAt) || g.Expires.Sub(g.IssuedAt) > 24*time.Hour {
		return Actor{}, refused
	}
	return Actor{Kind: "runner", ID: g.Actor, Generation: g.Generation, EvidenceIdentity: Digest(raw), Expires: g.Expires}, nil
}

func Digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
