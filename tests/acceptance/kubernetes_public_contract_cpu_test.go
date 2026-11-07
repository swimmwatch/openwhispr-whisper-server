package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestP08PublicCandidateBinding(t *testing.T) {
	api := "ghcr.io/example/api@sha256:" + strings.Repeat("1", 64)
	backend := "ghcr.io/example/backend@sha256:" + strings.Repeat("2", 64)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, tc := range []struct {
		name       string
		references [2]string
		missing    bool
		want       bool
	}{
		{"selected", [2]string{api, backend}, false, true},
		{"wrong-api", [2]string{backend, backend}, false, false},
		{"missing-backend", [2]string{api, ""}, false, false},
		{"unavailable", [2]string{api, backend}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := 0
			err := p08PublicCandidateWork(t.Context(), [2]string{api, backend}, func(context.Context) ([2]string, error) {
				observed++
				if tc.missing {
					return [2]string{}, errors.New("unavailable")
				}
				return tc.references, nil
			}, func() error {
				resp, err := http.Get(server.URL)
				if err != nil {
					return err
				}
				return resp.Body.Close()
			})
			if (err == nil) != tc.want || observed == 0 {
				t.Fatal("candidate guard failed")
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatal("unbound candidate executed HTTP workload")
	}
}

func TestP08PublicCandidateRejectsLostFinalEvidence(t *testing.T) {
	for _, missing := range []bool{false, true} {
		calls := 0
		expected := [2]string{"api", "backend"}
		err := p08PublicCandidateWork(t.Context(), expected, func(context.Context) ([2]string, error) {
			calls++
			if calls == 2 {
				if missing {
					return [2]string{}, errors.New("unavailable")
				}
				return [2]string{"changed", "backend"}, nil
			}
			return expected, nil
		}, func() error { return nil })
		if err == nil || calls != 2 {
			t.Fatal("lost final candidate evidence accepted")
		}
	}
	if p08PublicCandidateWork(t.Context(), [2]string{}, nil, nil) == nil {
		t.Fatal("missing candidate and observations accepted")
	}
}

func TestP08PublicCandidateRuntimeBinding(t *testing.T) {
	api := "ghcr.io/example/api@sha256:" + strings.Repeat("1", 64)
	backend := "ghcr.io/example/backend@sha256:" + strings.Repeat("2", 64)
	refs := [2]string{api, backend}
	for _, tc := range []struct {
		name   string
		images [2]string
		valid  bool
	}{
		{"exact", refs, true},
		{"docker-pullable", [2]string{"docker-pullable://" + api, "docker-pullable://" + backend}, true},
		{"containerd", [2]string{"containerd://" + api, "containerd://" + backend}, true},
		{"digest-only", [2]string{strings.Split(api, "@")[1], strings.Split(backend, "@")[1]}, true},
		{"wrong-api", [2]string{backend, backend}, false},
		{"wrong-backend-digest", [2]string{api, "sha256:" + strings.Repeat("3", 64)}, false},
		{"missing", [2]string{api, ""}, false},
		{"foreign-reference", [2]string{"ghcr.io/other/api@" + strings.Split(api, "@")[1], backend}, false},
		{"invalid-prefix", [2]string{"unknown://" + api, backend}, false},
		{"stacked-prefix", [2]string{"containerd://docker-pullable://" + api, backend}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := p08HeapOwner{Containers: [2]p08HeapContainer{{Image: tc.images[0]}, {Image: tc.images[1]}}}
			if (p08PublicRuntimeMatchesCandidate(refs, owner) == nil) != tc.valid {
				t.Fatal("runtime candidate identity mismatch")
			}
		})
	}
}

func TestP08PublicCandidateRuntimeBracketHTTP(t *testing.T) {
	api := "ghcr.io/example/api@sha256:" + strings.Repeat("1", 64)
	backend := "ghcr.io/example/backend@sha256:" + strings.Repeat("2", 64)
	refs := [2]string{api, backend}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, tc := range []struct {
		name          string
		before, after [2]string
		wantOK        bool
		wantRequests  int32
	}{
		{"selected", refs, refs, true, 1},
		{"initial-mismatch", [2]string{backend, backend}, refs, false, 0},
		{"changed-after-work", refs, [2]string{api, api}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests.Store(0)
			observations := 0
			observe := func(context.Context) (p08HeapOwner, error) {
				observations++
				images := tc.before
				if observations > 1 {
					images = tc.after
				}
				owner := p08HeapOwner{Containers: [2]p08HeapContainer{{Image: images[0]}, {Image: images[1]}}}
				if err := p08PublicRuntimeMatchesCandidate(refs, owner); err != nil {
					return p08HeapOwner{}, err
				}
				return owner, nil
			}
			err := runP08BoundHTTPSlice(t.Context(), refs, observe, func() bool {
				response, e := http.Get(server.URL)
				if e != nil {
					return false
				}
				return response.Body.Close() == nil
			})
			if (err == nil) != tc.wantOK || requests.Load() != tc.wantRequests {
				t.Fatal("runtime identity did not fence actual HTTP work")
			}
		})
	}
}

func TestP08PublicCandidatePodImages(t *testing.T) {
	target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "ctx", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
	target.run = func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--kubeconfig /private/kubeconfig --context ctx --namespace test") {
			t.Error("ambient Kubernetes target selected")
		}
		name, uid, image := "api", "a", "api-reference"
		if strings.Contains(joined, "backend-0") {
			name, uid, image = "backend", "b", "backend-reference"
		}
		return json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid}, "spec": map[string]any{"containers": []any{map[string]string{"name": name, "image": image}}}})
	}
	got, err := p08PublicPodImages(t.Context(), target)
	if err != nil || got != [2]string{"api-reference", "backend-reference"} {
		t.Fatal("selected Pod references unavailable")
	}
	target.APIUID = "wrong"
	if _, err = p08PublicPodImages(t.Context(), target); err == nil {
		t.Fatal("changed Pod UID accepted")
	}
}
