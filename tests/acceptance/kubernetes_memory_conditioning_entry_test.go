package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

type p08MemoryCUDASelection struct {
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

func p08ReadMemoryCUDASelection(path string) (p08MemoryCUDASelection, error) {
	var v p08MemoryCUDASelection
	if !filepath.IsAbs(path) {
		return v, errors.New("absolute private CUDA selection required")
	}
	f, err := os.Open(path)
	if err != nil {
		return v, errors.New("private CUDA selection unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return v, errors.New("private CUDA selection must be mode 0600")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return v, errors.New("private CUDA selection unavailable")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF || !filepath.IsAbs(v.Kubeconfig) || v.Context == "" || v.Namespace == "" || v.NamespaceUID == "" || v.Release == "" || v.DriverNamespace == "" || v.DriverPod == "" || v.DriverUID == "" || v.GPUUUID == "" || v.BackendPod == "" || v.BackendPodUID == "" || v.BackendPort < 1 || v.BackendPort > 65535 {
		return p08MemoryCUDASelection{}, errors.New("complete private CUDA selection required")
	}
	return v, nil
}

type p08MemoryEpochEvidence struct {
	Scope                    string                  `json:"scope"`
	Qualification            string                  `json:"qualification"`
	Workload                 string                  `json:"workload"`
	RequestsAttempted        *int                    `json:"requests_attempted"`
	Completion               *residentResourcePoint  `json:"completion"`
	Idle                     []residentResourcePoint `json:"idle"`
	NativeDescendantRSSBytes []uint64                `json:"native_descendant_rss_bytes"`
	FailureStage             string                  `json:"failure_stage,omitempty"`
	ObservationPhase         string                  `json:"observation_phase,omitempty"`
	ObserverStage            string                  `json:"observer_stage,omitempty"`
	ObserverDetail           string                  `json:"observer_detail,omitempty"`
	OperationSeconds         int                     `json:"operation_seconds"`
	FixtureSHA256            string                  `json:"fixture_sha256"`
	APIImage                 string                  `json:"api_image"`
	BackendImage             string                  `json:"backend_image"`
	Limitations              []string                `json:"limitations"`
}

func p08MemoryEvidence(epoch p08ConditioningEpoch, operationSeconds int, fixture, apiImage, backendImage, failure string) p08MemoryEpochEvidence {
	out := p08MemoryEpochEvidence{Scope: "partial_not_full_P08", Qualification: "unknown", Workload: "single_conditioning_epoch_not_frozen", RequestsAttempted: epoch.RequestsAttempted, Idle: epoch.Idle, NativeDescendantRSSBytes: epoch.NativeDescendantRSSBytes, FailureStage: failure, ObservationPhase: epoch.ObservationPhase, ObserverStage: epoch.ObserverStage, ObserverDetail: epoch.ObserverDetail, OperationSeconds: operationSeconds, FixtureSHA256: fixture, APIImage: apiImage, BackendImage: backendImage, Limitations: []string{"mounted configuration and model declaration observed; selected startup overrides checked, but original startup file bytes and later projected-file changes are not independently established", "native mapped model file identity and recurring model I/O are not established", "GC proof, calibrated baseline, full conditioning, measured blocks and terminal phases are unknown", "serial RSS/GPU observations are not continuous peak evidence; native descendants remain separate from the P07 aggregate"}}
	if !epoch.Completion.At.IsZero() {
		point := epoch.Completion
		out.Completion = &point
	}
	return out
}

// Return only a fixed marker, never argv or environment values. The files are
// PID1's startup inputs; later projected-file changes remain unproven.
const p08MemoryStartupProbe = `set -eu
vars=$(tr '\000' '\n' < "$1") || exit 1
args=$(tr '\000' '\n' < "$2") || exit 1
IFS='
'
for v in $vars; do case "$v" in WHISPER_*) exit 1;; esac; done
config=0; next=0
for a in $args; do
 if [ "$next" = 1 ]; then [ "$a" = /etc/whisper/config.json ] || exit 1; next=0; continue; fi
 case "$a" in
  --config) config=$((config+1)); next=1;;
  --config=*|--operation-seconds|--operation-seconds=*|--rate-limit-enabled|--rate-limit-enabled=*|--backend-port|--backend-port=*|--audio-bytes|--audio-bytes=*) exit 1;;
 esac
done
[ "$config" = 1 ] && [ "$next" = 0 ] || exit 1
printf OK
`

func p08MemoryVerifyStartup(ctx context.Context, target p08ResourceTarget, expected p08RepeatedIdentity, model, port string) error {
	before, err := p08RepeatedOwner(ctx, target, model, port)
	if err != nil || before != expected {
		return errors.New("startup identity unavailable")
	}
	data, err := target.command(ctx, "exec", target.APIPod, "-c", target.APIContainer, "--", "sh", "-eu", "-c", p08MemoryStartupProbe, "p08-memory-startup", "/proc/1/environ", "/proc/1/cmdline")
	if err != nil || string(data) != "OK" {
		return errors.New("startup configuration overrides unavailable or present")
	}
	after, err := p08RepeatedOwner(ctx, target, model, port)
	if err != nil || after != expected {
		return errors.New("startup identity unavailable")
	}
	return nil
}

func p08MemoryCandidateFence(ctx context.Context, target p08ResourceTarget, client *PublicClient, refs, images [2]string, expected p08HeapOwner, model, port string) error {
	pods, err := p08PublicPodImages(ctx, target)
	if err != nil || pods != refs {
		return errors.New("candidate Pod references unavailable")
	}
	before, err := p08ReadHeapOwner(ctx, target, model, port)
	if err != nil || before != expected || before.Containers[0].Image != images[0] || before.Containers[1].Image != images[1] || p08PublicRuntimeMatchesCandidate(refs, before) != nil {
		return errors.New("candidate runtime identity unavailable")
	}
	if p08VerifyHTTPForward(ctx, client, func(ctx context.Context) ([]byte, error) {
		return target.command(ctx, "logs", target.APIPod, "-c", target.APIContainer, "--since=30s", "--limit-bytes=1048576")
	}) != nil {
		return errors.New("public forward correlation unavailable")
	}
	after, err := p08ReadHeapOwner(ctx, target, model, port)
	if err != nil || after != before {
		return errors.New("candidate runtime identity changed")
	}
	return nil
}

func TestP08MemoryConditioningEpochPartial(t *testing.T) {
	if os.Getenv("P08_MEMORY_CONDITIONING_EPOCH") != "1" {
		t.Skip("requires an authorized existing Kubernetes pair and GPU observation")
	}
	if os.Getenv("WHISPER_TEST_TARGET") != "kubernetes" {
		t.Fatal("explicit Kubernetes target required")
	}
	publishedPath := os.Getenv("P08_PUBLISHED_SELECTION")
	if !filepath.IsAbs(publishedPath) {
		t.Fatal("absolute published selection required")
	}
	published, err := loadPublishedSelection(publishedPath)
	if err != nil {
		t.Fatal("published selection unavailable")
	}
	selection, err := readP08SharedHTTPSelection(os.Getenv("P08_SHARED_HTTP_SELECTION"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := readP08ResourceTarget(selection.ResourceTarget)
	if err != nil {
		t.Fatal(err)
	}
	cuda, err := p08ReadMemoryCUDASelection(os.Getenv("P08_CUDA_ACTIVITY_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	address, model, port, dir := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_SHARED_BACKEND_PORT"), os.Getenv("P08_DEVELOPMENT_EVIDENCE")
	portNumber, err := strconv.Atoi(port)
	info, statErr := os.Stat(dir)
	if !isLoopbackAPIRoot(address) || model == "" || err != nil || portNumber < 1 || portNumber > 65535 || !filepath.IsAbs(dir) || statErr != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("explicit loopback, model, port and private evidence directory required")
	}
	if cuda.Kubeconfig != target.Kubeconfig || cuda.Context != target.Context || cuda.Namespace != target.Namespace || cuda.NamespaceUID != target.NamespaceUID || cuda.Release != target.Release || cuda.BackendPod != target.BackendPod || cuda.BackendPodUID != target.BackendUID || cuda.BackendPort != portNumber {
		t.Fatal("CUDA and resource selections do not identify the same backend")
	}
	setup, cancelSetup := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelSetup()
	initial, err := p08ReadHeapOwner(setup, target, model, port)
	if err != nil {
		t.Fatal("resident identity unavailable")
	}
	refs := [2]string{published.APIImage, published.BackendImage}
	images := [2]string{selection.APIImageID, selection.BackendImageID}
	if p08PublicRuntimeMatchesCandidate(refs, initial) != nil || initial.Containers[0].Image != images[0] || initial.Containers[1].Image != images[1] {
		t.Fatal("published runtime identity unavailable")
	}
	if p08MemoryVerifyStartup(setup, target, initial.Process, model, port) != nil {
		t.Fatal("startup override boundary unavailable")
	}
	data, err := target.command(setup, "exec", target.APIPod, "-c", target.APIContainer, "--", "cat", "/etc/whisper/config.json")
	if err != nil || len(data) == 0 || len(data) >= 1<<20 {
		t.Fatal("mounted configuration unavailable")
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if os.WriteFile(configPath, data, 0600) != nil {
		t.Fatal("private configuration staging failed")
	}
	settings, err := config.Load([]string{"--config", configPath}, []string{})
	if err != nil {
		t.Fatal("mounted configuration invalid")
	}
	fingerprint, err := config.CanonicalFingerprint(settings)
	if err != nil || fingerprint != initial.Process.fingerprint || settings.Models.Served != model || settings.Backend.Port != portNumber || settings.API.RateLimit.Enabled {
		t.Fatal("mounted configuration does not match required model, port or rate policy")
	}
	audio, err := readPublicCaseFixture(published.FixtureDirectory, "jfk.wav")
	if err != nil || int64(len(audio)) > settings.Limits.AudioBytes {
		t.Fatal("bounded pinned public fixture unavailable")
	}
	client, err := NewPublicClient(address, time.Duration(settings.Timeouts.OperationSeconds)*time.Second, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	device := kubernetesGPUObserver{Kubeconfig: cuda.Kubeconfig, Context: cuda.Context, Namespace: cuda.Namespace, NamespaceUID: cuda.NamespaceUID, Release: cuda.Release, DriverNamespace: cuda.DriverNamespace, DriverPod: cuda.DriverPod, DriverUID: cuda.DriverUID, GPUUUID: cuda.GPUUUID, OldPodUID: cuda.BackendPodUID}
	observer := p08MemoryConditioningObserver{target: target, gpu: kubernetesCUDAActivity{observer: device, backendPod: cuda.BackendPod, backendPort: cuda.BackendPort}, initial: initial.Process, model: model, port: port}
	ctx, cancel := context.WithTimeout(t.Context(), time.Duration(settings.Timeouts.OperationSeconds)*time.Second)
	defer cancel()
	var epoch p08ConditioningEpoch
	failure := ""
	if p08MemoryCandidateFence(ctx, target, client, refs, images, initial, model, port) != nil {
		failure = "before_fence"
	} else {
		epoch, err = p08MemoryConditioningEpoch(ctx, observer, client, audio)
		if err != nil || epoch.RequestsAttempted == nil || *epoch.RequestsAttempted != residentConditioningBlockRequests || epoch.Completion.At.IsZero() || len(epoch.Idle) != 5 || len(epoch.NativeDescendantRSSBytes) != 6 {
			failure = "epoch"
		}
		if p08MemoryCandidateFence(ctx, target, client, refs, images, initial, model, port) != nil {
			if failure == "" {
				failure = "after_fence"
			} else {
				failure = "epoch_and_after_fence"
			}
		}
	}
	report := p08MemoryEvidence(epoch, settings.Timeouts.OperationSeconds, acceptanceSHA256(audio), refs[0], refs[1], failure)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal("partial evidence encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-memory-conditioning-epoch-partial.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("partial evidence creation failed")
	}
	_, werr := f.Write(append(encoded, '\n'))
	cerr := f.Close()
	if errors.Join(werr, cerr) != nil {
		t.Fatal("partial evidence write failed")
	}
	if failure != "" {
		t.Fatal("partial conditioning epoch incomplete; see sanitized stage")
	}
	t.Skip("partial resource epoch recorded; model-file identity, GC, baseline and full P08 qualification remain unknown")
}
