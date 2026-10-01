package desktop_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/smartbackend"
)

// shippedExample is one of the connection examples the release ships.
func shippedExample(topology, name string) string {
	path, _ := filepath.Abs(filepath.Join("..", "..", "samples", "connections", topology, name+".json"))
	return path
}

// importExample imports an example as the window does: read it, then import
// it with a value for every placeholder.
func importExample(t *testing.T, app *desktop.App, context desktop.RequestContext, path string, values map[string]string) desktop.ConnectionExampleResult {
	t.Helper()
	read := app.ImportConnectionExample(desktop.ConnectionExampleRequest{Context: context, Path: path})
	if read.State != desktop.Completed || read.Example == nil || len(read.Saved) != 0 {
		t.Fatalf("read the example: %+v", read)
	}
	for _, placeholder := range read.Example.Placeholders {
		if _, given := values[placeholder.Token]; !given {
			t.Fatalf("the test gives no value for %s", placeholder.Token)
		}
	}
	imported := app.ImportConnectionExample(desktop.ConnectionExampleRequest{Context: context, Path: path, Import: true, Values: values, IntentID: "import-" + filepath.Base(filepath.Dir(path)) + "-" + strings.TrimSuffix(filepath.Base(path), ".json")})
	if imported.State != desktop.Completed || len(imported.Saved) != len(read.Example.Objects) {
		t.Fatalf("import the example: %+v", imported)
	}
	return imported
}

// tlsEndpoint is a TLS listener that completes handshakes, standing in for an
// engine's TLS MLLP input; its certificate is written as a CA file.
func tlsEndpoint(t *testing.T) (host, port, ca string) {
	t.Helper()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	ca = filepath.Join(t.TempDir(), "engine-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	host, port, _ = net.SplitHostPort(server.Listener.Addr().String())
	return host, port, ca
}

// smartLab is the independent FHIR lab requiring SMART Backend Services, with
// the read-only client's key registered as a project credential.
func smartLab(t *testing.T, app *desktop.App, context desktop.RequestContext) (*connectedlab.FHIRLab, map[string]string) {
	t.Helper()
	lab := connectedlab.StartFHIRLab(t)
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	lab.RequireSMART(&key.PublicKey)
	dir := t.TempDir()
	privateRaw, _ := x509.MarshalPKCS8PrivateKey(key)
	private := filepath.Join(dir, "observer.pem")
	public := filepath.Join(dir, "observer-keys.json")
	ca := filepath.Join(dir, "fhir-ca.pem")
	jwks, _ := json.Marshal(smartbackend.JWKS{Keys: []smartbackend.JWK{{Kty: "EC", Kid: "observer-key", Alg: "ES384", Use: "sig", Crv: "P-384", X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 48))), Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 48)))}}})
	for path, raw := range map[string][]byte{private: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateRaw}), public: jwks, ca: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: lab.Server().Certificate().Raw})} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The administrator registers the signing key in the approved store first.
	if saved := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "observer-signing", Purpose: secret.SourceEndpoint, Store: secret.OSKeychain, Address: lab.Server().Listener.Addr().String(), Command: "/bin/cat", Arguments: []string{private}}); saved.State != desktop.Completed {
		t.Fatalf("register the read-only signing key: %+v", saved)
	}
	return lab, map[string]string{
		"<FHIR_BASE>": lab.Base(), "<FHIR_SERVER_NAME>": "example.com", "<FHIR_CA_FILE>": ca, "<FHIR_RANGE>": "127.0.0.1/32",
		"<TOKEN_ENDPOINT>": lab.Server().URL + "/token", "<OBSERVER_CLIENT_ID>": "lab-client", "<OBSERVER_KEY_REFERENCE>": "observer-signing",
		"<OBSERVER_KEY_ID>": "observer-key", "<OBSERVER_PUBLIC_KEYS_FILE>": public, "<SIGNING_ALGORITHM>": "ES384",
	}
}

// reviews numbers each reviewed click of these tests.
var reviews int

