package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestP08ConditioningObserverUsesOwnedProcessAndGPU(t *testing.T) {
	for _, mode := range []string{"complete", "pmon_unavailable", "pmon_idle", "wrong_mapping", "missing_gpu", "gpu_parse", "gpu_missing", "gpu_ambiguous", "gpu_overflow", "missing_rss", "changed_generation", "owner_before", "owner_after", "resource", "decoder", "gpu_after", "point"} {
		t.Run(mode, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "context", Namespace: "ns", NamespaceUID: "namespace-uid", Release: "release", APIPod: "api-0", APIUID: "api-uid", APIContainer: "api", BackendPod: "backend-0", BackendUID: oldTestUID, BackendContainer: "backend"}
			namespaceReads := 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				s := strings.Join(args, " ")
				switch {
				case strings.Contains(s, "get namespace"):
					namespaceReads++
					if mode == "owner_before" || mode == "owner_after" && namespaceReads > 8 {
						return nil, errors.New("private")
					}
					return []byte("namespace-uid"), nil
				case strings.Contains(s, "get pod"):
					role, uid := "api", "api-uid"
					if strings.Contains(s, "pod backend-0") {
						role, uid = "backend", oldTestUID
					}
					return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":%q}},"status":{"containerStatuses":[{"name":%q,"containerID":%q,"restartCount":0,"state":{"running":{}}}]}}`, uid, role, role, role)), nil
				case strings.Contains(s, "p08-identity"):
					if strings.HasSuffix(s, "backend") {
						return []byte("20 42 42"), nil
					}
					return []byte("10 0 0"), nil
				case strings.Contains(s, "/health"):
					return []byte(fmt.Sprintf(`{"status":"ready","model":"base","protocol_version":"1","configuration_fingerprint":"f","process_generation":"same","cuda_ready":true}`+"\n%d", http.StatusOK)), nil
				case strings.Contains(s, "p08-conditioning-rss"):
					if mode == "missing_rss" {
						return nil, errors.New("private")
					}
					return []byte(`{"supervisor":{"rss_bytes":100,"anonymous_bytes":80,"file_bytes":20},"native":{"rss_bytes":900,"anonymous_bytes":700,"file_bytes":200}}`), nil
				case strings.Contains(s, "p08-memory-decoder"):
					if mode == "decoder" {
						return nil, errors.New("private")
					}
					return []byte("OK\n"), nil
				case strings.Contains(s, "p08-descendants"), strings.Contains(s, "p08-scratch"):
					return []byte("OK\n"), nil
				case strings.Contains(s, "p08-resource"):
					if mode == "resource" {
						return nil, errors.New("private")
					}
					rss, native := 100, 0
					if strings.HasSuffix(s, "backend") {
						rss, native = 1000, 42
					}
					files := 0
					if mode == "point" && strings.HasSuffix(s, "api") {
						files = 1
					}
					return []byte(fmt.Sprintf(`{"rss_bytes":%d,"fd_count":8,"scratch_file_count":%d,"scratch_bytes":0,"native_pid":%d,"ownership_lock_files":0}`, rss, files, native)), nil
				default:
					return nil, errors.New("unexpected")
				}
			}
			gpu := fakeCUDAObserver("good")
			gpu.backendPod = "backend-0"
			gpu.observer.Kubeconfig = target.Kubeconfig
			gpu.observer.Context = target.Context
			baseRun := gpu.observer.run
			gpuNamespaceReads := 0
			gpu.observer.run = func(ctx context.Context, args ...string) ([]byte, error) {
				s := strings.Join(args, " ")
				if strings.Contains(s, "get namespace") {
					gpuNamespaceReads++
					if mode == "gpu_after" && gpuNamespaceReads > 1 {
						return nil, errors.New("private")
					}
				}
				if strings.Contains(s, "/status") {
					if mode == "wrong_mapping" {
						return []byte("NSpid: 101 43\n"), nil
					}
					return []byte("NSpid: 101 42\n"), nil
				}
				if strings.Contains(s, "nvidia-smi pmon") {
					if mode == "pmon_unavailable" {
						return nil, errors.New("private")
					}
					if mode == "pmon_idle" {
						return []byte("# gpu pid type sm mem fb\n0 101 C 0 0 10\n"), nil
					}
				}
				if strings.Contains(s, "--query-compute-apps") {
					switch mode {
					case "missing_gpu":
						return nil, errors.New("private")
					case "gpu_parse":
						return []byte("101, GPU-test, N/A\n"), nil
					case "gpu_missing":
						return []byte("102, GPU-test, 10\n"), nil
					case "gpu_ambiguous":
						return []byte("101, GPU-test, 10\n101, GPU-test, 10\n"), nil
					case "gpu_overflow":
						return []byte("101, GPU-test, 18446744073709551615\n"), nil
					}
				}
				if mode == "changed_generation" && strings.Contains(s, "/health") {
					return []byte(fmt.Sprintf(`{"status":"ready","model":"base","protocol_version":"1","configuration_fingerprint":"f","process_generation":"changed","cuda_ready":true}`+"\n%d", http.StatusOK)), nil
				}
				return baseRun(ctx, args...)
			}
			initial := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "api"}, {ID: "backend"}}, apiStart: 10, backendStart: 20, nativePID: 42, nativeStart: 42, generation: "same", fingerprint: "f"}
			observer := p08MemoryConditioningObserver{target: target, gpu: gpu, initial: initial, model: "base", port: "8081"}
			got, err := observer.observe(t.Context())
			if mode == "complete" || mode == "pmon_unavailable" || mode == "pmon_idle" {
				if err != nil || got.BackendRSSBytes != 1000 || got.APIRSSBytes != 100 || got.GPUBytes != 10*(1<<20) || got.BackendProcessRSS == nil {
					t.Fatalf("complete observation unavailable: %v", err)
				}
				if mode != "complete" {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					if _, err := observeCUDAActivity(ctx, gpu, nil, time.Millisecond, 100*time.Millisecond, 1); err == nil {
						t.Fatal("unavailable or idle pmon became activity proof")
					}
					return
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"text":"test"}`))
				}))
				defer server.Close()
				client, e := NewPublicClient(server.URL, time.Second, 4096)
				if e != nil {
					t.Fatal(e)
				}
				defer client.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				epoch, e := p08MemoryConditioningEpoch(ctx, observer, client, []byte("fixture"))
				if e != nil || calls.Load() != 20 || len(epoch.Idle) != 5 || len(epoch.NativeDescendantRSSBytes) != 6 || epoch.Completion.At.IsZero() {
					t.Fatalf("conditioning epoch did not preserve the 20-request and five-idle behavior: %v", e)
				}
			} else {
				categories := map[string]string{"wrong_mapping": "pid_mapping", "missing_gpu": "gpu_allocation_query", "gpu_parse": "gpu_allocation_parse", "gpu_missing": "gpu_allocation_missing", "gpu_ambiguous": "gpu_allocation_ambiguous", "gpu_overflow": "gpu_allocation_overflow", "missing_rss": "breakdown", "changed_generation": "gpu_identity", "owner_before": "owner_before", "owner_after": "owner_after", "resource": "resource", "decoder": "decoder", "gpu_after": "gpu_identity_after", "point": "point"}
				if err == nil || err.Error() != "memory observation: "+categories[mode] || !got.At.IsZero() {
					t.Fatal("unknown owner or safe observer failure category was lost")
				}
				namespaceReads, gpuNamespaceReads = 0, 0
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
				defer server.Close()
				client, e := NewPublicClient(server.URL, time.Second, 4096)
				if e != nil {
					t.Fatal(e)
				}
				defer client.Close()
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				epoch, e := p08MemoryConditioningEpoch(ctx, observer, client, []byte("fixture"))
				if e == nil || calls.Load() != 0 || epoch.RequestsAttempted != nil || epoch.ObservationPhase != "preflight" || epoch.ObserverStage != categories[mode] {
					t.Fatal("failure stage or unknown attempt count was not preserved")
				}
				details := map[string]string{"gpu_after": "namespace", "changed_generation": "mismatch"}
				if epoch.ObserverDetail != details[mode] {
					t.Fatal("safe identity subcategory was lost")
				}
				report := p08MemoryEvidence(epoch, 10, "fixture", "api", "backend", "epoch")
				if report.ObserverDetail != details[mode] {
					t.Fatal("safe identity subcategory was not reported")
				}
				encoded, e := json.Marshal(report)
				if e != nil || strings.Contains(string(encoded), "private") || report.Qualification != "unknown" || report.ObserverStage != categories[mode] {
					t.Fatal("unsafe or incomplete failure evidence")
				}
			}
		})
	}
}

func TestP08ConditioningDoesNotWorkWithoutRealEvidence(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewPublicClient(server.URL, time.Second, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	epoch, err := p08MemoryConditioningEpoch(ctx, p08MemoryConditioningObserver{model: "base"}, client, []byte("audio"))
	if err == nil || calls.Load() != 0 || !epoch.Completion.At.IsZero() || epoch.ObservationPhase != "preflight" || epoch.ObserverStage != "selection" || epoch.RequestsAttempted != nil {
		t.Fatal("unknown identity admitted conditioning workload")
	}
}

func TestP08ConditioningRequiresExactNamespacePIDMapping(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		good         bool
	}{
		{"matching", "Name: whisper-server\nNSpid:\t1300\t42\n", true},
		{"missing", "Name: whisper-server\n", false},
		{"host only", "NSpid: 1300\n", false},
		{"wrong host", "NSpid: 1301 42\n", false},
		{"wrong namespace", "NSpid: 1300 43\n", false},
		{"duplicate", "NSpid: 1300 42\nNSpid: 1300 42\n", false},
		{"malformed", "NSpid: 1300 x\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := p08MemoryNamespacePID([]byte(tc.status), 1300, 42)
			if (err == nil) != tc.good {
				t.Fatal("native PID mapping not validated")
			}
		})
	}
}

func TestP08ConditioningMapsCompleteMeasuredResources(t *testing.T) {
	at := time.Now()
	zero, one, api, backend, desc, gpu := uint64(0), uint64(1), uint64(100), uint64(1000), uint64(25), uint64(500)
	base := p08ResourceEvidence{ObservedAt: at, API: DockerResourceSnapshot{RSSBytes: &api, FDCount: &one, ScratchFileCount: &zero, ScratchBytes: &zero, OwnershipLockFiles: &zero}, Backend: DockerResourceSnapshot{RSSBytes: &backend}, NativeDescendantRSSBytes: &desc, APIDeletedScratchFDs: &zero, APIDeletedScratchBytes: &zero}
	breakdown := residentRSSBreakdown{At: at, Supervisor: residentProcessRSS{RSSBytes: 100, AnonymousBytes: 80, FileBytes: 20}, Native: residentProcessRSS{RSSBytes: 900, AnonymousBytes: 700, FileBytes: 200}}
	point, err := p08MemoryConditioningPoint(base, &breakdown, &zero, &gpu)
	if err != nil || point.APIRSSBytes != 100 || point.BackendRSSBytes != 1000 || point.GPUBytes != 500 || point.BackendProcessRSS == nil || point.NativeDescendantRSSBytes == nil || *point.NativeDescendantRSSBytes != 25 {
		t.Fatalf("complete observations not mapped: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*p08ResourceEvidence, *residentRSSBreakdown, **uint64, **uint64)
	}{
		{"missing GPU", func(_ *p08ResourceEvidence, _ *residentRSSBreakdown, _ **uint64, g **uint64) { *g = nil }},
		{"zero GPU", func(_ *p08ResourceEvidence, _ *residentRSSBreakdown, _ **uint64, g **uint64) { *g = &zero }},
		{"missing decoder", func(_ *p08ResourceEvidence, _ *residentRSSBreakdown, d **uint64, _ **uint64) { *d = nil }},
		{"retained descendants", func(_ *p08ResourceEvidence, _ *residentRSSBreakdown, d **uint64, _ **uint64) { *d = &one }},
		{"missing native descendants", func(e *p08ResourceEvidence, _ *residentRSSBreakdown, _ **uint64, _ **uint64) {
			e.NativeDescendantRSSBytes = nil
		}},
		{"unknown scratch", func(e *p08ResourceEvidence, _ *residentRSSBreakdown, _ **uint64, _ **uint64) {
			e.APIDeletedScratchFDs = nil
		}},
		{"retained scratch", func(e *p08ResourceEvidence, _ *residentRSSBreakdown, _ **uint64, _ **uint64) {
			e.APIDeletedScratchBytes = &one
		}},
		{"invalid process rss", func(_ *p08ResourceEvidence, b *residentRSSBreakdown, _ **uint64, _ **uint64) { b.Native.RSSBytes = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, b, d, g := base, breakdown, &zero, &gpu
			tc.mutate(&e, &b, &d, &g)
			if _, err := p08MemoryConditioningPoint(e, &b, d, g); err == nil {
				t.Fatal("incomplete or retained resource accepted")
			}
		})
	}
}
