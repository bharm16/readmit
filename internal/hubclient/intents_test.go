package hubclient_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
)

// reviewHub is a project's review log served over mutual TLS the way the hub
// serves it, built from the shared protocol types: history reads, strictly
// decoded review commands recorded at the head they expect, and artifact
// uploads kept by digest. It records every command posted.
type reviewHub struct {
	events   []hubprotocol.ReviewEvent
	uploaded map[string][]byte
	posted   []hubprotocol.ReviewCommand
}

func newReviewHub(t *testing.T) (*reviewHub, *hubclient.Client) {
	t.Helper()
	hub := &reviewHub{uploaded: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/projects/alpha/history", func(w http.ResponseWriter, r *http.Request) {
		_ = json.MarshalWrite(w, hubprotocol.ReviewHistory{Schema: hubprotocol.ReviewHistoryV2, Head: len(hub.events), Events: hub.events})
	})
	mux.HandleFunc("POST /v2/projects/alpha/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		command, err := hubprotocol.DecodeReviewCommand(body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		hub.posted = append(hub.posted, command)
		if command.Expected != len(hub.events) {
			w.WriteHeader(http.StatusConflict)
			return
		}
		schema := hubprotocol.ReviewEventV1
		if hubprotocol.IsSupport(command) {
			schema = hubprotocol.ReviewEventV2
		}
		event := hubprotocol.ReviewEvent{Schema: schema, Project: "alpha", Sequence: len(hub.events) + 1, Issuer: "https://idp.example.com", Actor: "ana", At: "2026-09-24T12:00:00Z", Command: command}
		hub.events = append(hub.events, event)
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, event)
	})
	mux.HandleFunc("PUT /v1/projects/alpha/artifacts/{digest}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hub.uploaded[r.PathValue("digest")] = body
		w.WriteHeader(http.StatusCreated)
	})

	ca := newTestAuthority(t, "review-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "ana", false)
	pair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.pem)
	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	dir := t.TempDir()
	caPath, certPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "client.pem")
	if os.WriteFile(caPath, ca.pem, 0o600) != nil || os.WriteFile(certPath, clientCert, 0o600) != nil {
		t.Fatal("cannot write the client identity")
	}
	idp := hubclient.IdPConfig{Issuer: "https://idp.example.com", ClientID: "desktop", Audience: "hub",
		AuthorizeEndpoint: "https://idp.example.com/authorize", TokenEndpoint: "https://idp.example.com/token", Scopes: []string{"evidence.write"}}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	session, err := hubclient.ValidateAccessToken(issueTestToken(t, key, "key-1", idp.Issuer, "ana", idp.Audience, idp.ClientID, idp.Scopes, now.Unix(), now.Unix()+1800), idp, now)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(context.Background(), hubclient.Config{Schema: hubclient.Schema, Hub: server.URL, CA: caPath, Certificate: certPath,
		Key: hubclient.KeyReference{Command: createKeyProvider(t, clientKey)}, IdP: idp, Projects: []string{"alpha"}}, session)
	if err != nil {
		t.Fatal(err)
	}
	return hub, client
}

// verified writes data as the file the application verified, and answers
// its path and digest.
func verified(t *testing.T, data string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verified.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(data))
	return path, hex.EncodeToString(sum[:])
}

