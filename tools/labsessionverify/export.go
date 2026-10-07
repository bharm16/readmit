package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
)

var compactJWT = regexp.MustCompile(`[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{32,}`)

// Export only qualification acquisitions and immutable product output. Runtime
// providers, grants, admission policy and signing material stay private.
func (d *driver) export() error {
	destination := filepath.Join(d.root, "export")
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	privateKey, err := os.ReadFile(d.connection.FHIR.PrivateKey)
	if err != nil {
		return err
	}
	manifest := map[string]string{}
	total := int64(0)
	for _, name := range []string{"result", "inputs", "build.json", "qualification.json", "witness-before.json", "witness-after.json"} {
		source := filepath.Join(d.root, name)
		if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, e := filepath.Rel(d.root, path)
			if e != nil {
				return e
			}
			target := filepath.Join(destination, rel)
			if entry.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			total += info.Size()
			if !info.Mode().IsRegular() || info.Size() > 64<<20 || total > 512<<20 || len(manifest) > 10000 {
				return errors.New("invalid or oversized evidence export member")
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if bytes.Contains(raw, privateKey) || strings.Contains(string(raw), "PRIVATE KEY-----") || compactJWT.Match(raw) {
				return errors.New("credential material in export")
			}
			for _, token := range d.fixtureTokens {
				if bytes.Contains(raw, []byte(token)) {
					return errors.New("fixture credential in export")
				}
			}
			if e = os.WriteFile(target, raw, 0600); e != nil {
				return e
			}
			manifest[filepath.ToSlash(rel)] = networkaction.Digest(raw)
			return nil
		}); err != nil {
			return err
		}
	}
	// Validate the relocated artifact through both public readers. Neither step
	// needs the adapter, credential provider, source config or running lab.
	result, err := connectedrun.OpenFlow(context.Background(), filepath.Join(destination, "result"))
	if err != nil {
		return err
	}
	if _, err = d.command("run", "status", filepath.Join(destination, "result")); err != nil {
		var exit *exec.ExitError
		if string(result.Verdict) != "fail" || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return err
		}
	}
	return writeJSON(filepath.Join(destination, "manifest.json"), map[string]any{"schema": "readmit-lab-product-export/v1", "files": manifest, "relocated_offline_reopened": true})
}
