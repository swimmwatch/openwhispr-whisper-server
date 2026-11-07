package acceptance

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The operator supplies a verified, existing hostPID driver Pod on the same
// node as the selected backend. This helper does not deploy or change anything.
// Pod UIDs and GPU identity are private inputs and must not be reported.
type kubernetesGPUObserver struct {
	Kubeconfig, Context, Namespace, Release string
	DriverNamespace, DriverPod, GPUUUID     string
	OldPodUID, NamespaceUID, DriverUID      string
	run                                     func(context.Context, ...string) ([]byte, error)
	armed                                   func()
}

type kubernetesGPUSample struct {
	OldPods, NewPods, OldGPU, NewGPU int
	NewPodUID                        string
}

// kubectlOutput bounds both execution and stdout; errors never include command
// output or private selectors. No shell interpolation is used.
func kubectlOutput(ctx context.Context, args ...string) ([]byte, error) {
	return runKubernetesCommand(ctx, "kubectl", 5*time.Second, 1<<20, nil, args...)
}

func (o kubernetesGPUObserver) command(ctx context.Context, namespace string, args ...string) ([]byte, error) {
	if o.Kubeconfig == "" || o.Context == "" || namespace == "" || o.Release == "" || o.DriverNamespace == "" || o.DriverPod == "" || o.GPUUUID == "" || o.OldPodUID == "" || o.NamespaceUID == "" || o.DriverUID == "" {
		return nil, errors.New("explicit private Kubernetes observation selection is required")
	}
	run := o.run
	if run == nil {
		run = kubectlOutput
	}
	prefix := []string{"--kubeconfig", o.Kubeconfig, "--context", o.Context, "--namespace", namespace, "--request-timeout=4s"}
	return run(ctx, append(prefix, args...)...)
}

