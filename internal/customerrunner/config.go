// Package customerrunner operates a customer-local, explicitly enrolled runner.
package customerrunner

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/secret"
)

var ErrRefused = errors.New("runner operation refused; check private configuration, admission, and retained status")

type Reference struct {
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}

func (r Reference) locator() secret.Locator {
	return secret.Locator{Command: r.Command, Arguments: r.Arguments}
}

type Config struct {
	Schema       string    `json:"schema"`
	Hub          string    `json:"hub"`
	Project      string    `json:"project"`
	Environment  string    `json:"environment"`
	Root         string    `json:"root"`
	CA           string    `json:"ca"`
	Certificate  string    `json:"certificate"`
	Key          Reference `json:"key"`
	Token        Reference `json:"token"`
	UpdateKey    string    `json:"update_key"`
	UpdateEngine string    `json:"update_engine"`
}

func ReadConfig(path string) (Config, error) {
	var c Config
	raw, err := privateRead(path, 16384)
	if err != nil || runnerprotocol.Exact(raw, "schema", "hub", "project", "environment", "root", "ca", "certificate", "key", "token", "update_key", "update_engine") != nil || json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil {
		return c, ErrRefused
	}
	var nested struct {
		Key   jsontext.Value `json:"key"`
		Token jsontext.Value `json:"token"`
	}
	if json.Unmarshal(raw, &nested) != nil || runnerprotocol.Exact(nested.Key, "command", "arguments") != nil || runnerprotocol.Exact(nested.Token, "command", "arguments") != nil {
		return c, ErrRefused
	}
	return c, c.validate()
}
func (c Config) validate() error {
	u, err := url.Parse(c.Hub)
	key, e := base64.StdEncoding.Strict().DecodeString(c.UpdateKey)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || c.UpdateEngine == "" || len(c.UpdateEngine) > 64 || c.Schema != "readmit-runner/v1" || !runnerprotocol.ID(c.Project) || !runnerprotocol.ID(c.Environment) || e != nil || len(key) != 32 || c.Key.locator().Validate() != nil || c.Token.locator().Validate() != nil {
		return ErrRefused
	}
	for _, p := range []string{c.Root, c.CA, c.Certificate} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return ErrRefused
		}
	}
	info, err := os.Lstat(c.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateMode(info.Mode()) {
		return ErrRefused
	}
	return nil
}
func privateRead(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !privateMode(info.Mode()) || info.Size() > limit {
		return nil, ErrRefused
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrRefused
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrRefused
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrRefused
	}
	return raw, nil
}

// Windows mode bits do not describe ACLs; the installing administrator owns the
// private ACL boundary, as for retained artifacts elsewhere in the CLI.
func privateMode(mode os.FileMode) bool { return runtime.GOOS == "windows" || mode.Perm()&0077 == 0 }