// reviewed runs one reviewed environment action through its review.
func reviewed(t *testing.T, app *desktop.App, context desktop.RequestContext, action desktop.ActionID, ref desktop.ItemRef) desktop.ReviewedActionResult {
	t.Helper()
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: action, Items: []desktop.ItemRef{ref}})
	if review.Review == nil || !review.Review.Ready {
		t.Fatalf("%s review: %+v", action, review.Review.Requirements)
	}
	reviews++
	return app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Review.Token, IntentID: string(action) + "-" + strconv.Itoa(reviews)})
}

// checkFHIR runs the three explicit FHIR checks on a saved environment: one
// verified TLS connection, a token for the read-only client, and the
// CapabilityStatement.
func checkFHIR(t *testing.T, app *desktop.App, context desktop.RequestContext, lab *connectedlab.FHIRLab, ref desktop.ItemRef) {
	t.Helper()
	before := lab.Tokens.Load()
	for _, action := range []desktop.ActionID{desktop.CheckFHIRConnectionAction, desktop.CheckFHIRAuthorizationAction, desktop.CheckFHIRCapabilitiesAction} {
		if result := reviewed(t, app, context, action, ref); result.State != desktop.Completed || result.FHIRCheck == nil {
			t.Fatalf("%s: %+v", action, result)
		}
	}
	if lab.Tokens.Load() <= before {
		t.Fatal("the read-only client was never authorized")
	}
}

func savedOf(t *testing.T, result desktop.ConnectionExampleResult, kind desktop.ItemKind, n int) desktop.ItemRef {
	t.Helper()
	for _, ref := range result.Saved {
		if ref.Kind == kind {
			if n == 0 {
				return ref
			}
			n--
		}
	}
	t.Fatalf("no saved %s in %+v", kind, result.Saved)
	return desktop.ItemRef{}
}

// listenerCertificate writes a self-signed certificate for 127.0.0.1 into the
// project folder and its key outside it, answering the CA pool that trusts it.
func listenerCertificate(t *testing.T, project string) (certificate, key string, pool *x509.CertPool) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Example receiver"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	certificate = filepath.Join(project, "receiver.pem")
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyRaw, _ := x509.MarshalPKCS8PrivateKey(private)
	key = filepath.Join(t.TempDir(), "receiver-key.pem")
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw}), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	return certificate, key, pool
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

