package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestP08AnchoredDiagnosticUsesBothRolesAndRejectsReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			var mu sync.Mutex
			var apiIDs, backendIDs []string
			var apiHealth, backendHealth int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health/ready" {
					id := r.Header.Get("X-Request-ID")
					mu.Lock()
					apiIDs = append(apiIDs, id)
					mu.Unlock()
					w.Header().Set("X-Request-ID", id)
				} else if r.URL.Path == "/health/live" {
					mu.Lock()
					apiHealth++
					mu.Unlock()
				} else {
					t.Error("unexpected request")
				}
				_, _ = w.Write([]byte(`{"status":"live"}`))
			}))
			defer srv.Close()
			client, err := NewPublicClient(srv.URL, time.Second, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "ctx", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				for _, arg := range args {
					if arg == p08GCBackendHealthScript {
						n, err := strconv.Atoi(args[len(args)-2])
						if err != nil || n < 1 || n > p08GCBackendHealthChunk {
							t.Fatal("invalid backend chunk")
						}
						backendHealth += n
						return []byte(args[len(args)-2] + " ok\n"), nil
					}
				}
				if strings.Contains(joined, " -- sh -ec ") {
					return []byte("verified\n"), nil
				}
				if strings.Contains(joined, " -- curl ") {
					for _, a := range args {
						if strings.HasPrefix(a, "X-Request-ID: ") {
							backendIDs = append(backendIDs, strings.TrimPrefix(a, "X-Request-ID: "))
						}
					}
					return []byte("200"), nil
				}
				if strings.Contains(joined, " logs ") {
					role, route, ids := "backend", "/health", backendIDs
					if strings.Contains(joined, "logs api-0") {
						mu.Lock()
						ids = append([]string(nil), apiIDs...)
						mu.Unlock()
						role, route = "api", "/health/ready"
					}
					if len(ids) != 2 {
						return nil, errors.New("anchors missing")
					}
					return []byte(fmt.Sprintf("2026-01-01T00:00:01Z {\"event\":\"request_completed\",\"component\":%q,\"request_id\":%q,\"method\":\"GET\",\"route\":%q,\"status\":200}\n2026-01-01T00:00:02Z {\"event\":\"request_completed\",\"component\":%q,\"request_id\":%q,\"method\":\"GET\",\"route\":%q,\"status\":200}\n", role, ids[0], route, role, ids[1], route)), nil
				}
				return nil, errors.New("unexpected command")
			}
			initial := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "a"}, {ID: "b"}}, apiStart: 1, backendStart: 2, nativePID: 3, nativeStart: 4, generation: "g", fingerprint: "f"}
			count := 0
			owner := func(context.Context) (p08RepeatedIdentity, error) {
				count++
				if replace && count > 1 {
					return p08RepeatedIdentity{}, nil
				}
				return initial, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := observeP08AnchoredGCBatch(ctx, target, client, "8080", owner, residentHealthObservationMaxCalls)
			if replace {
				var diagnostic *p08GCDiagnosticError
				if !errors.Is(err, errResidentSupervisorHeapUnknown) || !errors.As(err, &diagnostic) || diagnostic.Stage != "window_owner_after" || diagnostic.Reason != "identity_changed" {
					t.Fatalf("replacement not classified safely: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			observedAPIHealth := apiHealth
			mu.Unlock()
			if result.Qualification != "not_established" || result.Health.Calls != residentHealthObservationMaxCalls/2 || result.BackendHealth.Calls != residentHealthObservationMaxCalls/2 || observedAPIHealth != result.Health.Calls || backendHealth != result.BackendHealth.Calls || result.API.GC != nil || result.Backend.GC != nil || !result.API.End.After(result.API.Start) || !result.Backend.End.After(result.Backend.Start) {
				t.Fatal("incomplete or misleading anchored evidence")
			}
		})
	}
}