// A release review request uploads the verified bytes and names them; an
// approval answers the last request naming the same release; a refusal the
// history decides, or bytes that changed after they were verified, send no
// command.
func TestAReleaseReviewIsBuiltFromTheHistoryItIsPostedAgainst(t *testing.T) {
	ctx := context.Background()
	hub, client := newReviewHub(t)
	path, release := verified(t, `{"release":"booking"}`)

	if _, _, err := client.PostReleaseReview(ctx, hubclient.ReleaseReview{Project: "alpha", ID: "grant", Kind: "grant", Release: release, Text: "x"}); !errors.Is(err, hubclient.ErrReleaseReviewKind) {
		t.Fatalf("another kind: %v", err)
	}
	if _, _, err := client.PostReleaseReview(ctx, hubclient.ReleaseReview{Project: "alpha", ID: "ok-1", Kind: "approval", Release: release, Text: "approved"}); !errors.Is(err, hubclient.ErrNoReleaseRequest) {
		t.Fatalf("an approval with no request: %v", err)
	}
	_, other := verified(t, `{"release":"other"}`)
	if _, _, err := client.PostReleaseReview(ctx, hubclient.ReleaseReview{Project: "alpha", ID: "ask-0", Kind: "review-request", Release: other, Path: path, Recipient: "rui", Text: "review"}); err == nil || !strings.Contains(err.Error(), "does not name the reviewed bytes") {
		t.Fatalf("bytes changed after verification: %v", err)
	}
	if len(hub.posted) != 0 {
		t.Fatalf("refused reviews sent %d commands", len(hub.posted))
	}

	event, replay, err := client.PostReleaseReview(ctx, hubclient.ReleaseReview{Project: "alpha", ID: "ask-1", Kind: "review-request", Release: release, Path: path, Recipient: "rui", Text: "review"})
	want := hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "ask-1", Kind: "review-request", Evidence: release, Recipient: "rui", Text: "review", Release: release}
	if err != nil || replay || event.Command != want || string(hub.uploaded[release]) != `{"release":"booking"}` {
		t.Fatalf("request: %+v %v %v", event.Command, replay, err)
	}
	event, _, err = client.PostReleaseReview(ctx, hubclient.ReleaseReview{Project: "alpha", ID: "ok-1", Kind: "approval", Release: release, Recipient: "ignored", Text: "approved"})
	want = hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "ok-1", Expected: 1, Kind: "approval", Evidence: release, Parent: "ask-1", Text: "approved", Release: release}
	if err != nil || event.Command != want {
		t.Fatalf("approval: %+v %v", event.Command, err)
	}
}

// A sharing decision binds to the policy in force the history derives, and a
// request or approval the history cannot bind is refused before anything is
// sent.
func TestASupportReviewBindsToThePolicyInForce(t *testing.T) {
	ctx := context.Background()
	hub, client := newReviewHub(t)
	policyPath, policy := verified(t, `{"policy":1}`)
	summaryPath, summary := verified(t, `{"summary":1}`)
	request := hubclient.SupportReview{Project: "alpha", ID: "request", Kind: "support-request", Digest: summary, Path: summaryPath, Recipient: "rui", SummaryPolicy: policy}

	for name, c := range map[string]struct {
		review hubclient.SupportReview
		want   error
	}{
		"another kind":                {hubclient.SupportReview{Project: "alpha", ID: "grant", Kind: "grant"}, hubclient.ErrSupportReviewKind},
		"a request with no policy":    {request, hubclient.ErrNoSupportPolicy},
		"an approval with no request": {hubclient.SupportReview{Project: "alpha", ID: "approve", Kind: "support-approval", Digest: summary}, hubclient.ErrNoSupportRequest},
	} {
		if _, _, err := client.PostSupportReview(ctx, c.review); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	event, _, err := client.PostSupportReview(ctx, hubclient.SupportReview{Project: "alpha", ID: "policy", Kind: "support-policy", Digest: policy, Path: policyPath})
	want := hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV2, ID: "policy", Kind: "support-policy", Evidence: policy, Text: "support"}
	if err != nil || event.Command != want || string(hub.uploaded[policy]) != `{"policy":1}` {
		t.Fatalf("policy: %+v %v", event.Command, err)
	}
	misnamed := request
	misnamed.SummaryPolicy = summary
	if _, _, err := client.PostSupportReview(ctx, misnamed); !errors.Is(err, hubclient.ErrOtherSupportPolicy) {
		t.Fatalf("a summary naming another policy: %v", err)
	}
	if len(hub.posted) != 1 {
		t.Fatalf("refused decisions sent %d commands", len(hub.posted)-1)
	}
	event, _, err = client.PostSupportReview(ctx, request)
	want = hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV2, ID: "request", Expected: 1, Kind: "support-request", Evidence: summary, Parent: "policy", Recipient: "rui", Text: "support", Release: policy}
	if err != nil || event.Command != want || string(hub.uploaded[summary]) != `{"summary":1}` {
		t.Fatalf("request: %+v %v", event.Command, err)
	}
	event, _, err = client.PostSupportReview(ctx, hubclient.SupportReview{Project: "alpha", ID: "approve", Kind: "support-approval", Digest: summary})
	want = hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV2, ID: "approve", Expected: 2, Kind: "support-approval", Evidence: summary, Parent: "request", Text: "support", Release: policy}
	if err != nil || event.Command != want {
		t.Fatalf("approval: %+v %v", event.Command, err)
	}
}
