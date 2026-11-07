package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

type p08HeapContainer struct {
	Image   string
	Started time.Time
}
type p08HeapOwner struct {
	Process    p08RepeatedIdentity
	Containers [2]p08HeapContainer
}

// A separate bracket obtains actual image IDs and start times; identity never
// comes from requested Helm values or a synthetic placeholder.
func p08ReadHeapOwner(ctx context.Context, target p08ResourceTarget, model, port string) (p08HeapOwner, error) {
	var out p08HeapOwner
	before, err := p08RepeatedOwner(ctx, target, model, port)
	if err != nil {
		return out, errResidentSupervisorHeapUnknown
	}
	for i, s := range []struct{ pod, uid, container string }{{target.APIPod, target.APIUID, target.APIContainer}, {target.BackendPod, target.BackendUID, target.BackendContainer}} {
		data, e := target.command(ctx, "get", "pod", s.pod, "-o", "json")
		if e != nil {
			return p08HeapOwner{}, errResidentSupervisorHeapUnknown
		}
		var pod struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
			Status struct {
				Containers []struct {
					Name, ContainerID, ImageID string
					RestartCount               *int
					State                      struct {
						Running *struct {
							StartedAt time.Time `json:"startedAt"`
						} `json:"running"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		}
		if json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != s.uid {
			return p08HeapOwner{}, errResidentSupervisorHeapUnknown
		}
		found := 0
		for _, c := range pod.Status.Containers {
			if c.Name == s.container {
				found++
				if c.ContainerID != before.containers[i].ID || c.RestartCount == nil || *c.RestartCount != before.containers[i].Restarts || c.ImageID == "" || c.State.Running == nil || c.State.Running.StartedAt.IsZero() {
					return p08HeapOwner{}, errResidentSupervisorHeapUnknown
				}
				out.Containers[i] = p08HeapContainer{Image: c.ImageID, Started: c.State.Running.StartedAt}
			}
		}
		if found != 1 {
			return p08HeapOwner{}, errResidentSupervisorHeapUnknown
		}
	}
	after, err := p08RepeatedOwner(ctx, target, model, port)
	if err != nil || after != before {
		return p08HeapOwner{}, errResidentSupervisorHeapUnknown
	}
	out.Process = before
	return out, nil
}

type p08HeapCalibration struct {
	Before, After            p08AnchoredEvidence          `json:"-"`
	APIBound, BackendBound   *residentSupervisorHeapBound `json:"-"` // Scopes contain private identities.
	CalibrationObservation   *p08AnchoredEvidence         `json:"calibration_observation,omitempty"`
	PostworkObservation      *p08AnchoredEvidence         `json:"postwork_observation,omitempty"`
	APICalibrationCycles     int                          `json:"api_calibration_cycles,omitempty"`
	BackendCalibrationCycles int                          `json:"backend_calibration_cycles,omitempty"`
	APIPostCycles            int                          `json:"api_post_cycles,omitempty"`
	BackendPostCycles        int                          `json:"backend_post_cycles,omitempty"`
	APIInterval              *residentByteInterval        `json:"api_interval,omitempty"`
	BackendInterval          *residentByteInterval        `json:"backend_interval,omitempty"`
	Requests                 int                          `json:"requests,omitempty"`
	Verdict                  string                       `json:"verdict"`
	Limitation               string                       `json:"limitation"`
}

// Bracket independent natural windows around a real workload. Reuse the P07
// completion barrier, exact frozen interval and post-work guard. The CRI stream
// limitation prevents this diagnostic from claiming an acceptance verdict.
func calibrateP08Heap(ctx context.Context, model string, owner func(context.Context) (p08HeapOwner, error), window func(context.Context) (p08AnchoredEvidence, error), workload func(context.Context) (int, error)) (p08HeapCalibration, error) {
	out := p08HeapCalibration{Verdict: "unknown", Limitation: "partial diagnostic only: CRI stream completeness, P07 resource envelopes, long-input conditioning and memory gate are not established"}
	if model == "" || owner == nil || window == nil || workload == nil {
		return out, errResidentSupervisorHeapUnknown
	}
	initial, err := owner(ctx)
	if err != nil {
		return out, errResidentSupervisorHeapUnknown
	}
	validOwner := func(current p08HeapOwner) bool {
		return current == initial && initial.Process.containers[0].ID != "" && initial.Process.containers[1].ID != "" && initial.Process.apiStart != 0 && initial.Process.backendStart != 0 && initial.Process.nativePID > 1 && initial.Process.nativeStart != 0 && initial.Process.generation != "" && initial.Process.fingerprint != "" && initial.Containers[0].Image != "" && initial.Containers[1].Image != "" && !initial.Containers[0].Started.IsZero() && !initial.Containers[1].Started.IsZero()
	}
	if !validOwner(initial) {
		return out, errResidentSupervisorHeapUnknown
	}
	checked := func() error {
		current, e := owner(ctx)
		if e != nil || !validOwner(current) || ctx.Err() != nil {
			return errResidentSupervisorHeapUnknown
		}
		return nil
	}
	out.Before, err = window(ctx)
	if err != nil {
		var diagnostic *p08GCDiagnosticError
		if errors.As(err, &diagnostic) && diagnostic != nil {
			return out, diagnostic
		}
		return out, errResidentSupervisorHeapUnknown
	}
	if checked() != nil {
		return out, errResidentSupervisorHeapUnknown
	}
	out.CalibrationObservation = &out.Before
	windows := []p08AnchoredWindow{out.Before.API, out.Before.Backend}
	bounds := make([]*residentSupervisorHeapBound, 2)
	for i, role := range []string{"api", "backend"} {
		w := windows[i]
		policy := residentSupervisorHeapPolicy{Scope: residentSupervisorHeapScope{Role: role, ContainerID: initial.Process.containers[i].ID, ImageID: initial.Containers[i].Image, StartedAt: initial.Containers[i].Started, NativeGeneration: initial.Process.generation, Model: model}, OrdinaryStart: w.Start, ProfileVerified: true}
		bounds[i], err = freezeResidentSupervisorPostWork(policy, w.GC, w.Start, w.End)
		if err != nil {
			return out, err
		}
	}
	out.APIBound, out.BackendBound = bounds[0], bounds[1]
	out.APIInterval, out.BackendInterval = &bounds[0].MarkedLiveBytes, &bounds[1].MarkedLiveBytes
	proof0, _ := residentPostWorkCycles(bounds[0].Policy, windows[0].GC, windows[0].Start, windows[0].End)
	proof1, _ := residentPostWorkCycles(bounds[1].Policy, windows[1].GC, windows[1].Start, windows[1].End)
	out.APICalibrationCycles, out.BackendCalibrationCycles = len(proof0.Comparable), len(proof1.Comparable)
	if err = checked(); err != nil {
		return out, err
	}
	out.Requests, err = workload(ctx)
	if err != nil || out.Requests < 30 || checked() != nil {
		return out, errResidentSupervisorHeapUnknown
	}
	out.After, err = window(ctx)
	if err != nil {
		var diagnostic *p08GCDiagnosticError
		if errors.As(err, &diagnostic) && diagnostic != nil {
			return out, diagnostic
		}
		return out, errResidentSupervisorHeapUnknown
	}
	if checked() != nil {
		return out, errResidentSupervisorHeapUnknown
	}
	out.PostworkObservation = &out.After
	post := []p08AnchoredWindow{out.After.API, out.After.Backend}
	for i, w := range post {
		if !w.Start.After(windows[i].End) {
			return out, errResidentSupervisorHeapUnknown
		}
		cycles, e := checkResidentSupervisorHeap(bounds[i], w.GC, bounds[i].FrozenAt, w.Start, w.End)
		if e != nil {
			return out, e
		}
		if i == 0 {
			out.APIPostCycles = len(cycles)
		} else {
			out.BackendPostCycles = len(cycles)
		}
	}
	out.Verdict = "partial_not_qualified"
	return out, nil
}

func TestP08GCHeapCalibration(t *testing.T) {
	if os.Getenv("P08_GC_CALIBRATION") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes pair")
	}
	address, model, fixtures, dir := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES"), os.Getenv("P08_RESOURCE_EVIDENCE_DIR")
	if !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(fixtures) || !filepath.IsAbs(dir) {
		t.Fatal("explicit loopback API, model and absolute private directories required")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("existing mode 0700 private evidence directory required")
	}
	target, err := readP08ResourceTarget(os.Getenv("P08_RESOURCE_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	audio, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("workload configuration unavailable")
	}
	port := strconv.Itoa(settings.Backend.Port)
	timeout := time.Duration(settings.Timeouts.OperationSeconds+settings.Timeouts.CleanupSeconds+5) * time.Second
	client, err := NewPublicClient(address, timeout, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 31*timeout+15*time.Minute)
	defer cancel()
	owner := func(ctx context.Context) (p08HeapOwner, error) { return p08ReadHeapOwner(ctx, target, model, port) }
	initial, err := owner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	process := func(ctx context.Context) (p08RepeatedIdentity, error) {
		o, e := owner(ctx)
		if e != nil || o != initial {
			return p08RepeatedIdentity{}, errResidentSupervisorHeapUnknown
		}
		return o.Process, nil
	}
	// Each phase retains its caller's deadline and uses at most 4096 natural GETs.
	window := func(ctx context.Context) (p08AnchoredEvidence, error) {
		return observeP08AnchoredGCBatch(ctx, target, client, port, process, residentHealthObservationMaxCalls)
	}
	workload := func(ctx context.Context) (int, error) {
		r, e := measureP08RepeatedResources(ctx, client, audio, model, 30, process, target.observe)
		return r.Requests, e
	}
	evidence, measurementErr := calibrateP08Heap(ctx, model, owner, window, workload)
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("calibration encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-gc-calibration-partial.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("calibration evidence creation failed")
	}
	_, werr := f.Write(append(bytes.TrimSpace(data), '\n'))
	cerr := f.Close()
	if errors.Join(werr, cerr) != nil {
		t.Fatal("calibration evidence write failed")
	}
	if measurementErr != nil {
		t.Fatal(measurementErr)
	}
	t.Log("partial natural Go heap calibration and guard recorded; P08 memory qualification not established")
}
