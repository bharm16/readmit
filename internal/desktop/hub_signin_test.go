package desktop_test

import (
	"encoding/json/v2"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
)

// A hub sign-in that fails or is cancelled ends there (#326): the loopback
// listener it opened is closed at once, the operation slot is free for the
// next action, the connection the person made is still theirs, nothing is
// kept for a later call to complete, and no request reaches the hub or the
// IdP that the attempt did not already make. Each case is a separate attempt
// on one connected window, and the last attempt signs in, so a failure never
// leaves the window unable to recover.
func TestAFailedHubSignInClosesItsListenerAndFreesTheSlot(t *testing.T) {
	var hubRequests, exchanges atomic.Int64
	hub := http.NewServeMux()
	hub.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hubRequests.Add(1)
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	subject, scopes := "analyst@hospital.org", []string{"evidence.read"}
	tok := signedHubAccessToken(t, subject, scopes)
	// A code held at the token endpoint is answered only once the test lets
	// it go, so a cancel can arrive while the IdP is answering.
	held, release := make(chan struct{}), make(chan struct{})
	fixture := newConnectedHubApp(t, hub, subject, scopes, func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		switch r.FormValue("code") {
		case "refused-at-token":
			w.WriteHeader(http.StatusBadRequest)
			return
		case "held-at-token":
			close(held)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 1800})
	})
	app := fixture.app

	cases := []struct {
		name      string
		want      desktop.State
		reason    string
		exchanges int64
		finish    func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult
	}{
		{"the IdP refuses the sign-in", desktop.Failed, "IdP returned error", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				go returnToLoopback(flow, "error=access_denied&state="+signInState(t, flow))
				return app.CompleteHubAuth("", "")
			}},
		{"the browser returns a forged state", desktop.Failed, "state mismatch", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				go returnToLoopback(flow, "code=forged&state=forged")
				return app.CompleteHubAuth("", "")
			}},
		{"the window names a state the flow did not issue", desktop.Failed, "state mismatch", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				return app.CompleteHubAuth("typed-code", "forged")
			}},
		{"the browser was closed and the person cancels", desktop.Cancelled, "cancelled", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				done := make(chan desktop.HubResult, 1)
				go func() { done <- app.CompleteHubAuth("", "") }()
				// No browser returns. The person presses the hub panel's
				// cancel, which names the sign-in; pressed before the wait
				// has claimed the slot it does nothing, so it is pressed
				// until the wait ends.
				deadline := time.After(10 * time.Second)
				for {
					select {
					case res := <-done:
						return res
					case <-deadline:
						t.Fatal("cancelling the sign-in did not end its wait")
					case <-time.After(10 * time.Millisecond):
						app.Cancel("hub-sign-in")
					}
				}
			}},
		{"the browser never returns", desktop.Failed, "timed out", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				return desktop.CompleteHubAuthWithinForTest(app, "", "", 100*time.Millisecond)
			}},
		{"the IdP refuses the code", desktop.Failed, "token exchange failed", 1,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				go returnToLoopback(flow, "code=refused-at-token&state="+signInState(t, flow))
				return app.CompleteHubAuth("", "")
			}},
		{"the window's cancel command arrives while the IdP answers", desktop.Cancelled, "cancelled", 1,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				done := make(chan desktop.HubResult, 1)
				go func() { done <- app.CompleteHubAuth("", "") }()
				go returnToLoopback(flow, "code=held-at-token&state="+signInState(t, flow))
				<-held
				app.Cancel("")
				close(release)
				return <-done
			}},
		{"another operation holds the slot when the window asks to wait", desktop.Busy, "already running", 0,
			func(t *testing.T, flow desktop.HubAuthUrlResult) desktop.HubResult {
				open, closeDialog := make(chan struct{}), make(chan struct{})
				fixture.dialog.before = func() {
					close(open)
					<-closeDialog
				}
				chosen := make(chan desktop.ImportSourcesResult, 1)
				go func() { chosen <- app.ChooseImportSources("folder") }()
				<-open
				res := app.CompleteHubAuth("", "")
				close(closeDialog)
				<-chosen
				fixture.dialog.before = nil
				return res
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			flow := app.StartHubAuth()
			if flow.State != desktop.Completed || flow.Port <= 0 {
				t.Fatalf("start sign-in: %+v", flow)
			}
			hubBefore, exchangesBefore := hubRequests.Load(), exchanges.Load()

			res := c.finish(t, flow)
			if res.State != c.want || !strings.Contains(res.Reason, c.reason) {
				t.Fatalf("sign-in answered %+v, want %s naming %q", res, c.want, c.reason)
			}
			// The listener is closed as the attempt ends, so a browser that
			// returns late reaches nothing.
			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", flow.Port), time.Second); err == nil {
				conn.Close()
				t.Errorf("the sign-in listener on port %d is still open", flow.Port)
			}
			// The slot is free for the next action, and the connection the
			// person made is still in place, not signed in.
			if status := app.HubStatus(); status.State != desktop.Completed || !status.Connected || status.Authenticated {
				t.Errorf("the next action after a failed sign-in answered %+v", status)
			}
			// Nothing of the attempt is kept for another call to complete.
			if again := desktop.CompleteHubAuthWithinForTest(app, "", "", 100*time.Millisecond); again.State != desktop.Failed || !strings.Contains(again.Reason, "start sign-in first") {
				t.Errorf("completing again without a new sign-in answered %+v", again)
			}
			if sent := hubRequests.Load() - hubBefore; sent != 0 {
				t.Errorf("the failed sign-in sent %d requests to the hub", sent)
			}
			if sent := exchanges.Load() - exchangesBefore; sent != c.exchanges {
				t.Errorf("the failed sign-in asked the IdP for a token %d times, want %d", sent, c.exchanges)
			}
		})
	}

	// Signing in again works, and a duplicate completion while the sign-in
	// waits is refused busy without ending the sign-in it collided with.
	flow := app.StartHubAuth()
	if flow.State != desktop.Completed {
		t.Fatalf("start sign-in after the failures: %+v", flow)
	}
	done := make(chan desktop.HubResult, 2)
	go func() { done <- app.CompleteHubAuth("", "") }()
	time.Sleep(100 * time.Millisecond) // the sign-in is waiting for the browser
	go func() { done <- app.CompleteHubAuth("", "") }()
	if duplicate := <-done; duplicate.State != desktop.Busy {
		t.Fatalf("a duplicate completion answered %+v, want busy", duplicate)
	}
	go returnToLoopback(flow, "code=accepted&state="+signInState(t, flow))
	if res := <-done; res.State != desktop.Completed || !res.Authenticated || res.Subject != subject {
		t.Fatalf("signing in after the failures answered %+v", res)
	}
}

// returnToLoopback is the system browser arriving at the sign-in listener
// with the query the IdP redirected it with.
func returnToLoopback(flow desktop.HubAuthUrlResult, query string) {
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?%s", flow.Port, query)); err == nil {
		resp.Body.Close()
	}
}

// signInState is the state the flow sent the browser to the IdP with.
func signInState(t *testing.T, flow desktop.HubAuthUrlResult) string {
	t.Helper()
	authorization, err := url.Parse(flow.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	return authorization.Query().Get("state")
}
