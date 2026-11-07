package acceptance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func steadyFixture() TranscriptionWorkload {
	return TranscriptionWorkload{
		Audio: []byte("public fixture"), Filename: "sample.wav", Fields: []FormField{{"model", "base"}},
		AudioDuration: time.Second, Samples: 30,
	}
}

func TestPublicSteadyStateMeasurements(t *testing.T) {
	var active, maximum, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); count > previous && !maximum.CompareAndSwap(previous, count); previous = maximum.Load() {
		}
		current := calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if current <= 5 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"not ready","type":"service_unavailable_error","param":null,"code":"not_ready"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"text":"speech"}`)
	}))
	defer server.Close()
	summary, err := MeasurePublicSteadyState(t.Context(), newPublicClient(t, server.URL, 1024), steadyFixture())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 30 || maximum.Load() != 1 || summary.Successes != 25 || summary.Errors != 5 || summary.Public == nil || summary.Public.Count != 25 {
		t.Fatal("steady workload lost attempts, ran concurrently or counted failed requests as successful samples")
	}
	if summary.Queue != nil || summary.Backend != nil || summary.APIOverhead != nil || summary.BackendAudioRatio != nil || summary.Startup != nil || summary.ModelLoad != nil {
		t.Fatal("public workload manufactured unobserved backend or startup measurements")
	}
}

func TestPublicSteadyStateInvalidInputsDoNotSubmitWork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	client := newPublicClient(t, server.URL, 1024)
	for _, change := range []func(*TranscriptionWorkload){
		func(w *TranscriptionWorkload) { w.Samples = 29 },
		func(w *TranscriptionWorkload) { w.Samples = -1 },
		func(w *TranscriptionWorkload) { w.AudioDuration = 0 },
		func(w *TranscriptionWorkload) { w.Audio = nil },
		func(w *TranscriptionWorkload) { w.Filename = "" },
	} {
		workload := steadyFixture()
		change(&workload)
		if _, err := MeasurePublicSteadyState(t.Context(), client, workload); err == nil {
			t.Fatal("invalid steady measurement input was accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid measurement inputs reached inference")
	}
}

func TestPublicSteadyStateCancellationRemainsIncomplete(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	summary, err := MeasurePublicSteadyState(ctx, newPublicClient(t, server.URL, 1024), steadyFixture())
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 || summary.Successes != 0 || summary.Errors != 1 {
		t.Fatal("cancelled workload continued inference or returned apparently complete measurements")
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := MeasurePublicSteadyState(cancelled, newPublicClient(t, server.URL, 1024), steadyFixture()); !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatal("already cancelled workload submitted a request")
	}
}