// Topology 1: v2 input through an engine to a downstream v2 capture. The
// example's engine input connects over verified TLS under its approved
// destinations and isolation confirmation; its receiver captures the engine's
// output over TLS with the key named in the approved store; the second example
// observes the captured output by its business key with a full horizon.
func TestTheEngineCaptureExampleConnectsCapturesAndObservesThroughNamedObjects(t *testing.T) {
	// The engine input check holds its declared 30 s quiet window; the two
	// v2 topologies wait beside each other.
	t.Parallel()
	app, context := namedProject(t)
	host, port, ca := tlsEndpoint(t)
	certificate, key, pool := listenerCertificate(t, context.Project)
	listenerPort := freePort(t)
	if saved := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "receiver-key", Purpose: secret.MLLPEndpoint, Store: secret.OSKeychain, Address: "127.0.0.1:" + listenerPort, Command: "/bin/cat", Arguments: []string{key}}); saved.State != desktop.Completed {
		t.Fatalf("register the receiver key: %+v", saved)
	}
	imported := importExample(t, app, context, shippedExample("v2-engine-v2-capture", "connection"), map[string]string{
		"<ENGINE_HOST>": host, "<ENGINE_PORT>": port, "<ENGINE_SERVER_NAME>": "example.com", "<ENGINE_CA_FILE>": ca, "<ENGINE_RANGE>": "127.0.0.1/32",
		"<LISTENER_ADDRESS>": "127.0.0.1", "<LISTENER_PORT>": listenerPort, "<LISTENER_CERTIFICATE_FILE>": certificate, "<LISTENER_KEY_REFERENCE>": "receiver-key",
	})
	engine := savedOf(t, imported, desktop.EnvironmentItem, 0)
	if checked := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: engine}); checked.State != desktop.Completed || checked.Report == nil || checked.Report.Outcome != "reachable" {
		t.Fatalf("the engine input's verified TLS check: %+v", checked)
	}
	receiver := savedOf(t, imported, desktop.SourceItem, 0)
	done, progress := startedCapture(t, app, context, receiver, "engine-output")
	connection, err := tls.Dial("tcp", progress.BoundAddress, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal("the engine's output could not reach the receiver over TLS:", err)
	}
	output := "MSH|^~\\&|ENGINE|LAB|RECEIVER|LAB|20300102093000||SIU^S12|OUT-1|P|2.5.1\rSCH|APPT-EXAMPLE-1||||||||||^^^20300102093000\r"
	if _, err := connection.Write(mllp.Frame([]byte(output))); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(connection).ReadBytes(0x1c); err != nil {
		t.Fatal("no acknowledgement from the receiver:", err)
	}
	connection.Close()
	app.FinishCapture()
	captured := awaitCapture(t, done)
	if captured.State != desktop.Completed || captured.CaseRef == nil {
		t.Fatalf("the capture: %+v", captured)
	}
	observed := importExample(t, app, context, shippedExample("v2-engine-v2-capture", "observation"), map[string]string{"<CAPTURE_CASE>": captured.CaseRef.ID})
	observation := savedOf(t, observed, desktop.ObservationItem, 0)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: observation})
	if opened.Draft == nil || opened.Draft.Observation.Connected == nil || opened.Draft.Observation.Connected.Projection == nil || opened.Draft.Observation.Connected.BusinessKeys[0].Variable != "appointment-id" || opened.Draft.Observation.Connected.Completion.HorizonMS != 30000 {
		t.Fatalf("the observation's projection, key mapping and horizon: %+v", opened)
	}
	// The saved observation reads the captured output by the example's key.
	collected := reviewed(t, app, context, desktop.CollectObservationAction, observation)
	if collected.State != desktop.Completed || collected.TypedCollection == nil || len(collected.TypedCollection.Rows) != 1 || collected.TypedCollection.Rows[0].Values[0].Text != "APPT-EXAMPLE-1" {
		t.Fatalf("collect the captured output: %+v %+v", collected, collected.TypedCollection)
	}
}

// Topology 2: v2 input through an engine to the application's FHIR state. The
// engine input and the application's read-only FHIR client are separate
// environments; the observation reads appointments by the explicit identifier
// system the engine's authority maps to, and the engine input links it.
func TestTheApplicationFHIRExampleChecksTheLabAsItsReadOnlyClient(t *testing.T) {
	// The engine input check holds its declared 30 s quiet window; the two
	// v2 topologies wait beside each other.
	t.Parallel()
	app, context := namedProject(t)
	host, port, ca := tlsEndpoint(t)
	lab, values := smartLab(t, app, context)
	values["<ENGINE_HOST>"], values["<ENGINE_PORT>"], values["<ENGINE_SERVER_NAME>"], values["<ENGINE_CA_FILE>"], values["<ENGINE_RANGE>"] = host, port, "example.com", ca, "127.0.0.1/32"
	values["<APPOINTMENT_ID_SYSTEM>"] = connectedlab.AppointmentSystem
	imported := importExample(t, app, context, shippedExample("v2-application-fhir", "connection"), values)
	engine, application := savedOf(t, imported, desktop.EnvironmentItem, 0), savedOf(t, imported, desktop.EnvironmentItem, 1)
	observation := savedOf(t, imported, desktop.ObservationItem, 0)
	if checked := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: engine}); checked.State != desktop.Completed {
		t.Fatalf("the engine input's verified TLS check: %+v", checked)
	}
	checkFHIR(t, app, context, lab, application)
	linked := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: engine})
	if linked.Draft == nil || linked.Draft.Links == nil || linked.Draft.Links.Observation != observation.ID || linked.Draft.ResetPlan == nil || len(linked.Draft.ResetPlan.Actions) != 2 {
		t.Fatalf("the engine input's observation link and isolation reset: %+v", linked)
	}
	read := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: observation})
	if read.Draft == nil || read.Draft.Observation.Connected.Environment != application.ID || read.Draft.Observation.Connected.FHIR.Criteria[0].System != connectedlab.AppointmentSystem {
		t.Fatalf("the observation's environment and identifier authority: %+v", read)
	}
}

