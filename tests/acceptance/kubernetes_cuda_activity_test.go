package acceptance

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This is an independent read-only probe, not a transcription workload. The
// health generation is an opaque supervisor token, not a host PID. Unchanged
// generation and PID/starttime are correlated but cannot alone prove their
// mapping. The opt-in test reports that missing link as a skipped gate.
type kubernetesCUDAActivity struct {
	observer    kubernetesGPUObserver
	backendPod  string
	backendPort int
}

// Fixed categories describe which read failed without exposing its input/output.
type p08CUDAIdentityStage string

const (
	p08CUDAIdentityNamespace       p08CUDAIdentityStage = "namespace"
	p08CUDAIdentityDriver          p08CUDAIdentityStage = "driver"
	p08CUDAIdentityPod             p08CUDAIdentityStage = "pod"
	p08CUDAIdentityGPU             p08CUDAIdentityStage = "gpu"
	p08CUDAIdentityEnumeration     p08CUDAIdentityStage = "enumeration"
	p08CUDAIdentityEnumGuard       p08CUDAIdentityStage = "enumeration_guard"
	p08CUDAIdentityEnumAccess      p08CUDAIdentityStage = "enumeration_access"
	p08CUDAIdentityEnumIO          p08CUDAIdentityStage = "enumeration_io"
	p08CUDAIdentityEnumFormat      p08CUDAIdentityStage = "enumeration_format"
	p08CUDAIdentityEnumUnconfirmed p08CUDAIdentityStage = "enumeration_unconfirmed"
	p08CUDAIdentityCgroup          p08CUDAIdentityStage = "cgroup"
	p08CUDAIdentityStat            p08CUDAIdentityStage = "stat"
	p08CUDAIdentityHealth          p08CUDAIdentityStage = "health"
	p08CUDAIdentityMissing         p08CUDAIdentityStage = "native_missing"
	p08CUDAIdentityAmbiguous       p08CUDAIdentityStage = "native_ambiguous"
	p08CUDAIdentityMismatch        p08CUDAIdentityStage = "mismatch"
)

func (s p08CUDAIdentityStage) Error() string { return "CUDA identity: " + string(s) }

type cudaLifetime struct {
	pid               int
	start, generation string
}

type cudaActivityResult struct {
	samples         int
	peakSM, peakMiB uint64
}

func (a kubernetesCUDAActivity) health(ctx context.Context) (*DockerPrivateHealth, error) {
	if a.backendPod == "" || a.backendPort < 1 || a.backendPort > 65535 {
		return nil, errors.New("backend observation selection unavailable")
	}
	data, err := a.observer.command(ctx, a.observer.Namespace, "exec", a.backendPod, "--", "curl", "--silent", "--show-error", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "2", "--write-out", "\n%{http_code}", "http://127.0.0.1:"+strconv.Itoa(a.backendPort)+"/health")
	if err != nil {
		return nil, errors.New("private health observation unavailable")
	}
	h, err := parsePrivateHealth(data)
	if err != nil || h.HTTPStatus != http.StatusOK || !h.CUDAReady || (h.Status != "ready" && h.Status != "busy") || h.ProcessGeneration == "" {
		return nil, errors.New("resident CUDA health observation unavailable")
	}
	return h, nil
}

