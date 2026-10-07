// labsessionverify drives a separately built production CLI against an owned
// independent OIE/HAPI session. It never sends HL7 itself or writes FHIR state.
package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/smartbackend"
)

type connection struct {
	Schema string `json:"schema"`
	MLLP   struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		TLS  bool   `json:"tls"`
	} `json:"mllp"`
	FHIR struct {
		Base        string   `json:"base"`
		Token       string   `json:"token_endpoint"`
		Authorities string   `json:"authorities"`
		ServerName  string   `json:"server_name"`
		Client      string   `json:"client_id"`
		PrivateKey  string   `json:"private_key"`
		Scopes      []string `json:"scopes"`
	} `json:"fhir"`
}
type session struct {
	Schema     string `json:"schema"`
	Generation string `json:"generation"`
	Mode       string `json:"mode"`
	Status     string `json:"status"`
}
type driver struct {
	fixtureTokens               []string
	ctx                         context.Context
	owner                       *os.File
	state, root, binary, policy string
	connection                  connection
	session                     session
	client                      *http.Client
	key                         *rsa.PrivateKey
	ca                          []byte
}

func main() {
	var d driver
	flag.StringVar(&d.state, "state", "", "Owned independent lab directory")
	flag.StringVar(&d.root, "output", "", "New private qualification directory")
	flag.StringVar(&d.binary, "readmit", "", "Production Readmit executable")
	flag.StringVar(&d.policy, "operation-policy", "", "Optional existing operator admission policy")
	flag.Parse()
	if err := d.run(); err != nil {
		fmt.Fprintln(os.Stderr, "Product qualification:", err)
		os.Exit(1)
	}
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func (d *driver) run() error {
	if d.state == "" || d.root == "" || d.binary == "" {
		return errors.New("--state, --output and --readmit are required")
	}
	var err error
	var cancel context.CancelFunc
	d.ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	d.state, err = filepath.Abs(d.state)
	if err != nil {
		return err
	}
	d.root, err = filepath.Abs(d.root)
	if err != nil {
		return err
	}
	d.binary, err = filepath.Abs(d.binary)
	if err != nil {
		return err
	}
	owner, release, err := holdSession(filepath.Join(d.state, "control/session.lock"))
	if err != nil {
		return err
	}
	defer release()
	d.owner = owner
	if _, pendingErr := os.Lstat(filepath.Join(d.state, "control/session-intent.json")); pendingErr == nil {
		return errors.New("session transition is pending; inspect the interrupted controller before product execution")
	} else if !os.IsNotExist(pendingErr) {
		return pendingErr
	}
	if err = readJSON(filepath.Join(d.state, "control/session.json"), &d.session); err != nil {
		return err
	}
	if d.session.Schema != "readmit-lab-session/v1" || d.session.Status != "ready" || filepath.Base(d.session.Generation) != d.session.Generation {
		return errors.New("session is not ready")
	}
	attemptPath := filepath.Join(d.state, "session-evidence", d.session.Generation, "product-attempt.json")
	if _, attemptErr := os.Lstat(attemptPath); attemptErr == nil {
		return errors.New("generation already has a product attempt; explicitly select a new revision before executing again")
	} else if !os.IsNotExist(attemptErr) {
		return attemptErr
	}
	if err = readJSON(filepath.Join(d.state, "connection.json"), &d.connection); err != nil {
		return err
	}
	if d.connection.Schema != "readmit-lab-connection/v1" || d.connection.MLLP.Host != "127.0.0.1" || d.connection.MLLP.TLS {
		return errors.New("expected owned plain loopback session")
	}
	if err = os.Mkdir(d.root, 0700); err != nil {
		return err
	}
	if err = d.credentials(); err != nil {
		return err
	}
	before, err := d.get("Appointment?_count=1000&_tag=" + url.QueryEscape("urn:readmit:independent-lab|"+d.session.Generation))
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(d.root, "witness-before.json"), before); err != nil {
		return err
	}
	entries, _ := before["entry"].([]any)
	if len(entries) != 0 {
		return errors.New("owned session has appointments; explicitly reset before product run")
	}
	// The adapter selects an actual server-owned prerequisite and never creates,
	// changes, or deletes HAPI resources. The process-wide controller flock fences
	// session reset/revision for the complete product execution.
	patient, err := d.get("Patient/lab-patient-01")
	if err != nil {
		return err
	}
	adapter, err := newAdapter(d, patient)
	if err != nil {
		return err
	}
	defer adapter.server.Close()
	if err = d.author(adapter, patient); err != nil {
		return err
	}
	binary, err := os.ReadFile(d.binary)
	if err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(d.binary)
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(d.root, "build.json"), map[string]any{"go_build": info, "sha256": networkaction.Digest(binary), "session": d.session, "platform": platform(), "qualification": "local production CLI; not packaged desktop or release acceptance"}); err != nil {
		return err
	}
	if _, err = d.command("--version"); err != nil {
		return err
	}
	if _, err = d.command("connected", "prepare", filepath.Join(d.root, "inputs"), filepath.Join(d.root, "plan"), "--seed", "7", "--base-time", "2030-01-01T00:00:00Z"); err != nil {
		return err
	}
	if err = d.configure(adapter); err != nil {
		return err
	}
	// Public preflight returns exactly the bindings an operator must grant.
	preview, err := d.command("test", filepath.Join(d.root, "plan"), "--connected-config", filepath.Join(d.root, "config.json"), "--instance", "product")
	if err != nil {
		return err
	}
	var p struct {
		Bindings map[string]networkaction.Binding `json:"bindings"`
	}
	if err = json.Unmarshal(preview, &p); err != nil {
		return err
	}
	for name, binding := range p.Bindings {
		if err = writeJSON(filepath.Join(d.root, grantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "operator", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
	}
	intent, err := os.OpenFile(attemptPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(intent).Encode(map[string]any{"schema": "readmit-lab-product-attempt/v1", "generation": d.session.Generation, "state": "effect-intent", "started_at": time.Now().UTC(), "readmit_sha256": networkaction.Digest(binary), "output": d.root})
	if err == nil {
		err = intent.Sync()
	}
	closeErr := intent.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	directory, err := os.Open(filepath.Dir(attemptPath))
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr = directory.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	_, runErr := d.command("test", filepath.Join(d.root, "plan"), "--connected-config", filepath.Join(d.root, "config.json"), "--instance", "product", "--send", "--output", filepath.Join(d.root, "result"))
	after, witnessErr := d.get("Appointment?_count=1000&_tag=" + url.QueryEscape("urn:readmit:independent-lab|"+d.session.Generation))
	if witnessErr != nil {
		return witnessErr
	}
	if err = writeJSON(filepath.Join(d.root, "witness-after.json"), after); err != nil {
		return err
	}
	result, err := connectedrun.OpenFlow(context.Background(), filepath.Join(d.root, "result"))
	if err != nil {
		return fmt.Errorf("product result did not reopen: %w; command: %v", err, runErr)
	}
	want := "pass"
	count := 1
	if d.session.Mode == "defective" || d.session.Mode == "reintroduced" {
		want = "fail"
		count = 2
	}
	actual, _ := after["entry"].([]any)
	if result.State != "complete" || string(result.Verdict) != want || len(actual) != count {
		return fmt.Errorf("unexpected outcome: state=%s verdict=%s independent appointments=%d", result.State, result.Verdict, len(actual))
	}
	if len(result.Phases) != 2 {
		return errors.New("product did not execute both phases")
	}
	for _, phase := range result.Phases {
		ack := false
		for _, check := range phase.Checks {
			if check.ID == "wire:accepted" && string(check.Outcome) == "passed" {
				ack = true
			}
		}
		if !ack {
			return errors.New("positive ACK assertion missing")
		}
	}
	if want == "fail" {
		for _, id := range []string{"typed:one", "typed:unique"} {
			found := false
			for _, phase := range result.Phases {
				if phase.ID != "move" {
					continue
				}
				for _, check := range phase.Checks {
					if check.ID == id && string(check.Outcome) == "failed" {
						found = true
					}
				}
			}
			if !found {
				return errors.New("duplicate-specific assertion did not fail")
			}
		}
	}
	oracle := map[string]map[string]string{}
	for _, id := range []string{"book", "move"} {
		var typed map[string]json.RawMessage
		if err = readJSON(filepath.Join(d.root, "inputs", id+"-checks.json"), &typed); err != nil {
			return err
		}
		var wire struct {
			Assertions []map[string]any `json:"assertions"`
		}
		if err = readJSON(filepath.Join(d.root, "inputs", id+"-ack.json"), &wire); err != nil {
			return err
		}
		for _, assertion := range wire.Assertions {
			subject, _ := assertion["subject"].(map[string]any)
			field, _ := subject["field"].(map[string]any)
			delete(field, "message")
		}
		wireRaw, e := json.Marshal(wire.Assertions)
		if e != nil {
			return e
		}
		oracle[id] = map[string]string{"typed": networkaction.Digest(typed["assertions"]), "wire_without_source_occurrence": networkaction.Digest(wireRaw)}
	}
	oracleRaw, err := json.Marshal(oracle)
	if err != nil {
		return err
	}
	if want == "pass" && runErr != nil {
		return runErr
	}
	if want == "fail" {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 1 {
			return errors.New("failed assertions did not return CLI exit 1")
		}
	}
	if _, err = d.command("run", "status", filepath.Join(d.root, "result")); err != nil {
		var exit *exec.ExitError
		if want != "fail" || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return err
		}
	}
	if err = writeJSON(filepath.Join(d.root, "qualification.json"), map[string]any{"schema": "readmit-lab-product-qualification/v1", "generation": d.session.Generation, "mode": d.session.Mode, "state": result.State, "verdict": result.Verdict, "independent_appointments": len(actual), "positive_acks": true, "assertion_oracle_sha256": networkaction.Digest(oracleRaw), "assertions": oracle, "offline_reopened": true}); err != nil {
		return err
	}
	return d.export()
}
func (d *driver) command(args ...string) ([]byte, error) {
	if d.policy != "" {
		args = append([]string{"--operation-policy", d.policy}, args...)
	}
	cmd := exec.CommandContext(d.ctx, d.binary, args...)
	if d.owner != nil {
		cmd.ExtraFiles = []*os.File{d.owner}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	name := fmt.Sprintf("command-%d", time.Now().UnixNano())
	_ = writeJSON(filepath.Join(d.root, name+".json"), map[string]any{"arguments": args, "stdout": string(b), "stderr": stderr.String(), "success": e == nil})
	return b, e
}
func (d *driver) credentials() error {
	var err error
	d.ca, err = os.ReadFile(d.connection.FHIR.Authorities)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(d.ca) {
		return errors.New("invalid CA")
	}
	d.client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, ServerName: d.connection.FHIR.ServerName}}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	raw, err := os.ReadFile(d.connection.FHIR.PrivateKey)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return errors.New("missing signing key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	var ok bool
	d.key, ok = key.(*rsa.PrivateKey)
	if !ok {
		return errors.New("expected RSA lab key")
	}
	return nil
}
func (d *driver) get(path string) (map[string]any, error) {
	// Independent read-only witness uses standard HTTP/crypto, not product collectors.
	enc := base64.RawURLEncoding.EncodeToString
	now := time.Now().Unix()
	nonce := make([]byte, 16)
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	claims, _ := json.Marshal(map[string]any{"iss": d.connection.FHIR.Client, "sub": d.connection.FHIR.Client, "aud": d.connection.FHIR.Token, "iat": now, "exp": now + 120, "jti": enc(nonce)})
	text := enc([]byte(`{"alg":"RS384","typ":"JWT"}`)) + "." + enc(claims)
	hash := sha512.Sum384([]byte(text))
	sig, err := rsa.SignPKCS1v15(rand.Reader, d.key, crypto.SHA384, hash[:])
	if err != nil {
		return nil, err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join(d.connection.FHIR.Scopes, " ")}, "client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {text + "." + enc(sig)}}
	response, err := d.client.PostForm(d.connection.FHIR.Token, form)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(raw) > 65536 {
		return nil, errors.New("witness token refused")
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(raw, &token) != nil || token.AccessToken == "" {
		return nil, errors.New("invalid token response")
	}
	request, err := http.NewRequest(http.MethodGet, d.connection.FHIR.Base+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, err = d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != 200 {
		return nil, errors.New("witness read refused")
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result["resourceType"] == "Bundle" {
		if links, ok := result["link"].([]any); ok {
			for _, link := range links {
				if item, ok := link.(map[string]any); ok && item["relation"] == "next" {
					return nil, errors.New("witness incomplete: pagination")
				}
			}
		}
	}
	return result, nil
}
func (d *driver) jwk() smartbackend.JWK {
	return smartbackend.JWK{Kty: "RSA", Kid: "observer", Alg: "RS384", Use: "sig", N: base64.RawURLEncoding.EncodeToString(d.key.N.Bytes()), E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(d.key.E)).Bytes())}
}