// Topology 3: FHIR-native input with its declared downstream encounter. The
// read-only client checks the lab; the downstream observation declares its
// authoritative boundary and the environment's isolation confirmation.
func TestTheFHIRNativeExampleChecksTheLabAndDeclaresItsDownstreamObservation(t *testing.T) {
	app, context := namedProject(t)
	lab, values := smartLab(t, app, context)
	values["<ENCOUNTER_ID_SYSTEM>"] = "urn:readmit-lab:encounter"
	imported := importExample(t, app, context, shippedExample("fhir-native-downstream", "connection"), values)
	application := savedOf(t, imported, desktop.EnvironmentItem, 0)
	checkFHIR(t, app, context, lab, application)
	read := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: savedOf(t, imported, desktop.ObservationItem, 0)})
	if read.Draft == nil || read.Draft.Observation.Connected.FHIR.Boundary != "authoritative-application-api" || read.Draft.Observation.Connected.FHIR.Resource != "Encounter" {
		t.Fatalf("the downstream observation: %+v", read)
	}
}

// An example is read strictly and never saved with a placeholder: a missing,
// marked or wrongly typed value saves nothing, and so does a document that
// declares a placeholder it never uses, uses one it never declares or holds a
// member its object's editor does not save.
func TestAConnectionExampleSavesNothingUntilEveryValueIsGiven(t *testing.T) {
	app, context := namedProject(t)
	path := shippedExample("fhir-native-downstream", "connection")
	listed := func() int {
		total := 0
		for _, kind := range []desktop.ItemKind{desktop.EnvironmentItem, desktop.ObservationItem, desktop.SourceItem} {
			if page := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: kind}).Page; page != nil {
				total += len(page.Items)
			}
		}
		return total
	}
	values := map[string]string{"<FHIR_BASE>": "https://fhir.example.test/fhir", "<FHIR_SERVER_NAME>": "fhir.example.test", "<FHIR_CA_FILE>": "/nonexistent/ca.pem", "<FHIR_RANGE>": "192.0.2.0/24", "<TOKEN_ENDPOINT>": "https://fhir.example.test/token", "<OBSERVER_CLIENT_ID>": "client", "<OBSERVER_KEY_REFERENCE>": "observer", "<OBSERVER_KEY_ID>": "key", "<OBSERVER_PUBLIC_KEYS_FILE>": "/nonexistent/keys.json", "<SIGNING_ALGORITHM>": "ES384"}
	for name, change := range map[string]func(map[string]string){
		"a missing value":     func(v map[string]string) { delete(v, "<FHIR_BASE>") },
		"a placeholder value": func(v map[string]string) { v["<FHIR_BASE>"] = "<FHIR_BASE>" },
		"a control character": func(v map[string]string) { v["<OBSERVER_CLIENT_ID>"] = "client\nsecond" },
	} {
		given := map[string]string{}
		for k, v := range values {
			given[k] = v
		}
		change(given)
		result := app.ImportConnectionExample(desktop.ConnectionExampleRequest{Context: context, Path: path, Import: true, Values: given, IntentID: "refused-" + strings.ReplaceAll(name, " ", "-")})
		if result.State != desktop.Failed || len(result.Saved) != 0 || len(result.Problems) == 0 || !strings.HasPrefix(result.Problems[0].Field, "values.") {
			t.Errorf("%s: %+v", name, result)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(string) string{
		"an undeclared placeholder": func(s string) string { return strings.Replace(s, `"<FHIR_BASE>"`, `"<UNDECLARED_BASE>"`, 1) },
		"an unused placeholder":     func(s string) string { return strings.Replace(s, `"<FHIR_RANGE>"`, `"192.0.2.0/24"`, 1) },
		"an unknown member":         func(s string) string { return strings.Replace(s, `"topology"`, `"author": "x", "topology"`, 1) },
		"a member its editor never saves": func(s string) string {
			return strings.Replace(s, `"name": "Example application FHIR API",`, `"name": "Example application FHIR API", "test_document": "{}",`, 1)
		},
		"a placeholder in a number position": func(s string) string {
			return strings.Replace(s, `"pages": 16`, `"pages": "<FHIR_RANGE>"`, 1)
		},
	} {
		edited := filepath.Join(t.TempDir(), "example.json")
		if err := os.WriteFile(edited, []byte(edit(string(raw))), 0o600); err != nil {
			t.Fatal(err)
		}
		if result := app.ImportConnectionExample(desktop.ConnectionExampleRequest{Context: context, Path: edited, Import: true, Values: values, IntentID: "edited"}); result.State != desktop.Failed || len(result.Saved) != 0 {
			t.Errorf("%s: %+v", name, result)
		}
	}
	if listed() != 0 {
		t.Fatal("a refused import saved an object")
	}
}

// The shipped examples carry no secret, key, patient value or real endpoint:
// every value a customer supplies is a declared placeholder, and nothing else
// names a host, an address, a path or key material.
func TestShippedConnectionExamplesHoldOnlyPlaceholdersForCustomerValues(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "samples", "connections", "*", "*.json"))
	if err != nil || len(paths) != 4 {
		t.Fatalf("the shipped examples: %v %v", paths, err)
	}
	address := regexp.MustCompile(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|https?://|-----BEGIN|/Users/|/home/|[A-Za-z]:\\`)
	topologies := map[string]bool{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if found := address.FindString(string(raw)); found != "" {
			t.Errorf("%s names %q instead of a placeholder", path, found)
		}
		var example struct {
			Schema   string `json:"schema"`
			Topology string `json:"topology"`
		}
		if err := json.Unmarshal(raw, &example); err != nil || example.Schema != desktop.ConnectionExampleSchema {
			t.Errorf("%s is not a connection example: %v", path, err)
		}
		topologies[example.Topology] = true
		if filepath.Base(filepath.Dir(path)) != example.Topology {
			t.Errorf("%s is filed under another topology than %s", path, example.Topology)
		}
	}
	if len(topologies) != 3 {
		t.Fatalf("the examples cover %v, not the three topologies", topologies)
	}
}

