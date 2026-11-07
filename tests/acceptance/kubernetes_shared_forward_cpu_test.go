package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestP08HTTPForwardRequiresMatchingPodLog(t *testing.T) {
	for _, mode := range []string{"matching", "missing", "wrong-route", "duplicate", "log-error", "wrong-header"} {
		t.Run(mode, func(t *testing.T) {
			id := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id = r.Header.Get("X-Request-ID")
				if mode != "wrong-header" {
					w.Header().Set("X-Request-ID", id)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			readLogs := func(context.Context) ([]byte, error) {
				if mode == "log-error" {
					return nil, fmt.Errorf("private diagnostic")
				}
				if mode == "missing" {
					return []byte("{}\n"), nil
				}
				route := "/health/ready"
				if mode == "wrong-route" {
					route = "/health/live"
				}
				line := fmt.Sprintf("{\"event\":\"request_completed\",\"component\":\"api\",\"request_id\":%q,\"method\":\"GET\",\"route\":%q,\"status\":%d}\n", id, route, http.StatusOK)
				if mode == "duplicate" {
					line += line
				}
				return []byte(line), nil
			}
			err = p08VerifyHTTPForward(t.Context(), client, readLogs)
			if (err == nil) != (mode == "matching") {
				t.Fatalf("forward correlation failure=%t", err != nil)
			}
		})
	}
}
