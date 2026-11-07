package acceptance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResidentHealthObservationUsesOnlyBoundedSequentialPublicGETs(t *testing.T) {
	var calls, active atomic.Int32
	var boundaryViolation atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if active.Add(1) != 1 {
			t.Error("health observer issued overlapping calls")
		}
		defer active.Add(-1)
		if r.Method != http.MethodGet || r.URL.Path != "/prefix/health/live" || r.ContentLength != 0 {
			if !boundaryViolation.Swap(true) {
				t.Error("health observation crossed the inference or body boundary")
			}
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"live"}`))
	}))
	defer server.Close()
	client, err := NewPublicClient(server.URL+"/prefix", time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var facts residentHealthObservation
	for i := 0; i < residentHealthObservationMaxCalls/residentHealthObservationBatch; i++ {
		if err := runResidentHealthObservation(ctx, client, &facts, residentHealthObservationBatch); err != nil {
			t.Fatal(err)
		}
	}
	if facts.Calls != residentHealthObservationMaxCalls || int(calls.Load()) != facts.Calls || facts.Errors != 0 || !facts.FinishedAt.After(facts.StartedAt) {
		t.Fatal("health observation did not record actual bounded calls and phase timing")
	}
	deadline, _ := ctx.Deadline()
	if !facts.Deadline.Equal(deadline) {
		t.Fatal("health observation reset its caller deadline")
	}
	if err := runResidentHealthObservation(ctx, client, &facts, 1); err == nil || calls.Load() != residentHealthObservationMaxCalls {
		t.Fatal("health observation exceeded its per-phase ceiling")
	}
}

func TestResidentHealthObservationFailsClosedAndCancels(t *testing.T) {
	for _, mode := range []string{"status", "oversized", "malformed", "redirect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var actual atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actual.Add(1)
				switch mode {
				case "status":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", 4097)))
				case "malformed":
					_, _ = w.Write([]byte(`{"status":"unknown"}`))
				case "redirect":
					http.Redirect(w, r, "/v1/audio/transcriptions", http.StatusFound)
				case "cancel":
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			client, _ := NewPublicClient(server.URL, 3*time.Second, 1<<20)
			defer client.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			var facts residentHealthObservation
			started := time.Now()
			err := runResidentHealthObservation(ctx, client, &facts, 10)
			if mode == "cancel" && time.Since(started) >= time.Second {
				t.Fatal("health observation extended the caller deadline to the transport timeout")
			}
			if err == nil || facts.Calls != 1 || facts.Errors != 1 || actual.Load() != 1 || facts.FinishedAt.IsZero() {
				t.Fatal("failed health work was retried, uncounted, redirected or accepted")
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("health workload lost caller cancellation")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var facts residentHealthObservation
	if err := runResidentHealthObservation(ctx, nil, &facts, 1); err == nil || facts.Calls != 0 {
		t.Fatal("expired/invalid health observation admitted work")
	}
}