// An import repeated under its submission saves nothing twice: the objects
// saved before a refusal are answered again and only what was missing is
// saved, so correcting a value and pressing Import again leaves one of each.
func TestRetryingAnImportUnderItsSubmissionSavesEachObjectOnce(t *testing.T) {
	app, context := namedProject(t)
	_, values := smartLab(t, app, context)
	path := shippedExample("fhir-native-downstream", "connection")
	values["<ENCOUNTER_ID_SYSTEM>"] = "urn:readmit-lab:encounter"
	request := desktop.ConnectionExampleRequest{Context: context, Path: path, Import: true, Values: values, IntentID: "retried-import"}
	first := app.ImportConnectionExample(request)
	again := app.ImportConnectionExample(request)
	if first.State != desktop.Completed || again.State != desktop.Completed || len(again.Saved) != len(first.Saved) {
		t.Fatalf("a repeated import: %+v %+v", first, again)
	}
	for i := range first.Saved {
		if first.Saved[i] != again.Saved[i] {
			t.Fatalf("a repeated import saved another object: %+v %+v", first.Saved, again.Saved)
		}
	}
	for _, kind := range []desktop.ItemKind{desktop.EnvironmentItem, desktop.ObservationItem} {
		if page := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: kind}).Page; page == nil || len(page.Items) != 1 {
			t.Fatalf("one %s after a repeated import: %+v", kind, page)
		}
	}
}

