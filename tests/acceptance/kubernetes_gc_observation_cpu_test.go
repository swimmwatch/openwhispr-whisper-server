package acceptance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestP08GCProfileProbeChecksActualEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"profile", "GODEBUG=gctrace=1,scavtrace=1\x00OTHER=private\x00", true},
		{"missing", "OTHER=private\x00", false},
		{"incorrect", "GODEBUG=gctrace=1\x00", false},
		{"gogc", "GODEBUG=gctrace=1,scavtrace=1\x00GOGC=\x00", false},
		{"limit", "GODEBUG=gctrace=1,scavtrace=1\x00GOMEMLIMIT=1GiB\x00", false},
		{"duplicate", "GODEBUG=gctrace=1,scavtrace=1\x00GODEBUG=gctrace=1,scavtrace=1\x00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "environ")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			script := strings.Replace(p08GCProfileProbe, "/proc/1/environ", `"$1"`, 1)
			output, err := exec.CommandContext(t.Context(), "sh", "-ec", script, "probe", path).Output()
			if (err == nil) != tc.valid || (tc.valid && string(output) != "verified\n") || strings.Contains(string(output), "private") {
				t.Fatal("profile validation or output privacy failed")
			}
		})
	}
}

func TestP08GCDiagnosticRejectsUnavailableProfileAndReplacement(t *testing.T) {
	for _, mode := range []string{"complete", "profile", "logs", "replacement", "missing-cycle"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/health/live" {
					t.Error("unexpected workload")
				}
				calls++
				_, _ = w.Write([]byte(`{"status":"live"}`))
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "private", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				s := strings.Join(args, " ")
				if strings.Contains(s, " exec ") {
					if mode == "profile" {
						return nil, errors.New("private error")
					}
					return []byte("verified\n"), nil
				}
				if strings.Contains(s, " logs ") {
					if mode == "logs" {
						return nil, errors.New("private log error")
					}
					if mode == "missing-cycle" {
						return nil, nil
					}
					return []byte(time.Now().UTC().Format(time.RFC3339Nano) + " gc 1 @1s 1%: 1+1+1 ms clock, 1+1/1/1+1 ms cpu, 1->1->1 MB, 2 MB goal, 0 MB stacks, 0 MB globals, 1 P\n"), nil
				}
				return nil, errors.New("unexpected call")
			}
			identity := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "a"}, {ID: "b"}}, apiStart: 1, backendStart: 2, nativePID: 3, nativeStart: 4, generation: "g", fingerprint: "f"}
			owners := 0
			owner := func(context.Context) (p08RepeatedIdentity, error) {
				owners++
				if mode == "replacement" && owners > 1 {
					return p08RepeatedIdentity{}, nil
				}
				return identity, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			evidence, err := observeP08GCPartial(ctx, target, client, "base", "8080", owner)
			if mode == "profile" || mode == "logs" || mode == "replacement" {
				if err == nil {
					t.Fatal("unverified observation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != residentHealthObservationBatch || evidence.Qualification != "not_established" {
				t.Fatal("partial diagnostic misreported")
			}
			if mode == "missing-cycle" {
				if evidence.API.GC != nil || evidence.Backend.GC != nil {
					t.Fatal("unknown cycles became measured zeros")
				}
			} else if evidence.API.GC == nil || evidence.Backend.GC == nil {
				t.Fatal("numeric cycles missing")
			}
		})
	}
}
