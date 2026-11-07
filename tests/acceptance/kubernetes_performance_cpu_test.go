package acceptance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestKubernetesPublicPerformanceWorkload(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"My fellow Americans ask what your country can do"}`))
	}))
	defer server.Close()
	client, err := NewPublicClient(server.URL, time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	evidence, err := measureKubernetesPublicPerformance(t.Context(), client, []byte("fixture"), "base", 30)
	if err != nil || !evidence.Complete || calls.Load() != 31 || evidence.Summary.Public == nil || evidence.Summary.Public.Median < time.Millisecond || evidence.Summary.Successes != 30 {
		t.Fatalf("unexpected measured result: %+v, %v", evidence, err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeKubernetesPublicPerformance(dir, evidence); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "p08-public-steady-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "fellow Americans") || strings.Contains(string(content), server.URL) {
		t.Fatal("private response content leaked into evidence")
	}
	var saved kubernetesPublicPerformanceEvidence
	if json.Unmarshal(content, &saved) != nil || saved.Summary.Successes != 30 || !saved.Complete {
		t.Fatal("evidence did not preserve measurement")
	}
	if writeKubernetesPublicPerformance(dir, evidence) == nil {
		t.Fatal("evidence unexpectedly overwritten")
	}
}

func TestKubernetesPublicPerformanceFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		speech     bool
		failSample bool
	}{
		{name: "speech preflight"}, {name: "steady failure", speech: true, failSample: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if tc.failSample && call == 2 {
					http.Error(w, "private error", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.speech {
					_, _ = w.Write([]byte(`{"text":"My fellow Americans ask what your country can do"}`))
				} else {
					_, _ = w.Write([]byte(`{"text":"unrelated"}`))
				}
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			evidence, err := measureKubernetesPublicPerformance(t.Context(), client, []byte("fixture"), "base", 30)
			if err == nil || evidence.Complete {
				t.Fatal("failed workload accepted")
			}
			if tc.failSample && (evidence.Summary.Errors != 1 || calls.Load() != 31) {
				t.Fatal("steady errors not counted without retry")
			}
			if !tc.speech && calls.Load() != 1 {
				t.Fatal("measurement proceeded after failed preflight")
			}
		})
	}
}
