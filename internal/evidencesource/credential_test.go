package evidencesource_test

import (
	"os"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/secret"
)

// writeSourceCredential registers one reference for a source endpoint. The
// locator is a program the test wrote; no credential value is stored here,
// because readmit stores none. Nothing in this file is a real credential.
func writeSourceCredential(t *testing.T, store, reader, name, address string) {
	t.Helper()
	document := secret.Document{Schema: secret.Schema, References: []secret.Reference{{
		Name: name, Store: secret.CustomerManaged, Purpose: secret.SourceEndpoint,
		Address: address, Command: reader, Generation: 1, RotatedAt: time.Now().UTC(),
	}}}
	if _, err := os.Lstat(store); err == nil {
		if err := os.Remove(store); err != nil {
			t.Fatal(err)
		}
	}
	if err := secret.WriteStore(store, document); err != nil {
		t.Fatal(err)
	}
}
