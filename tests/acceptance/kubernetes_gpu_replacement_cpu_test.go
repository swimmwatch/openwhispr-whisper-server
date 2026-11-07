package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

const oldTestUID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
const newTestUID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
const otherTestUID = "cccccccc-cccc-cccc-cccc-cccccccccccc"

func fakeReplacementObserver(mode string) kubernetesGPUObserver {
	index := -1
	pod := func(uid string) string {
		return fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":"backend"}},"spec":{"nodeName":"node"}}`, uid)
	}
	o := kubernetesGPUObserver{Kubeconfig: "explicit", Context: "explicit", Namespace: "ns", NamespaceUID: "namespace-uid", Release: "release", DriverNamespace: "driver-ns", DriverPod: "driver", DriverUID: "driver-uid", GPUUUID: "GPU-test", OldPodUID: oldTestUID}
	o.run = func(ctx context.Context, args ...string) ([]byte, error) {
		s := strings.Join(args, " ")
		switch {
		case strings.Contains(s, "get namespace"):
			return []byte("namespace-uid"), nil
		case strings.Contains(s, "get pod driver"):
			return []byte(`{"metadata":{"uid":"driver-uid"},"spec":{"nodeName":"node","hostPID":true}}`), nil
		case strings.Contains(s, "get pods"):
			index++
			if mode == "slow" && index == 1 {
				time.Sleep(10 * time.Millisecond)
			}
			items := ""
			if index == 0 {
				items = pod(oldTestUID)
			}
			if index >= 2 || (mode == "no-retirement" || mode == "new-pod-before-gpu" || mode == "new-pod-old-gpu" || mode == "replacement-changed") && index == 1 {
				items = pod(newTestUID)
			}
			if mode == "replacement-changed" && index >= 2 {
				items = pod(otherTestUID)
			}
			if mode == "surge" && index == 1 {
				items = pod(oldTestUID) + "," + pod(newTestUID)
			}
			return []byte(`{"items":[` + items + `]}`), nil
		case strings.Contains(s, "--query-gpu=uuid"):
			if mode == "wrong-gpu" {
				return []byte("GPU-other\n"), nil
			}
			return []byte("GPU-test\n"), nil
		case strings.Contains(s, "--query-compute-apps"):
			if mode == "missing" && index == 1 {
				return nil, fmt.Errorf("private error")
			}
			if index == 0 || mode == "new-pod-old-gpu" && index == 1 {
				return []byte("101, GPU-test\n"), nil
			}
			if mode == "gpu-overlap" && index >= 2 {
				return []byte("101, GPU-test\n102, GPU-test\n"), nil
			}
			if index >= 2 || mode == "no-retirement" && index == 1 {
				return []byte("102, GPU-test\n"), nil
			}
			if mode == "orphan" {
				return []byte("103, GPU-test\n"), nil
			}
			return nil, nil
		case strings.Contains(s, "/proc/101/cgroup"):
			return []byte("0::/kubepods/pod" + oldTestUID + "/container\n"), nil
		case strings.Contains(s, "/proc/102/cgroup"):
			uid := newTestUID
			if mode == "replacement-changed" {
				uid = otherTestUID
			}
			return []byte("0::/kubepods.slice/kubepods-pod" + strings.ReplaceAll(uid, "-", "_") + ".slice/container\n"), nil
		case strings.Contains(s, "/proc/103/cgroup"):
			return []byte("0::/unrelated\n"), nil
		default:
			return nil, fmt.Errorf("unexpected read")
		}
	}
	return o
}

func TestKubernetesGPUReplacementReadOnlySamples(t *testing.T) {
	for _, mode := range []string{"good", "new-pod-before-gpu", "new-pod-old-gpu", "replacement-changed", "surge", "gpu-overlap", "no-retirement", "missing", "orphan", "wrong-gpu", "slow"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			o := fakeReplacementObserver(mode)
			armed := false
			o.armed = func() { armed = true }
			n, err := observeKubernetesGPUReplacement(ctx, o, time.Millisecond, 5*time.Millisecond, 4)
			if mode == "good" || mode == "new-pod-before-gpu" {
				if err != nil || n != 3 || !armed {
					t.Fatalf("valid sampled replacement rejected: n=%d err=%v", n, err)
				}
			} else if err == nil {
				t.Fatalf("%s was incorrectly accepted", mode)
			}
		})
	}
}

func TestKubernetesGPUCgroupOwnershipBoundaries(t *testing.T) {
	for _, cgroup := range []string{"0::/other/pod" + oldTestUID + "-suffix/container", "0::/unrelated", "0::/pod" + oldTestUID + "/pod" + newTestUID} {
		if _, err := gpuCgroupOwner(cgroup, []string{oldTestUID, newTestUID}); err == nil {
			t.Fatal("unattributed or ambiguous process accepted")
		}
	}
}
