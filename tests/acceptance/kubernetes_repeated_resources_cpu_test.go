package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestP08RepeatedOwnerActualBoundary(t *testing.T) {
	for _, change := range []string{"", "container", "health", "process"} {
		t.Run(change, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "context", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
			pods := 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				cmd := strings.Join(args, " ")
				switch {
				case strings.Contains(cmd, "get namespace"):
					return []byte("ns"), nil
				case strings.Contains(cmd, "get pod"):
					pods++
					role, uid := "api", "a"
					if strings.Contains(cmd, "pod backend-0") {
						role, uid = "backend", "b"
					}
					id := "original"
					if change == "container" && pods > 2 {
						id = "replacement"
					}
					return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":%q}},"status":{"containerStatuses":[{"name":%q,"containerID":%q,"restartCount":0,"state":{"running":{}}}]}}`, uid, role, role, id)), nil
				case strings.Contains(cmd, "p08-identity"):
					if strings.HasSuffix(cmd, "backend") {
						if change == "process" {
							return []byte("10 0 0"), nil
						}
						return []byte("10 42 20"), nil
					}
					return []byte("9 0 0"), nil
				case strings.Contains(cmd, "curl"):
					generation := "generation"
					if change == "health" {
						generation = ""
					}
					return []byte(fmt.Sprintf(`{"status":"ready","model":"base","protocol_version":"1","configuration_fingerprint":"fingerprint","process_generation":%q,"cuda_ready":true}`+"\n%d", generation, http.StatusOK)), nil
				default:
					return nil, errors.New("unexpected request")
				}
			}
			identity, err := p08RepeatedOwner(t.Context(), target, "base", "8081")
			if (err != nil) != (change != "") {
				t.Fatalf("unexpected identity result: %v", err)
			}
			if change == "" && (identity.nativePID != 42 || identity.nativeStart != 20 || identity.generation != "generation") {
				t.Fatal("actual process identity not preserved")
			}
		})
	}
}

func TestP08RepeatedResourceMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name, failure string
		wantErr       bool
	}{
		{"complete", "", false}, {"unknown RSS", "unknown", true}, {"retained scratch", "scratch", true}, {"unknown native descendants", "unknown-descendant", true}, {"unknown deleted scratch", "unknown-deleted", true}, {"deleted-open scratch", "deleted-open", true}, {"owner replacement", "owner", true}, {"observation failure", "observe", true}, {"public failure", "request", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if tc.failure == "request" && n == 3 {
					http.Error(w, "private", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"text":"My fellow Americans ask what your country can do"}`))
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ownerCalls, observations := 0, 0
			owner := func(context.Context) (p08RepeatedIdentity, error) {
				ownerCalls++
				identity := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "api"}, {ID: "backend"}}, apiStart: 1, backendStart: 2, nativePID: 42, nativeStart: 5, generation: "g", fingerprint: "f"}
				if tc.failure == "owner" && ownerCalls > 4 {
					identity.nativeStart++
				}
				return identity, nil
			}
			observe := func(context.Context) (p08ResourceEvidence, error) {
				observations++
				if tc.failure == "observe" {
					return p08ResourceEvidence{}, errors.New("private detail")
				}
				zero, one, rss := uint64(0), uint64(1), uint64(100)
				api := DockerResourceSnapshot{RSSBytes: &rss, FDCount: &one, ScratchFileCount: &zero, ScratchBytes: &zero, OwnershipLockFiles: &zero}
				backend := DockerResourceSnapshot{RSSBytes: &rss, FDCount: &one}
				if tc.failure == "unknown" {
					api.RSSBytes = nil
				}
				if tc.failure == "scratch" {
					api.ScratchFileCount = &one
				}
				evidence := p08ResourceEvidence{ObservedAt: time.Now(), API: api, Backend: backend, NativeDescendantRSSBytes: &zero, APIDeletedScratchFDs: &zero, APIDeletedScratchBytes: &zero}
				if tc.failure == "unknown-descendant" {
					evidence.NativeDescendantRSSBytes = nil
				}
				if tc.failure == "unknown-deleted" {
					evidence.APIDeletedScratchFDs = nil
				}
				if tc.failure == "deleted-open" {
					evidence.APIDeletedScratchFDs = &one
				}
				return evidence, nil
			}
			evidence, err := measureP08RepeatedResources(t.Context(), client, []byte("fixture"), "base", 30, owner, observe)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantErr && (calls.Load() != 30 || observations != 4 || evidence.Requests != 30 || len(evidence.Snapshots) != 4 || evidence.Scope != "partial_not_memory_stability") {
				t.Fatalf("unexpected result: %+v", evidence)
			}
			if tc.failure == "request" && calls.Load() != 3 {
				t.Fatal("failed request was retried")
			}
		})
	}
}
