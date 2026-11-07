package acceptance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeadlineQueueOrderRejectsMissingOrUnexpectedWork(t *testing.T) {
	active := `{"event":"queue_admitted","request_id":"expiry-active"}` + "\n"
	waiting := `{"event":"queue_admitted","request_id":"expiry-waiting"}` + "\n"
	dispatch := `{"event":"queue_dispatched","request_id":"expiry-active"}` + "\n"
	unexpected := `{"event":"queue_dispatched","request_id":"expiry-waiting"}` + "\n"
	for _, tc := range []struct {
		name, logs string
		valid      bool
	}{
		{"valid", active + dispatch + waiting, true},
		{"missing admission", active + dispatch, false},
		{"reordered admission", waiting + active + dispatch, false},
		{"waiting dispatched", active + dispatch + waiting + unexpected, false},
		{"duplicate dispatch", active + dispatch + waiting + dispatch, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (validateDeadlineQueueOrder([]byte(tc.logs)) == nil) != tc.valid {
				t.Fatal("incorrect deadline order verdict")
			}
		})
	}
}

func TestDevelopmentReadinessWaitsForRecovery(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/health/ready" || calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := waitDevelopmentReady(context.Background(), server.URL, time.Second); err != nil || calls < 2 {
		t.Fatal("readiness did not wait for the recovered API")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitDevelopmentReady(ctx, server.URL, time.Second) == nil {
		t.Fatal("cancelled readiness observation succeeded")
	}
}