func p08CUDAEnumeration(data []byte) ([]int, error) {
	switch string(data) {
	case "ERROR:guard\n":
		return nil, p08CUDAIdentityEnumGuard
	case "ERROR:access\n":
		return nil, p08CUDAIdentityEnumAccess
	case "ERROR:io\n":
		return nil, p08CUDAIdentityEnumIO
	case "ERROR:format\n":
		return nil, p08CUDAIdentityEnumFormat
	case "ERROR:unconfirmed\n":
		return nil, p08CUDAIdentityEnumUnconfirmed
	}
	if !bytes.HasPrefix(data, []byte("OK\n")) || !bytes.HasSuffix(data, []byte("END\n")) || len(data) < 7 || len(data) > 4096 {
		return nil, p08CUDAIdentityEnumFormat
	}
	lines := strings.Split(string(data[3:len(data)-4]), "\n")
	if len(lines) > 257 {
		return nil, p08CUDAIdentityEnumFormat
	}
	ids := make([]int, 0, len(lines)-1)
	seen := make(map[int]bool)
	for _, s := range lines[:len(lines)-1] {
		pid, err := strconv.Atoi(s)
		if err != nil || pid <= 1 || strconv.Itoa(pid) != s || seen[pid] {
			return nil, p08CUDAIdentityEnumFormat
		}
		seen[pid] = true
		ids = append(ids, pid)
	}
	return ids, nil
}

func (a kubernetesCUDAActivity) identity(ctx context.Context) (cudaLifetime, error) {
	o := a.observer
	var result cudaLifetime
	ns, err := o.command(ctx, o.Namespace, "get", "namespace", o.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(ns) != o.NamespaceUID {
		return result, p08CUDAIdentityNamespace
	}
	readPod := func(namespace, name string) (observedPod, error) {
		var pod observedPod
		b, err := o.command(ctx, namespace, "get", "pod", name, "-o", "json")
		if err != nil || json.Unmarshal(b, &pod) != nil {
			return pod, errors.New("Pod identity unavailable")
		}
		return pod, nil
	}
	driver, err := readPod(o.DriverNamespace, o.DriverPod)
	if err != nil || driver.Metadata.UID != o.DriverUID || !driver.Spec.HostPID || driver.Spec.NodeName == "" {
		return result, p08CUDAIdentityDriver
	}
	backend, err := readPod(o.Namespace, a.backendPod)
	if err != nil || backend.Metadata.UID != o.OldPodUID || backend.Spec.NodeName != driver.Spec.NodeName || backend.Metadata.Labels["app.kubernetes.io/instance"] != o.Release || backend.Metadata.Labels["app.kubernetes.io/component"] != "backend" {
		return result, p08CUDAIdentityPod
	}
	devices, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "nvidia-smi", "--query-gpu=uuid", "--format=csv,noheader,nounits")
	matches := 0
	for _, line := range strings.Split(string(devices), "\n") {
		if strings.TrimSpace(line) == o.GPUUUID {
			matches++
		}
	}
	if err != nil || matches != 1 {
		return result, p08CUDAIdentityGPU
	}
	// The guarded Perl probe emits a complete framed numeric list, never names.
	// Missing Perl, inaccessible /proc or an unverified host view fail closed.
	data, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "perl", "-e", p08MemoryNativeProcProbe, "/proc")
	if err != nil {
		return result, p08CUDAIdentityEnumeration
	}
	ids, err := p08CUDAEnumeration(data)
	if err != nil {
		return result, err
	}
	for _, pid := range ids {
		group, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "cat", fmt.Sprintf("/proc/%d/cgroup", pid))
		if err != nil || len(group) == 0 {
			return result, p08CUDAIdentityCgroup
		}
		owner, err := gpuCgroupOwner(string(group), []string{o.OldPodUID})
		if err != nil {
			continue
		} // Another Pod's native process is not this owner.
		if owner != o.OldPodUID || result.pid != 0 {
			return cudaLifetime{}, p08CUDAIdentityAmbiguous
		}
		result.pid = pid
	}
	if result.pid == 0 {
		return result, p08CUDAIdentityMissing
	}
	stat, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "cat", fmt.Sprintf("/proc/%d/stat", result.pid))
	if err != nil {
		return result, p08CUDAIdentityStat
	}
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return result, p08CUDAIdentityStat
	}
	fields := strings.Fields(string(stat[end+2:]))
	if len(fields) < 20 {
		return result, p08CUDAIdentityStat
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return result, p08CUDAIdentityStat
	}
	result.start = fields[19]
	h, err := a.health(ctx)
	if err != nil {
		return result, p08CUDAIdentityHealth
	}
	result.generation = h.ProcessGeneration
	return result, nil
}

