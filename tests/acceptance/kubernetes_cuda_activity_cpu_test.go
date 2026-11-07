package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fakeCUDAObserver(mode string) kubernetesCUDAActivity {
	identityCount, healthCount := 0, 0
	o := kubernetesGPUObserver{Kubeconfig: "explicit", Context: "explicit", Namespace: "ns", NamespaceUID: "namespace-uid", Release: "release", DriverNamespace: "driver-ns", DriverPod: "driver", DriverUID: "driver-uid", GPUUUID: "GPU-test", OldPodUID: oldTestUID}
	o.run = func(ctx context.Context, args ...string) ([]byte, error) {
		s := strings.Join(args, " ")
		switch {
		case strings.Contains(s, "get namespace"):
			identityCount++
			return []byte("namespace-uid"), nil
		case strings.Contains(s, "get pod driver"):
			return []byte(`{"metadata":{"uid":"driver-uid"},"spec":{"nodeName":"node","hostPID":true}}`), nil
		case strings.Contains(s, "get pod backend"):
			return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":"backend"}},"spec":{"nodeName":"node"}}`, oldTestUID)), nil
		case strings.Contains(s, "--query-gpu=uuid"):
			return []byte("GPU-test\n"), nil
		case strings.Contains(s, "perl -e"):
			if mode == "multiple" {
				return []byte("OK\n101\n102\nEND\n"), nil
			}
			return []byte("OK\n101\nEND\n"), nil
		case strings.Contains(s, "/cgroup"):
			if mode == "wrong-pod" {
				return []byte("0::/kubepods/pod" + newTestUID + "/container\n"), nil
			}
			return []byte("0::/kubepods/pod" + oldTestUID + "/container\n"), nil
		case strings.Contains(s, "/stat"):
			start := "42"
			if mode == "pid-reuse" && identityCount > 1 {
				start = "43"
			}
			return []byte("101 (whisper-server) " + strings.Repeat("0 ", 19) + start + " 0\n"), nil
		case strings.Contains(s, "/health"):
			healthCount++
			generation := "same"
			if mode == "generation" && healthCount > 1 {
				generation = "changed"
			}
			return []byte(fmt.Sprintf(`{"status":"busy","model":"base","process_generation":%q,"protocol_version":"1","configuration_fingerprint":"x","cuda_ready":true}`+"\n"+strconv.Itoa(http.StatusOK), generation)), nil
		case strings.Contains(s, "--query-compute-apps"):
			if mode == "allocation" {
				return []byte("101, GPU-test, N/A\n"), nil
			}
			return []byte("101, GPU-test, 10\n"), nil
		case strings.Contains(s, "nvidia-smi pmon"):
			if mode == "gap" {
				time.Sleep(12 * time.Millisecond)
			}
			if mode == "unknown" {
				return []byte("# gpu pid type sm mem fb\n0 101 C - 0 10\n"), nil
			}
			if mode == "zero" {
				return []byte("# gpu pid type sm mem fb\n0 101 C 0 0 10\n"), nil
			}
			if mode == "pmon-error" {
				return nil, errors.New("private error")
			}
			return []byte("# gpu pid type sm mem fb\n0 101 C 25 0 10\n"), nil
		default:
			return nil, errors.New("unexpected read")
		}
	}
	return kubernetesCUDAActivity{observer: o, backendPod: "backend", backendPort: 8081}
}

func TestKubernetesCUDAActivityCPU(t *testing.T) {
	for _, mode := range []string{"good", "multiple", "wrong-pod", "pid-reuse", "generation", "allocation", "unknown", "zero", "pmon-error", "gap"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			armed := false
			result, err := observeCUDAActivity(ctx, fakeCUDAObserver(mode), func() { armed = true }, time.Millisecond, 8*time.Millisecond, 2)
			if mode == "good" {
				if err != nil || !armed || result.samples != 1 || result.peakSM != 25 || result.peakMiB != 10 {
					t.Fatalf("known CUDA sample rejected: %+v, %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("%s evidence was accepted", mode)
			}
		})
	}
}
