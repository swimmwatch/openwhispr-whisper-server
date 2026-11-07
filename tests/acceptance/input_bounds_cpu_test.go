package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestInputBoundsRejectsWrongPublishedRuntimeBeforeWork(t *testing.T) {
	refs := [2]string{"registry/api@sha256:" + strings.Repeat("a", 64), "registry/backend@sha256:" + strings.Repeat("b", 64)}
	for _, mode := range []string{"valid", "foreign-runtime", "foreign-pod", "unknown-platform", "changed-after"} {
		t.Run(mode, func(t *testing.T) {
			owner := p08HeapOwner{Containers: [2]p08HeapContainer{{Image: refs[0]}, {Image: refs[1]}}}
			images := refs
			if mode == "foreign-runtime" {
				owner.Containers[0].Image = "foreign-api"
				images[0] = "foreign-api"
			}
			if mode == "unknown-platform" {
				owner.Containers[0].Image = "sha256:" + strings.Repeat("c", 64)
				images[0] = owner.Containers[0].Image
			}
			calls, observations := 0, 0
			err := p08InputCandidateWork(t.Context(), refs, images, func(context.Context) ([2]string, error) {
				if mode == "foreign-pod" {
					return [2]string{"foreign", refs[1]}, nil
				}
				return refs, nil
			}, func(context.Context) (p08HeapOwner, error) {
				observations++
				if mode == "changed-after" && observations == 2 {
					return p08HeapOwner{}, errors.New("unavailable")
				}
				return owner, nil
			}, func() bool { calls++; return true })
			if (err == nil) != (mode == "valid") {
				t.Fatal("incorrect candidate binding result")
			}
			if (mode == "foreign-runtime" || mode == "foreign-pod" || mode == "unknown-platform") && calls != 0 {
				t.Fatal("wrong image admitted workload")
			}
			if mode == "changed-after" && calls != 1 {
				t.Fatal("workload did not execute before final identity failure")
			}
		})
	}
}

func TestInputBoundsKubernetesPrivacyFailsClosed(t *testing.T) {
	for _, fault := range []string{"none", "root-cache", "wrong-cache", "dot-cache", "missing-cache", "duplicate-cache", "subpath", "nested-mount", "log-error", "log-marker", "log-full", "log-partial", "file-error", "file-marker", "identity"} {
		t.Run(fault, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "selected", Namespace: "selected", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "api-id", APIContainer: "api", BackendPod: "backend-0", BackendUID: "backend-id", BackendContainer: "backend"}
			expected := [2]p08ContainerIdentity{{ID: "api-container"}, {ID: "backend-container"}}
			reads := 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				command := strings.Join(args, " ")
				if strings.Contains(command, "get namespace") {
					return []byte("ns"), nil
				}
				if strings.Contains(command, "get pod") {
					role, uid, cid := "api", "api-id", "api-container"
					if strings.Contains(command, "backend-0") {
						role, uid, cid = "backend", "backend-id", "backend-container"
					}
					if fault == "identity" && reads >= 4 && role == "api" {
						cid = "changed"
					}
					mounts := []map[string]string{{"name": "cache", "mountPath": "/cache"}}
					if fault == "missing-cache" {
						mounts = nil
					}
					if fault == "duplicate-cache" {
						mounts = append(mounts, mounts[0])
					}
					if fault == "subpath" {
						mounts[0]["subPath"] = "nested"
					}
					if fault == "nested-mount" {
						mounts = append(mounts, map[string]string{"name": "other", "mountPath": "/cache/other"})
					}
					body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid, "labels": map[string]string{"app.kubernetes.io/instance": "release", "app.kubernetes.io/component": role}}, "spec": map[string]any{"containers": []map[string]any{{"name": role, "volumeMounts": mounts}}}, "status": map[string]any{"containerStatuses": []map[string]any{{"name": role, "containerID": cid, "restartCount": 0, "state": map[string]any{"running": map[string]any{}}}}}})
					return body, nil
				}
				reads++
				if strings.Contains(command, "logs ") {
					if !strings.Contains(command, "--limit-bytes=1048577") || fault == "log-error" {
						return nil, errors.New("private error")
					}
					if fault == "log-marker" {
						return []byte("PRIVATE-MARKER\n"), nil
					}
					if fault == "log-full" {
						return []byte(strings.Repeat("x", 1<<20)), nil
					}
					if fault == "log-partial" {
						return []byte("partial"), nil
					}
					return []byte("clean log\n"), nil
				}
				if fault == "file-error" {
					return nil, errors.New("private error")
				}
				if fault == "file-marker" {
					return []byte("leak"), nil
				}
				return []byte("clean"), nil
			}
			cache := "/cache"
			if fault == "root-cache" {
				cache = "/"
			}
			if fault == "wrong-cache" {
				cache = "/wrong"
			}
			if fault == "dot-cache" {
				cache = "/cache/.."
			}
			err := p08InputPrivacy(t.Context(), target, expected, cache, "PRIVATE-MARKER")
			if (err == nil) != (fault == "none") {
				t.Fatalf("incorrect privacy verdict: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE-MARKER") {
				t.Fatal("private data appeared in error")
			}
		})
	}
}

func TestInputBoundsUsesSelectedModel(t *testing.T) {
	if os.Getenv("P08_BOUNDS_MODEL_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInputBoundsUsesSelectedModel$/^exact_file_cap_and_one_byte_excess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "P08_BOUNDS_MODEL_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shared selected-model workload failed: %s", output)
		}
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("model") != "tiny" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		w.Header().Set("Content-Type", "application/json")
		if len(body) > 64 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "payload exceeds limit", "type": "invalid_request_error", "param": "file", "code": "payload_too_large"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "sample"})
	}))
	defer server.Close()
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	settings.Models.Served = "tiny"
	settings.Limits.AudioBytes = 64
	client, err := NewPublicClient(server.URL, time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	runInputBoundsAndIsolation(t, &acceptanceEnvironment{ctx: context.Background(), client: client, settings: settings, audio: make([]byte, 16)})
}