func TestP08AnchoredWindowProfileFailureIsSafe(t *testing.T) {
	target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "ctx", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
	target.run = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("private command output") }
	owner := func(context.Context) (p08RepeatedIdentity, error) {
		return p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "a"}, {ID: "b"}}, apiStart: 1, backendStart: 2, nativePID: 3, nativeStart: 4, generation: "g", fingerprint: "f"}, nil
	}
	client := &PublicClient{}
	_, err := observeP08AnchoredGCBatch(t.Context(), target, client, "8080", owner, 0)
	var diagnostic *p08GCDiagnosticError
	if !errors.Is(err, errResidentSupervisorHeapUnknown) || !errors.As(err, &diagnostic) || diagnostic.Stage != "window_profile" || diagnostic.Reason != "profile_unverified" || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe or incorrect profile failure: %v", err)
	}
}

func TestP08AnchoredGCReportsSafeReasons(t *testing.T) {
	for _, tc := range []struct {
		name, logs, reason string
	}{
		{"empty", "", "empty_logs"},
		{"too large", strings.Repeat("x", p08GCLogLimit), "logs_too_large"},
		{"truncated", "private", "truncated_logs"},
		{"missing markers", "2026-01-01T00:00:00Z private\n", "missing_marker"},
		{"bad timestamp", "private private\n", "invalid_timestamp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p08AnchoredGC([]byte(tc.logs), "api", "start", "end")
			var diagnostic *p08GCDiagnosticError
			if !errors.Is(err, errResidentSupervisorHeapUnknown) || !errors.As(err, &diagnostic) || diagnostic.Stage != "window_parse" || diagnostic.Reason != tc.reason || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe or incorrect diagnostic: %v", err)
			}
		})
	}
	if got := (&p08GCDiagnosticError{Stage: "private", Reason: "private"}).Error(); strings.Contains(got, "private") {
		t.Fatal("unrecognized diagnostics exposed")
	}
}

func TestP08AnchoredGCDetectsLostAndAmbiguousBoundaries(t *testing.T) {
	marker := func(second int, id, role, route string) string {
		return fmt.Sprintf("2026-01-01T00:00:%02dZ {\"event\":\"request_completed\",\"component\":%q,\"request_id\":%q,\"method\":\"GET\",\"route\":%q,\"status\":200}\n", second, role, id, route)
	}
	cycle := func(second, number int) string {
		return fmt.Sprintf("2026-01-01T00:00:%02dZ gc %d @%ds 1%%: 1+1+1 ms clock, 1+1/1/1+1 ms cpu, 1->1->1 MB, 2 MB goal, 0 MB stacks, 0 MB globals, 1 P\n", second, number, number)
	}
	start, end := marker(1, "start", "api", "/health/ready"), marker(4, "end", "api", "/health/ready")
	for _, tc := range []struct {
		name, logs string
		valid      bool
		count      int
	}{
		{"complete", start + cycle(2, 1) + cycle(3, 2) + end + cycle(5, 3), true, 2},
		{"rotated start", cycle(2, 1) + end, false, 0},
		{"missing end", start + cycle(2, 1), false, 0},
		{"duplicate start", start + marker(2, "start", "api", "/health/ready") + end, false, 0},
		{"duplicate end", start + end + marker(5, "end", "api", "/health/ready"), false, 0},
		{"wrong role", marker(1, "start", "backend", "/health") + end, false, 0},
		{"interleaved cycles", start + cycle(2, 1) + cycle(3, 1) + end, false, 0},
		{"timestamp reversal", start + cycle(3, 1) + cycle(2, 2) + end, false, 0},
		{"truncated line", start + end + "truncated", false, 0},
		{"oversized", start + strings.Repeat("x", p08GCLogLimit) + end, false, 0},
		{"no cycles", start + end, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := p08AnchoredGC([]byte(tc.logs), "api", "start", "end")
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected result: %v", err)
			}
			if tc.valid {
				if tc.count == 0 {
					if result.GC != nil {
						t.Fatal("unknown cycles became a measured zero")
					}
				} else if result.GC == nil || len(result.GC.Records) != tc.count {
					t.Fatal("wrong selected cycles")
				}
			}
		})
	}
}
