package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestKubernetesCacheObservationRealFileHash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "model")
	data := []byte("private fixture data")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	s := kubernetesCacheSelection{Kubeconfig: "/private/config", Context: "ctx", Namespace: "ns", NamespaceUID: "ns", Release: "release", BackendPod: "backend", BackendUID: "pod", Container: "backend", Claim: "claim", ClaimUID: "claim", ArtifactPath: file, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	s.run = func(ctx context.Context, args ...string) ([]byte, error) {
		v := strings.Join(args, " ")
		switch {
		case strings.Contains(v, "get namespace"):
			return []byte("ns"), nil
		case strings.Contains(v, "get pvc"):
			return []byte(`{"metadata":{"uid":"claim"},"status":{"phase":"Bound"}}`), nil
		case strings.Contains(v, "get pod"):
			return []byte(`{"metadata":{"uid":"pod","labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":"backend"}},"spec":{"volumes":[{"name":"v","persistentVolumeClaim":{"claimName":"claim"}}],"containers":[{"name":"backend","volumeMounts":[{"name":"v","mountPath":` + strconv.Quote(dir) + `}]}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`), nil
		case strings.Contains(v, "exec backend"):
			for i, arg := range args {
				if arg == "sh" && i+4 < len(args) {
					return exec.CommandContext(ctx, args[i], args[i+1:]...).Output()
				}
			}
		}
		return nil, errors.New("unexpected command")
	}
	if err := observeKubernetesCache(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("modified fixture data"), 0600); err != nil {
		t.Fatal(err)
	}
	if observeKubernetesCache(t.Context(), s) == nil {
		t.Fatal("changed file was accepted")
	}
}

func TestKubernetesCacheObservationRequiresBoundIdentityAndBytes(t *testing.T) {
	hash := strings.Repeat("a", 64)
	pod := `{"metadata":{"uid":"pod-uid","labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":"backend"}},"spec":{"volumes":[{"name":"cache","persistentVolumeClaim":{"claimName":"claim"}}],"containers":[{"name":"backend","volumeMounts":[{"name":"cache","mountPath":"/cache"}]}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`
	claim := `{"metadata":{"uid":"claim-uid"},"status":{"phase":"Bound"}}`
	for _, tc := range []struct {
		name, pod, claim, output string
		fail                     bool
	}{
		{"valid", pod, claim, hash + "\n12\n", false},
		{"different hash", pod, claim, strings.Repeat("b", 64) + "\n12\n", true},
		{"different size", pod, claim, hash + "\n13\n", true},
		{"claim replaced", pod, strings.Replace(claim, "claim-uid", "other", 1), hash + "\n12\n", true},
		{"claim pending", pod, strings.Replace(claim, "Bound", "Pending", 1), hash + "\n12\n", true},
		{"pod replaced", strings.Replace(pod, "pod-uid", "other", 1), claim, hash + "\n12\n", true},
		{"pod unready", strings.Replace(pod, `"True"`, `"False"`, 1), claim, hash + "\n12\n", true},
		{"wrong release", strings.Replace(pod, `"release"`, `"other"`, 1), claim, hash + "\n12\n", true},
		{"wrong volume", strings.Replace(pod, `"claimName":"claim"`, `"claimName":"other"`, 1), claim, hash + "\n12\n", true},
		{"subpath", strings.Replace(pod, `"mountPath":"/cache"`, `"mountPath":"/cache","subPath":"other"`, 1), claim, hash + "\n12\n", true},
		{"malformed output", pod, claim, hash + "\n12\nextra", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := kubernetesCacheSelection{Kubeconfig: "/private/config", Context: "ctx", Namespace: "ns", NamespaceUID: "ns-uid", Release: "release", BackendPod: "backend-0", BackendUID: "pod-uid", Container: "backend", Claim: "claim", ClaimUID: "claim-uid", ArtifactPath: "/cache/artifacts/file", SHA256: hash, Size: 12}
			s.run = func(_ context.Context, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "--kubeconfig /private/config --context ctx --namespace ns") {
					return nil, errors.New("missing selection")
				}
				switch {
				case strings.Contains(joined, "get namespace"):
					return []byte("ns-uid"), nil
				case strings.Contains(joined, "get pvc"):
					return []byte(tc.claim), nil
				case strings.Contains(joined, "get pod"):
					return []byte(tc.pod), nil
				case strings.Contains(joined, "exec backend-0"):
					return []byte(tc.output), nil
				}
				return nil, errors.New("unexpected command")
			}
			err := observeKubernetesCache(t.Context(), s)
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected observation outcome: %v", err)
			}
		})
	}
}

func TestKubernetesCacheObservationFailsOnMidReadReplacement(t *testing.T) {
	hash := strings.Repeat("a", 64)
	s := kubernetesCacheSelection{Kubeconfig: "/private/config", Context: "ctx", Namespace: "ns", NamespaceUID: "ns-uid", Release: "release", BackendPod: "backend", BackendUID: "pod", Container: "backend", Claim: "claim", ClaimUID: "claim", ArtifactPath: "/cache/file", SHA256: hash, Size: 12}
	reads := 0
	s.run = func(_ context.Context, args ...string) ([]byte, error) {
		v := strings.Join(args, " ")
		switch {
		case strings.Contains(v, "get namespace"):
			return []byte("ns-uid"), nil
		case strings.Contains(v, "get pvc"):
			reads++
			if reads > 1 {
				return []byte(`{"metadata":{"uid":"replacement"},"status":{"phase":"Bound"}}`), nil
			}
			return []byte(`{"metadata":{"uid":"claim"},"status":{"phase":"Bound"}}`), nil
		case strings.Contains(v, "get pod"):
			return []byte(`{"metadata":{"uid":"pod","labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":"backend"}},"spec":{"volumes":[{"name":"v","persistentVolumeClaim":{"claimName":"claim"}}],"containers":[{"name":"backend","volumeMounts":[{"name":"v","mountPath":"/cache"}]}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`), nil
		case strings.Contains(v, "exec backend"):
			return []byte(hash + "\n12\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	if observeKubernetesCache(t.Context(), s) == nil {
		t.Fatal("claim replacement during observation was accepted")
	}
	s.NamespaceUID = ""
	if observeKubernetesCache(t.Context(), s) == nil {
		t.Fatal("missing namespace identity was accepted")
	}
}