type p08GPUAllocationError string

const (
	p08GPUAllocationQuery     p08GPUAllocationError = "gpu_allocation_query"
	p08GPUAllocationParse     p08GPUAllocationError = "gpu_allocation_parse"
	p08GPUAllocationMissing   p08GPUAllocationError = "gpu_allocation_missing"
	p08GPUAllocationAmbiguous p08GPUAllocationError = "gpu_allocation_ambiguous"
)

func (e p08GPUAllocationError) Error() string { return "GPU allocation: " + string(e) }

// A resource point requires an owned nonzero allocation, not GPU activity.
// MiB is the unit of the nvidia-smi query and is converted by the caller.
func (a kubernetesCUDAActivity) allocation(ctx context.Context, pid int) (uint64, error) {
	o := a.observer
	if pid <= 1 || o.GPUUUID == "" {
		return 0, p08GPUAllocationMissing
	}
	data, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "nvidia-smi", "--query-compute-apps=pid,gpu_uuid,used_gpu_memory", "--format=csv,noheader,nounits")
	if err != nil {
		return 0, p08GPUAllocationQuery
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) > 256 {
		return 0, p08GPUAllocationParse
	}
	var memory uint64
	found := 0
	for _, row := range rows {
		if len(row) != 3 {
			return 0, p08GPUAllocationParse
		}
		p, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil || p <= 0 {
			return 0, p08GPUAllocationParse
		}
		if p != pid || strings.TrimSpace(row[1]) != o.GPUUUID {
			continue
		}
		memory, err = strconv.ParseUint(strings.TrimSpace(row[2]), 10, 64)
		if err != nil || memory == 0 {
			return 0, p08GPUAllocationParse
		}
		found++
	}
	if found == 0 {
		return 0, p08GPUAllocationMissing
	}
	if found != 1 {
		return 0, p08GPUAllocationAmbiguous
	}
	return memory, nil
}

func (a kubernetesCUDAActivity) sample(ctx context.Context, pid int) (uint64, uint64, error) {
	memory, err := a.allocation(ctx, pid)
	if err != nil {
		return 0, 0, err
	}
	o := a.observer
	data, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "nvidia-smi", "pmon", "-i", o.GPUUUID, "-s", "um", "-d", "1", "-c", "1")
	if err != nil {
		return 0, 0, errors.New("GPU process activity unavailable")
	}
	r := gpuActivityReader{pid: pid}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		before := r.activity.Samples
		r.sample(line)
		if len(fields) > 0 && fields[0] != "#" {
			column, ok := r.columns["pid"]
			if !ok || column >= len(fields) {
				return 0, 0, errors.New("GPU process activity row unavailable")
			}
			if fields[column] == strconv.Itoa(pid) && r.activity.Samples == before {
				return 0, 0, errors.New("native GPU process activity row unavailable")
			}
		}
	}
	if r.activity.Samples != 1 || r.activity.PeakMemoryBytes == 0 {
		return 0, 0, errors.New("GPU process activity sample unavailable or ambiguous")
	}
	return r.activity.PeakSMPercent, memory, nil
}

