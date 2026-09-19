package customerrunner

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"io"
	"os"
	"runtime"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// Update is signed by the customer's deployment authority, independently of
// transport. Signature covers deterministic JSON with signature set to "".
type Update struct {
	Schema    string `json:"schema"`
	Engine    string `json:"engine"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// VerifyUpdate verifies a staged candidate without executing it or replacing
// the running service. Installation remains an explicit administrator action.
func VerifyUpdate(c Config, manifest, binary string) error {
	data, err := privateRead(manifest, 4096)
	if err != nil {
		return err
	}
	var u Update
	if runnerprotocol.Exact(data, "schema", "engine", "os", "arch", "sha256", "signature") != nil || json.Unmarshal(data, &u, json.RejectUnknownMembers(true)) != nil || u.Schema != "readmit-runner-update/v1" || u.Engine != c.UpdateEngine || u.Engine == "" || len(u.Engine) > 64 || u.Engine == engine.Version() || u.OS != runtime.GOOS || u.Arch != runtime.GOARCH {
		return ErrRefused
	}
	key, err := base64.StdEncoding.Strict().DecodeString(c.UpdateKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ErrRefused
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(u.Signature)
	if err != nil {
		return ErrRefused
	}
	u.Signature = ""
	unsigned, err := json.Marshal(u, json.Deterministic(true))
	if err != nil || !ed25519.Verify(key, unsigned, signature) {
		return ErrRefused
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return ErrRefused
	}
	f, err := os.Open(binary)
	if err != nil {
		return ErrRefused
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return ErrRefused
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (256<<20)+1))
	if err != nil || n != info.Size() || hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
		return ErrRefused
	}
	return nil
}
