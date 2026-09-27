package connectedrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

// RecoveryStore is operator-selected local coordination state. Imported result
// bytes cannot create it or substitute a new host's store at the same pathname.
type RecoveryStore struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
}

func readRecoveryStore(path string) (RecoveryStore, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return RecoveryStore{}, invalid
	}
	entry, e := os.Lstat(abs)
	if e != nil || entry.Mode()&os.ModeSymlink != 0 {
		return RecoveryStore{}, invalid
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return RecoveryStore{}, invalid
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return RecoveryStore{}, invalid
	}
	raw, err := (artifactdir.Document{MaxBytes: 128}).Read(filepath.Join(real, "store-id"))
	identity := strings.TrimSpace(string(raw))
	if err != nil || !networkaction.ValidDigest(identity) {
		return RecoveryStore{}, invalid
	}
	return RecoveryStore{Path: real, Identity: identity}, nil
}
func scopeStore(binding networkaction.Binding, store *RecoveryStore) networkaction.Binding {
	if store != nil {
		binding.Configuration = networkaction.Digest([]byte(binding.Configuration + "/" + store.Identity + "/" + store.Path))
	}
	return binding
}

type storeAuthority struct {
	authority networkaction.Authority
	store     *RecoveryStore
}

func (a storeAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	return a.authority.Check(ctx, scopeStore(b, a.store))
}