// A positive sample must be bracketed by busy health and the same PID lifetime
// and generation. Probe gaps and unparseable observations are errors, not zero.
func observeCUDAActivity(ctx context.Context, a kubernetesCUDAActivity, armed func(), cadence, maxGap time.Duration, maxSamples int) (cudaActivityResult, error) {
	var result cudaActivityResult
	if _, ok := ctx.Deadline(); !ok || cadence <= 0 || maxGap < cadence || maxSamples < 1 {
		return result, errors.New("invalid CUDA observation bounds")
	}
	before, err := a.identity(ctx)
	if err != nil {
		return result, err
	}
	if armed != nil {
		armed()
	}
	last := time.Now()
	for result.samples < maxSamples {
		timer := time.NewTimer(cadence)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, errors.New("CUDA observation timed out")
		case <-timer.C:
		}
		h, err := a.health(ctx)
		if err != nil || h.ProcessGeneration != before.generation {
			return result, errors.New("CUDA health generation changed or became unavailable")
		}
		if time.Since(last) > maxGap {
			return result, errors.New("CUDA observation gap exceeded")
		}
		last = time.Now()
		if h.Status != "busy" {
			continue
		}
		sm, memory, err := a.sample(ctx, before.pid)
		if err != nil || ctx.Err() != nil {
			return result, errors.New("CUDA activity observation unavailable")
		}
		if time.Since(last) > maxGap {
			return result, errors.New("CUDA observation gap exceeded")
		}
		last = time.Now()
		result.samples++
		result.peakSM = max(result.peakSM, sm)
		result.peakMiB = max(result.peakMiB, memory)
		if sm == 0 {
			continue
		}
		afterHealth, err := a.health(ctx)
		if err != nil || afterHealth.Status != "busy" || afterHealth.ProcessGeneration != before.generation {
			return result, errors.New("busy generation did not bracket CUDA activity")
		}
		after, err := a.identity(ctx)
		if err != nil || after != before || ctx.Err() != nil {
			return result, errors.New("CUDA resident lifetime or generation changed")
		}
		if time.Since(last) > maxGap {
			return result, errors.New("CUDA observation gap exceeded")
		}
		return result, nil
	}
	return result, errors.New("positive CUDA activity not observed within sample bound")
}

func TestP08SampledCUDAActivity(t *testing.T) {
	if os.Getenv("P08_OBSERVE_CUDA_ACTIVITY") != "1" {
		t.Skip("requires an independently authorized public workload window")
	}
	file, err := os.Open(os.Getenv("P08_CUDA_ACTIVITY_TARGET"))
	if err != nil {
		t.Fatal("private CUDA selection unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("private CUDA selection exceeds bound")
	}
	var v struct {
		Kubeconfig      string `json:"kubeconfig"`
		Context         string `json:"context"`
		Namespace       string `json:"namespace"`
		NamespaceUID    string `json:"namespace_uid"`
		Release         string `json:"release"`
		DriverNamespace string `json:"driver_namespace"`
		DriverPod       string `json:"driver_pod"`
		DriverUID       string `json:"driver_uid"`
		GPUUUID         string `json:"gpu_uuid"`
		BackendPod      string `json:"backend_pod"`
		BackendPodUID   string `json:"backend_pod_uid"`
		BackendPort     int    `json:"backend_port"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&v) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("invalid private CUDA selection")
	}
	o := kubernetesGPUObserver{Kubeconfig: v.Kubeconfig, Context: v.Context, Namespace: v.Namespace, NamespaceUID: v.NamespaceUID, Release: v.Release, DriverNamespace: v.DriverNamespace, DriverPod: v.DriverPod, DriverUID: v.DriverUID, GPUUUID: v.GPUUUID, OldPodUID: v.BackendPodUID}
	a := kubernetesCUDAActivity{observer: o, backendPod: v.BackendPod, backendPort: v.BackendPort}
	marker := os.Getenv("P08_CUDA_ACTIVITY_ARMED")
	if marker == "" {
		t.Fatal("private armed marker is required")
	}
	armed := func() {
		f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal("cannot create CUDA observation marker")
		}
		if f.Close() != nil {
			t.Fatal("cannot close CUDA observation marker")
		}
		t.Log("CUDA observation armed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	result, err := observeCUDAActivity(ctx, a, armed, time.Second, 20*time.Second, 120)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("observed %d discrete busy samples, peak SM %d%% and allocation %d MiB; not continuous evidence", result.samples, result.peakSM, result.peakMiB)
	t.Skip("generation-to-host-PID mapping is not directly observable; correlated lifetime is partial CUDA evidence, not a complete gate")
}
