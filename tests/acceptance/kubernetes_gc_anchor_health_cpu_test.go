package acceptance

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestP08BackendHealthUsesLoopbackAndFailsClosed(t *testing.T) {
	var calls atomic.Int64
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			t.Error("unexpected backend request")
		}
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "ctx", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
	target.run = func(ctx context.Context, args ...string) ([]byte, error) {
		for i, arg := range args {
			if arg == p08GCBackendHealthScript {
				return exec.CommandContext(ctx, "sh", append([]string{"-ec", arg}, args[i+1:]...)...).Output()
			}
		}
		return nil, errors.New("unexpected command")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var facts p08BackendHealthObservation
	if err := runP08BackendHealthObservation(ctx, target, port, &facts, 3); err != nil || facts.Calls != 3 || facts.Errors != 0 || facts.CallsUnknown || calls.Load() != 3 {
		t.Fatalf("backend health failed: %v", err)
	}
	fail.Store(true)
	if err := runP08BackendHealthObservation(ctx, target, port, &facts, 3); !errors.Is(err, errResidentSupervisorHeapUnknown) || facts.Calls != 4 || facts.Errors != 1 || facts.CallsUnknown || calls.Load() != 4 {
		t.Fatalf("failed status not reported: %v", err)
	}
}

func TestP08HealthLimitsAndDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := observeP08AnchoredGCBatch(ctx, p08ResourceTarget{}, &PublicClient{}, "8080", func(context.Context) (p08RepeatedIdentity, error) {
		t.Fatal("over-limit called owner")
		return p08RepeatedIdentity{}, nil
	}, residentHealthObservationMaxCalls+1); !errors.Is(err, errResidentSupervisorHeapUnknown) {
		t.Fatal("global budget exceeded")
	}
	target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "ctx", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
	target.run = func(ctx context.Context, _ ...string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }
	var facts p08BackendHealthObservation
	short, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer stop()
	if err := runP08BackendHealthObservation(short, target, "8080", &facts, 1); !errors.Is(err, errResidentSupervisorHeapUnknown) || !facts.CallsUnknown || facts.Errors != 1 || facts.Calls != 0 {
		t.Fatal("timeout did not preserve unknown call count")
	}
	if err := runP08BackendHealthObservation(ctx, target, "8080", &facts, 1); !errors.Is(err, errResidentSupervisorHeapUnknown) {
		t.Fatal("unknown call count permitted retry")
	}
	var invalid p08BackendHealthObservation
	if err := runP08BackendHealthObservation(ctx, target, "8080;private", &invalid, 1); !errors.Is(err, errResidentSupervisorHeapUnknown) || invalid.Calls != 0 {
		t.Fatal("invalid port accepted")
	}
}
