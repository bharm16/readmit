package customerrunner_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/customerrunner"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUpdateRequiresCustomerSignatureAndExactStagedBinary(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	manifest := filepath.Join(dir, "update.json")
	os.WriteFile(bin, []byte("synthetic candidate"), 0700)
	c := customerrunner.Config{UpdateEngine: "v-next", UpdateKey: base64.StdEncoding.EncodeToString(pub)}
	claims := customerrunner.Update{Schema: "readmit-runner-update/v1", Engine: "v-next", OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic candidate")))}
	unsigned, _ := json.Marshal(claims, json.Deterministic(true))
	claims.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, unsigned))
	data, _ := json.Marshal(claims)
	os.WriteFile(manifest, data, 0600)
	if err := customerrunner.VerifyUpdate(c, manifest, bin); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(bin, []byte("altered"), 0700)
	if err := customerrunner.VerifyUpdate(c, manifest, bin); err == nil {
		t.Fatal("altered update accepted")
	}
}

func TestUpdateRefusesWrongAuthorityPlatformBuildAndSymlink(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	bin := filepath.Join(dir, "candidate")
	manifest := filepath.Join(dir, "update.json")
	os.WriteFile(bin, []byte("candidate"), 0700)
	c := customerrunner.Config{UpdateEngine: "approved-next", UpdateKey: base64.StdEncoding.EncodeToString(pub)}
	base := customerrunner.Update{Schema: "readmit-runner-update/v1", Engine: "approved-next", OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("candidate")))}
	write := func(u customerrunner.Update, key ed25519.PrivateKey) {
		unsigned, _ := json.Marshal(u, json.Deterministic(true))
		u.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, unsigned))
		raw, _ := json.Marshal(u)
		os.WriteFile(manifest, raw, 0600)
	}
	for _, kind := range []string{"authority", "platform", "build", "schema"} {
		t.Run(kind, func(t *testing.T) {
			u := base
			key := priv
			switch kind {
			case "authority":
				_, key, _ = ed25519.GenerateKey(rand.Reader)
			case "platform":
				u.OS = "other"
			case "build":
				u.Engine = "old-approved-build"
			case "schema":
				u.Schema = "readmit-runner-update/v2"
			}
			write(u, key)
			if err := customerrunner.VerifyUpdate(c, manifest, bin); err == nil {
				t.Fatal("unapproved update admitted")
			}
		})
	}
	write(base, priv)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(bin, link); err == nil {
		if err := customerrunner.VerifyUpdate(c, manifest, link); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	raw, _ := os.ReadFile(manifest)
	raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
	os.WriteFile(manifest, raw, 0600)
	if err := customerrunner.VerifyUpdate(c, manifest, bin); err == nil {
		t.Fatal("unknown manifest member accepted")
	}
}
