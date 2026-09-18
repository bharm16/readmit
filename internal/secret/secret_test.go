package secret

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The provider a test points a reference at is this test binary re-executed
// with providerSwitch set. It is a stand-in for an operating system credential
// store, and every value it returns is generated inside the test and marked
// test-only. No credential is committed anywhere in this repository.
const (
	providerSwitch = "READMIT_TEST_SECRET_PROVIDER"
	testOnlyValue  = "test-only-not-a-real-credential-9f2c"
)

func TestMain(m *testing.M) {
	if mode, ok := os.LookupEnv(providerSwitch); ok {
		os.Exit(provider(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// provider emits the bytes of the file its locator argument names. A locator is
// an argument; a value never is, which is the rule this package enforces.
func provider(mode string, args []string) int {
	switch mode {
	case "fail":
		return 3
	case "hang":
		time.Sleep(30 * time.Second)
		return 0
	case "empty":
		return 0
	}
	if len(args) != 1 {
		return 4
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return 4
	}
	os.Stdout.Write(data)
	return 0
}

// providerCommand returns the absolute path of this test binary.
func providerCommand(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// locator writes test-only material to a file and returns its path.
func locator(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-only-material")
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func reference(t *testing.T, name, value string) Reference {
	t.Helper()
	return Reference{
		Name:       name,
		Store:      OSKeychain,
		Purpose:    MLLPEndpoint,
		Address:    "127.0.0.1:2575",
		Command:    providerCommand(t),
		Arguments:  []string{locator(t, value)},
		Generation: 1,
		RotatedAt:  time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
		MaxAge:     "720h",
	}
}

func TestStoreRoundTripKeepsReferencesSortedAndDeterministic(t *testing.T) {
	document := Document{Schema: Schema}
	for _, name := range []string{"lab-mllp", "acceptance-mllp"} {
		updated, stored, err := Add(document, reference(t, name, testOnlyValue))
		if err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		if stored.Name != name || stored.Generation != 1 {
			t.Fatalf("add returned %+v", stored)
		}
		document = updated
	}
	if _, _, err := Add(document, reference(t, "lab-mllp", testOnlyValue)); err == nil {
		t.Fatal("a duplicate reference name was accepted")
	}
	data, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encode(document)
	if err != nil || string(data) != string(again) {
		t.Fatal("encoding the same document twice produced different bytes")
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.References) != 2 || decoded.References[0].Name != "acceptance-mllp" {
		t.Fatalf("references were not stored in one canonical order: %+v", decoded.References)
	}
	if strings.Contains(string(data), testOnlyValue) {
		t.Fatal("the encoded store held a credential value")
	}
}

func TestDecodeRefusesUnknownMembersVersionsAndScopes(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	data, err := Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"unknown member":  func(s string) string { return strings.Replace(s, `"references":`, `"value":"x","references":`, 1) },
		"unknown version": func(s string) string { return strings.Replace(s, Schema, "readmit-secrets/v2", 1) },
		"unknown store":   func(s string) string { return strings.Replace(s, string(OSKeychain), "somewhere", 1) },
		"unknown purpose": func(s string) string { return strings.Replace(s, string(MLLPEndpoint), "anything", 1) },
		"relative command": func(s string) string {
			return strings.Replace(s, `"command":"`+document.References[0].Command+`"`, `"command":"provider"`, 1)
		},
		"zero generation": func(s string) string { return strings.Replace(s, `"generation":1`, `"generation":0`, 1) },
		"negative max age": func(s string) string {
			return strings.Replace(s, `"max_age":"720h"`, `"max_age":"-1h"`, 1)
		},
	} {
		changed := mutate(string(data))
		if changed == string(data) {
			t.Fatalf("%s: the fixture did not change", name)
		}
		if _, err := Decode([]byte(changed)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestValueIsNeverRenderedOrSerialized(t *testing.T) {
	value := Value{raw: []byte(testOnlyValue)}
	for _, rendered := range []string{
		value.String(),
		fmt.Sprintf("%v", value),
		fmt.Sprintf("%s", value),
		fmt.Sprintf("%q", value),
		fmt.Sprintf("%x", value),
		fmt.Sprintf("%#v", value),
		fmt.Sprint(struct{ Credential Value }{value}),
	} {
		if strings.Contains(rendered, testOnlyValue) {
			t.Fatalf("a rendering disclosed the value: %s", rendered)
		}
		if !strings.Contains(rendered, Mask) {
			t.Fatalf("a rendering did not mask the value: %s", rendered)
		}
	}
	if _, err := json.Marshal(struct {
		Credential Value `json:"credential"`
	}{value}); err == nil {
		t.Fatal("a credential value was serialized to JSON")
	}
	if string(value.Expose()) != testOnlyValue {
		t.Fatal("the explicitly exposed value did not match")
	}
}

func TestResolveReadsTheDeclaredStoreThroughItsLocator(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	resolved, err := Resolve(context.Background(), reference(t, "lab-mllp", testOnlyValue))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if string(resolved.Expose()) != testOnlyValue {
		t.Fatalf("resolved %q", resolved.Expose())
	}
}

func TestResolveRefusesCommandsOutsideTheDeclaredContract(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	base := reference(t, "lab-mllp", testOnlyValue)
	relative := base
	relative.Command = "provider"
	missing := base
	missing.Command = filepath.Join(t.TempDir(), "absent")
	if runtime.GOOS == "windows" {
		missing.Command += ".exe"
	}
	directory := base
	directory.Command = t.TempDir()
	for name, candidate := range map[string]Reference{"relative": relative, "missing": missing, "directory": directory} {
		if _, err := Resolve(context.Background(), candidate); err == nil {
			t.Errorf("%s command was accepted", name)
		}
	}
}

func TestResolveBoundsAProviderThatFailsHangsOrReturnsNothing(t *testing.T) {
	for mode, label := range map[string]string{"fail": "a failing provider", "empty": "an empty provider", "hang": "a hanging provider"} {
		t.Setenv(providerSwitch, mode)
		started := time.Now()
		if _, err := Resolve(context.Background(), reference(t, "lab-mllp", testOnlyValue)); err == nil {
			t.Errorf("%s was accepted", label)
		}
		if elapsed := time.Since(started); elapsed > ResolveTimeout+20*time.Second {
			t.Errorf("%s was not bounded: %s", label, elapsed)
		}
	}
}

func TestResolveStopsWhenTheCallerCancels(t *testing.T) {
	t.Setenv(providerSwitch, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, reference(t, "lab-mllp", testOnlyValue)); err == nil {
		t.Fatal("a cancelled resolution returned a value")
	}
}

func TestBindRefusesAPurposeOrAddressOutsideTheReferenceScope(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	if _, err := Bind(document, "lab-mllp", MLLPEndpoint, "127.0.0.1:2575"); err != nil {
		t.Fatalf("the declared scope did not bind: %v", err)
	}
	if _, err := Bind(document, "lab-mllp", MLLPEndpoint, "127.0.0.1:9999"); err == nil {
		t.Error("a reference bound to an address outside its scope")
	}
	if _, err := Bind(document, "lab-mllp", Purpose("something-else"), "127.0.0.1:2575"); err == nil {
		t.Error("a reference bound to a purpose outside its scope")
	}
	if _, err := Bind(document, "absent", MLLPEndpoint, "127.0.0.1:2575"); err == nil {
		t.Error("an unregistered reference bound")
	}
}

func TestRotateRecordsAGenerationForAKnownReferenceOnly(t *testing.T) {
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	at := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	updated, stored, err := Rotate(document, "lab-mllp", at)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if stored.Generation != 2 || !stored.RotatedAt.Equal(at) {
		t.Fatalf("rotation recorded %+v", stored)
	}
	if document.References[0].Generation != 1 {
		t.Fatal("rotate modified the document it was given")
	}
	if _, _, err := Rotate(updated, "absent", at); err == nil {
		t.Fatal("an unregistered reference was rotated")
	}
}

func TestRotationStateReportsAnOverdueCredentialExplicitly(t *testing.T) {
	entry := reference(t, "lab-mllp", testOnlyValue)
	if state := entry.Rotation(entry.RotatedAt.Add(time.Hour)); state != RotationCurrent {
		t.Errorf("recent rotation reported %s", state)
	}
	if state := entry.Rotation(entry.RotatedAt.Add(800 * time.Hour)); state != RotationOverdue {
		t.Errorf("stale rotation reported %s", state)
	}
	entry.MaxAge = ""
	if state := entry.Rotation(entry.RotatedAt.Add(800 * time.Hour)); state != RotationNotDeclared {
		t.Errorf("undeclared rotation interval reported %s", state)
	}
}

func TestWriteStoreRetainsAnInterruptedWriteAndKeepsItOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	document := Document{Schema: Schema, References: []Reference{reference(t, "lab-mllp", testOnlyValue)}}
	if err := WriteStore(path, document); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("store mode %v", info.Mode().Perm())
	}
	reopened, err := ReadStore(path)
	if err != nil || len(reopened.References) != 1 {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path+".incomplete", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteStore(path, document); err == nil {
		t.Fatal("an interrupted write was overwritten")
	}
	if reopened, err = ReadStore(path); err != nil || len(reopened.References) != 1 {
		t.Fatalf("the previous store did not survive the refused write: %v", err)
	}
}

// FuzzSecretStore exercises the reader of the secret reference document. An
// accepted document must encode, decode again and encode to the same bytes, so
// no input can reach a state the store cannot faithfully rewrite.
func FuzzSecretStore(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-secrets/v1","references":[]}`))
	f.Add([]byte(`{"schema":"readmit-secrets/v1","references":[{"name":"lab","store":"os-keychain",` +
		`"purpose":"mllp-endpoint","address":"127.0.0.1:2575","command":"/usr/bin/true","arguments":["-s","lab"],` +
		`"generation":1,"rotated_at":"2026-09-18T09:00:00Z","max_age":"720h"}]}`))
	f.Add([]byte(`{"schema":"readmit-secrets/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := Decode(data)
		if err != nil {
			return
		}
		encoded, err := Encode(decoded)
		if err != nil {
			t.Fatalf("an accepted document could not be encoded: %v", err)
		}
		again, err := Decode(encoded)
		if err != nil {
			t.Fatalf("an encoded document was not accepted: %v", err)
		}
		second, err := Encode(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
