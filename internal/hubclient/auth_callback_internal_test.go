package hubclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClosingAuthFlowFinishesTheCallbackResponse(t *testing.T) {
	for _, before := range []bool{false, true} {
		name := "after-callback"
		if before {
			name = "before-callback-lock"
		}
		t.Run(name, func(t *testing.T) {
			flow := &AuthFlow{State: "synthetic-state", resultChan: make(chan authCallbackResult, 1)}
			finish := make(chan struct{})
			entered := make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				if before {
					<-finish
				}
				flow.handleCallback(w, r)
				if !before {
					<-finish
				}
			}))
			stopping := make(chan struct{})
			server.Config.RegisterOnShutdown(func() { close(stopping) })
			server.Start()
			defer server.Close()
			flow.server, flow.listener = server.Config, server.Listener
			result := make(chan error, 1)
			go func() {
				response, err := http.Get(server.URL + "/callback?code=synthetic-code&state=synthetic-state")
				if err == nil {
					var body []byte
					body, err = io.ReadAll(response.Body)
					response.Body.Close()
					if err == nil && (response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Authentication Complete")) {
						err = io.ErrUnexpectedEOF
					}
				}
				result <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			<-entered
			if !before {
				if code, err := flow.WaitForCallback(ctx); err != nil || code != "synthetic-code" {
					close(finish)
					t.Fatalf("callback %q: %v", code, err)
				}
			}
			closed := make(chan struct{})
			go func() { flow.Close(); close(closed) }()
			select {
			case <-stopping:
			case <-ctx.Done():
				close(finish)
				t.Fatal("listener did not begin graceful shutdown")
			}
			close(finish)
			if err := <-result; err != nil {
				t.Fatalf("callback response lost on close: %v", err)
			}
			<-closed

		})
	}
}