// The recovery paths an administrator meets after importing an example: a CA
// certificate that does not verify the server is reported by Test connection
// and fixed in the connection's Edit; a read-only client missing a resource's
// scope makes a collection fail, never an empty result, until the scope is
// granted.
func TestAnImportedFHIRExampleRecoversFromAnUntrustedCertificateAndAMissingScope(t *testing.T) {
	app, context := namedProject(t)
	lab, values := smartLab(t, app, context)
	values["<ENCOUNTER_ID_SYSTEM>"] = "urn:readmit-lab:encounter"
	trusted := values["<FHIR_CA_FILE>"]
	// Another authority's certificate, which never issued the lab's.
	wrong, _, _ := listenerCertificate(t, t.TempDir())
	values["<FHIR_CA_FILE>"] = wrong
	imported := importExample(t, app, context, shippedExample("fhir-native-downstream", "connection"), values)
	application := savedOf(t, imported, desktop.EnvironmentItem, 0)
	edit := func(ref desktop.ItemRef, change func(*desktop.ItemDraft)) desktop.ItemRef {
		t.Helper()
		opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
		if opened.Draft == nil {
			t.Fatalf("open %s: %+v", ref.ID, opened)
		}
		change(opened.Draft)
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: ref.Kind, Item: ref.ID, BaseRevision: opened.Ref.Revision, Draft: *opened.Draft, IntentID: "edit-" + ref.ID + "-" + strconv.Itoa(len(opened.Ref.Revision)) + opened.Ref.Revision})
		if saved.Outcome != desktop.SavedOutcome {
			t.Fatalf("edit %s: %+v", ref.ID, saved)
		}
		return *saved.Saved
	}
	untrusted := reviewed(t, app, context, desktop.CheckFHIRConnectionAction, application)
	if untrusted.State != desktop.Failed || untrusted.FHIRCheck == nil || untrusted.FHIRCheck.Outcome == "reachable" || !strings.Contains(untrusted.Reason, "Verify TLS") {
		t.Fatalf("an untrusted certificate: %+v %+v", untrusted, untrusted.FHIRCheck)
	}
	application = edit(application, func(d *desktop.ItemDraft) { d.FHIR.CAFile = trusted })
	if verified := reviewed(t, app, context, desktop.CheckFHIRConnectionAction, application); verified.State != desktop.Completed || verified.FHIRCheck.Outcome != "reachable" {
		t.Fatalf("the corrected CA certificate: %+v %+v", verified, verified.FHIRCheck)
	}
	// A literal identifier lets a standalone collection read the search; the
	// read-only client then loses the Encounter scope.
	observation := edit(savedOf(t, imported, desktop.ObservationItem, 0), func(d *desktop.ItemDraft) { d.Observation.Connected.FHIR.Criteria[0].Value = "ENC-LAB-1" })
	application = edit(application, func(d *desktop.ItemDraft) { d.FHIR.Scopes = []string{"system/Appointment.rs"} })
	if checked := reviewed(t, app, context, desktop.CheckFHIRCapabilitiesAction, application); checked.State != desktop.Completed {
		t.Fatalf("capabilities: %+v", checked)
	}
	before := lab.Bearers.Load()
	narrow := reviewed(t, app, context, desktop.CollectObservationAction, observation)
	if narrow.State != desktop.Failed || narrow.Collected == nil || narrow.Collected.Status != "acquisition-refused" || narrow.Collected.Trustworthy || narrow.Collected.Records != nil {
		t.Fatalf("a refused search was read as a result: %+v %+v", narrow, narrow.Collected)
	}
	if lab.Bearers.Load() != before {
		t.Fatal("the lab granted a search the client has no scope for")
	}
	application = edit(application, func(d *desktop.ItemDraft) {
		d.FHIR.Scopes = []string{"system/Appointment.rs", "system/Encounter.rs"}
	})
	if checked := reviewed(t, app, context, desktop.CheckFHIRCapabilitiesAction, application); checked.State != desktop.Completed {
		t.Fatalf("capabilities: %+v", checked)
	}
	if granted := reviewed(t, app, context, desktop.CollectObservationAction, observation); granted.State != desktop.Completed || granted.TypedCollection == nil || lab.Bearers.Load() == before {
		t.Fatalf("the granted scope: %+v %+v", granted, granted.TypedCollection)
	}
}
