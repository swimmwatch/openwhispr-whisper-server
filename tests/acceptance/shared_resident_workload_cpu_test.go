package acceptance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedResidentWorkloadRequiresRecognizedPreflightAndAllSamples(t *testing.T) {
	for _, tc := range []struct {
		name          string
		preflight     string
		failAt        int32
		wantPreflight bool
		wantCalls     int32
		wantSuccess   bool
	}{
		{"complete", "My fellow Americans, ask what you can do.", 0, true, 31, true},
		{"unrecognized", "Unrelated speech.", 0, false, 1, false},
		{"failed-sample", "My fellow Americans, ask what you can do.", 2, true, 31, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if call == tc.failAt {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":{"code":"not_ready"}}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"text": tc.preflight})
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			result, err := measureSharedResidentWorkload(t.Context(), client, []byte("synthetic"), "base", 30)
			if (err == nil) != tc.wantSuccess || result.PreflightPassed != tc.wantPreflight || calls.Load() != tc.wantCalls {
				t.Fatalf("incorrect workload outcome: err=%v preflight=%t calls=%d", err, result.PreflightPassed, calls.Load())
			}
			if tc.failAt != 0 && result.Summary.Errors != 1 {
				t.Fatal("failed sample was not retained")
			}
		})
	}
}