type observedPod struct {
	Metadata struct {
		UID    string            `json:"uid"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		NodeName string `json:"nodeName"`
		HostPID  bool   `json:"hostPID"`
	} `json:"spec"`
}

// gpuCgroupOwner accepts Kubernetes pod UID spellings used by cgroupfs and
// systemd; a row with no unique selected Pod owner is not silently discarded.
func gpuCgroupOwner(cgroup string, uids []string) (string, error) {
	owner := ""
	for _, uid := range uids {
		if uid == "" {
			return "", errors.New("missing Pod identity")
		}
		if len(uid) != 36 {
			return "", errors.New("invalid Pod identity")
		}
		match := func(token string) bool {
			for _, part := range strings.FieldsFunc(cgroup, func(r rune) bool { return r == '/' || r == ':' || r == '\n' }) {
				if part == token || part == token+".slice" {
					return true
				}
				if strings.HasSuffix(part, "-"+token+".slice") {
					return true
				}
			}
			return false
		}
		if match("pod"+uid) || match("pod"+strings.ReplaceAll(uid, "-", "_")) {
			if owner != "" {
				return "", errors.New("ambiguous GPU process ownership")
			}
			owner = uid
		}
	}
	if owner == "" {
		return "", errors.New("GPU process ownership unavailable")
	}
	return owner, nil
}

func (o kubernetesGPUObserver) sample(ctx context.Context) (kubernetesGPUSample, error) {
	var result kubernetesGPUSample
	namespace, err := o.command(ctx, o.Namespace, "get", "namespace", o.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(namespace) != o.NamespaceUID {
		return result, errors.New("namespace identity unavailable")
	}
	driverData, err := o.command(ctx, o.DriverNamespace, "get", "pod", o.DriverPod, "-o", "json")
	var driver observedPod
	if err != nil || json.Unmarshal(driverData, &driver) != nil || driver.Metadata.UID != o.DriverUID || !driver.Spec.HostPID || driver.Spec.NodeName == "" {
		return result, errors.New("host PID driver observation unavailable")
	}
	data, err := o.command(ctx, o.Namespace, "get", "pods", "-l", "app.kubernetes.io/instance="+o.Release+",app.kubernetes.io/component=backend", "-o", "json")
	var list struct {
		Items []observedPod `json:"items"`
	}
	if err != nil || json.Unmarshal(data, &list) != nil || len(list.Items) > 2 {
		return result, errors.New("backend Pod observation unavailable or exceeds one-GPU scope")
	}
	uids := []string{o.OldPodUID}
	for _, pod := range list.Items {
		if pod.Metadata.UID == "" || pod.Spec.NodeName != driver.Spec.NodeName || pod.Metadata.Labels["app.kubernetes.io/instance"] != o.Release || pod.Metadata.Labels["app.kubernetes.io/component"] != "backend" {
			return result, errors.New("backend Pod ownership or node is unavailable")
		}
		if pod.Metadata.UID == o.OldPodUID {
			result.OldPods++
		} else {
			result.NewPods++
			result.NewPodUID = pod.Metadata.UID
			uids = append(uids, pod.Metadata.UID)
		}
	}
	if result.OldPods > 1 || result.NewPods > 1 {
		return result, errors.New("ambiguous backend Pod identity")
	}
	devices, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "nvidia-smi", "--query-gpu=uuid", "--format=csv,noheader,nounits")
	found := 0
	for _, device := range strings.Split(string(devices), "\n") {
		if strings.TrimSpace(device) == o.GPUUUID {
			found++
		}
	}
	if err != nil || found != 1 {
		return result, errors.New("selected GPU identity unavailable")
	}
	data, err = o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "nvidia-smi", "--query-compute-apps=pid,gpu_uuid", "--format=csv,noheader,nounits")
	if err != nil {
		return result, errors.New("GPU process observation unavailable")
	}
	reader := csv.NewReader(bytes.NewReader(data))
	rows, err := reader.ReadAll()
	if err != nil || len(rows) > 256 {
		return result, errors.New("GPU process observation malformed or exceeds bound")
	}
	seen := make(map[int]bool)
	for _, row := range rows {
		if len(row) != 2 {
			return result, errors.New("GPU process row malformed")
		}
		pid, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil || pid <= 0 || strings.TrimSpace(row[1]) == "" {
			return result, errors.New("GPU process identity unavailable")
		}
		if strings.TrimSpace(row[1]) != o.GPUUUID {
			continue
		}
		if seen[pid] {
			return result, errors.New("duplicate GPU process observation")
		}
		seen[pid] = true
		cgroup, err := o.command(ctx, o.DriverNamespace, "exec", o.DriverPod, "--", "cat", fmt.Sprintf("/proc/%d/cgroup", pid))
		if err != nil || len(cgroup) == 0 {
			return result, errors.New("GPU process cgroup unavailable")
		}
		owner, err := gpuCgroupOwner(string(cgroup), uids)
		if err != nil {
			return result, err
		}
		if owner == o.OldPodUID {
			result.OldGPU++
		} else {
			result.NewGPU++
		}
	}
	return result, nil
}

// This is sampled Pod/GPU evidence only. It does not observe native processes,
// prove continuous non-overlap, perform a rollout, or qualify P08. Probe gaps,
// denied access and uncertain attribution fail rather than becoming zero.
func observeKubernetesGPUReplacement(ctx context.Context, o kubernetesGPUObserver, cadence, maxGap time.Duration, maxSamples int) (int, error) {
	if cadence <= 0 || maxGap < cadence || maxSamples < 3 {
		return 0, errors.New("invalid observation bounds")
	}
	if _, ok := ctx.Deadline(); !ok {
		return 0, errors.New("observation requires a deadline")
	}
	retired := false
	var replacementUID string
	var last time.Time
	for n := 1; n <= maxSamples; n++ {
		s, err := o.sample(ctx)
		now := time.Now()
		if err != nil || ctx.Err() != nil {
			return n, errors.New("replacement sample unavailable")
		}
		if !last.IsZero() && now.Sub(last) > maxGap {
			return n, errors.New("replacement observation gap exceeded")
		}
		last = now
		if s.NewPodUID != "" {
			if replacementUID != "" && replacementUID != s.NewPodUID {
				return n, errors.New("replacement Pod identity changed")
			}
			replacementUID = s.NewPodUID
		}
		if s.OldPods+s.NewPods > 1 || s.OldGPU+s.NewGPU > 1 {
			return n, errors.New("overlapping backend Pod or GPU process observed")
		}
		if n == 1 && (s.OldPods != 1 || s.OldGPU != 1 || s.NewPods != 0 || s.NewGPU != 0) {
			return n, errors.New("initial old GPU resident unavailable")
		}
		if n == 1 && o.armed != nil {
			o.armed()
		}
		// A replacement Pod may exist before it owns the GPU. Still require a
		// sample with neither old ownership nor a new GPU process.
		if n > 1 && s.OldPods == 0 && s.OldGPU == 0 && s.NewGPU == 0 {
			retired = true
		}
		if retired && (s.OldPods != 0 || s.OldGPU != 0) {
			return n, errors.New("old GPU ownership reappeared")
		}
		if s.NewPods+s.NewGPU > 0 && !retired {
			return n, errors.New("replacement preceded observed retirement")
		}
		if retired && s.NewPods == 1 && s.NewGPU == 1 {
			return n, nil
		}
		if n == maxSamples {
			break
		}
		timer := time.NewTimer(cadence)
		select {
		case <-ctx.Done():
			timer.Stop()
			return n, errors.New("replacement observation timed out")
		case <-timer.C:
		}
	}
	return maxSamples, errors.New("replacement was not observed within sample bound")
}

// The target file stays private. An independently authorized actor must start
// the replacement only after the first old-resident sample has been observed;
// this test performs reads only and does not orchestrate the replacement.
func TestP08SampledGPUReplacement(t *testing.T) {
	if os.Getenv("P08_OBSERVE_GPU_REPLACEMENT") != "1" {
		t.Skip("requires an independently authorized replacement observation window")
	}
	file, err := os.Open(os.Getenv("P08_GPU_OBSERVATION_TARGET"))
	if err != nil {
		t.Fatal("private observation selection unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("private observation selection exceeds bound")
	}
	var selection struct {
		Kubeconfig      string `json:"kubeconfig"`
		Context         string `json:"context"`
		Namespace       string `json:"namespace"`
		NamespaceUID    string `json:"namespace_uid"`
		Release         string `json:"release"`
		DriverNamespace string `json:"driver_namespace"`
		DriverPod       string `json:"driver_pod"`
		DriverUID       string `json:"driver_uid"`
		GPUUUID         string `json:"gpu_uuid"`
		OldPodUID       string `json:"old_pod_uid"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selection) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("invalid private observation selection")
	}
	o := kubernetesGPUObserver{Kubeconfig: selection.Kubeconfig, Context: selection.Context, Namespace: selection.Namespace, NamespaceUID: selection.NamespaceUID, Release: selection.Release, DriverNamespace: selection.DriverNamespace, DriverPod: selection.DriverPod, DriverUID: selection.DriverUID, GPUUUID: selection.GPUUUID, OldPodUID: selection.OldPodUID}
	var armedAt time.Time
	o.armed = func() {
		armedAt = time.Now()
		if path := os.Getenv("P08_GPU_OBSERVATION_ARMED"); path != "" {
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal("cannot signal replacement observation readiness")
			}
			if err := file.Close(); err != nil {
				t.Fatal("cannot close replacement observation readiness signal")
			}
		}
		t.Log("read-only replacement observer armed")
	}
	// The selected backend permits 30 minutes for provisioning; include cleanup
	// and startup margin rather than failing a still-valid replacement early.
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	n, err := observeKubernetesGPUReplacement(ctx, o, time.Second, 15*time.Second, 2700)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sampled %d bounded Pod/GPU observations over %s after arming; this is not public downtime, and gaps and serial reads do not prove continuous non-overlap", n, time.Since(armedAt))
}
