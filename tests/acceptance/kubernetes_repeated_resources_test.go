package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// Identity is kept only in memory. A missing identity is never a measured zero.
type p08RepeatedIdentity struct {
	containers                                     [2]p08ContainerIdentity
	apiStart, backendStart, nativePID, nativeStart uint64
	generation, fingerprint                        string
}

func p08SamplingOwner(ctx context.Context, target p08ResourceTarget, model, port string, expected p08RepeatedIdentity) (p08RepeatedIdentity, error) {
	value, err := p08ReadRepeatedOwner(ctx, target, model, port, true)
	if err != nil || value != expected {
		return p08RepeatedIdentity{}, errors.New("sampling identity unavailable")
	}
	return value, nil
}

func p08RepeatedOwner(ctx context.Context, target p08ResourceTarget, model, port string) (p08RepeatedIdentity, error) {
	return p08ReadRepeatedOwner(ctx, target, model, port, false)
}

func p08ReadRepeatedOwner(ctx context.Context, target p08ResourceTarget, model, port string, allowBusy bool) (p08RepeatedIdentity, error) {
	var out p08RepeatedIdentity
	before, err := target.identity(ctx)
	if err != nil {
		return out, errors.New("resource identity unavailable")
	}
	probe := func(pod, container, role string) ([]byte, error) {
		return target.command(ctx, "exec", pod, "-c", container, "--", "sh", "-eu", "-c", p08ProcessIdentityProbe, "p08-identity", role)
	}
	api, err := probe(target.APIPod, target.APIContainer, "api")
	if err != nil {
		return out, errors.New("API process identity unavailable")
	}
	backend, err := probe(target.BackendPod, target.BackendContainer, "backend")
	if err != nil {
		return out, errors.New("backend process identity unavailable")
	}
	var apiPID, apiNative uint64
	if parseP08ProcessIdentity(api, &out.apiStart, &apiPID, &apiNative) != nil || apiPID != 0 || apiNative != 0 || parseP08ProcessIdentity(backend, &out.backendStart, &out.nativePID, &out.nativeStart) != nil || out.nativePID <= 1 || out.nativeStart == 0 {
		return p08RepeatedIdentity{}, errors.New("process identity unavailable")
	}
	// Use the same selected command boundary, including in CPU tests.
	data, err := target.command(ctx, "exec", target.BackendPod, "--", "curl", "--silent", "--show-error", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "2", "--write-out", "\n%{http_code}", "http://127.0.0.1:"+port+"/health")
	health, parseErr := parsePrivateHealth(data)
	if err != nil || parseErr != nil || health == nil || health.HTTPStatus != http.StatusOK || (health.Status != "ready" && !(allowBusy && health.Status == "busy")) || !health.CUDAReady || health.Model != model || health.ProcessGeneration == "" || health.ConfigurationFingerprint == "" {
		return p08RepeatedIdentity{}, errors.New("resident health identity unavailable")
	}
	after, err := target.identity(ctx)
	if err != nil || before != after {
		return p08RepeatedIdentity{}, errors.New("resource owner changed")
	}
	out.containers = before
	out.generation = health.ProcessGeneration
	out.fingerprint = health.ConfigurationFingerprint
	return out, nil
}

type p08RepeatedResources struct {
	Scope         string                `json:"scope"`
	FixtureSHA256 string                `json:"fixture_sha256"`
	Requests      int                   `json:"requests"`
	Snapshots     []p08ResourceEvidence `json:"snapshots"`
	Decoder       *p08DecoderSampling   `json:"decoder_sampling,omitempty"`
	Limitation    string                `json:"limitation"`
}

// owner brackets every snapshot after a completed operation. Missing process,
// scratch, GPU or heap evidence is never interpreted as a measured zero.
func measureP08RepeatedResources(ctx context.Context, client *PublicClient, audio []byte, model string, count int, owner func(context.Context) (p08RepeatedIdentity, error), observe func(context.Context) (p08ResourceEvidence, error)) (p08RepeatedResources, error) {
	digest := sha256.Sum256(audio)
	result := p08RepeatedResources{Scope: "partial_not_memory_stability", FixtureSHA256: hex.EncodeToString(digest[:]), Limitation: "serial RSS, FD and API scratch observations; native-descendant RSS is separate; decoder RSS is separately sampled when present; no complete backend scratch, Go heap calibration, GPU allocation or calibrated P07 memory envelope"}
	if client == nil || len(audio) == 0 || model == "" || count < 30 || count > 1000 || owner == nil || observe == nil {
		return result, errors.New("invalid repeated resource workload")
	}
	initial, err := owner(ctx)
	if err != nil || initial.containers[0].ID == "" || initial.containers[1].ID == "" || initial.apiStart == 0 || initial.backendStart == 0 || initial.nativePID <= 1 || initial.nativeStart == 0 || initial.generation == "" || initial.fingerprint == "" {
		return result, errors.New("initial identity unavailable")
	}
	snapshot := func() error {
		before, e := owner(ctx)
		if e != nil || before != initial {
			return errors.New("resource owner changed or unavailable")
		}
		point, e := observe(ctx)
		if e != nil {
			return errors.New("resource snapshot unavailable")
		}
		after, e := owner(ctx)
		if e != nil || after != initial {
			return errors.New("resource owner changed or unavailable")
		}
		result.Snapshots = append(result.Snapshots, point)
		if point.API.RSSBytes == nil || point.Backend.RSSBytes == nil || point.Backend.FDCount == nil || point.NativeDescendantRSSBytes == nil || point.APIDeletedScratchFDs == nil || point.APIDeletedScratchBytes == nil || *point.APIDeletedScratchFDs != 0 || *point.APIDeletedScratchBytes != 0 || releasedScratchResources(point.API) != nil {
			return errors.New("resource counters unavailable or API scratch retained")
		}
		return nil
	}
	if err = snapshot(); err != nil {
		return result, err
	}
	fields := []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}}
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return result, errors.New("repeated workload cancelled")
		}
		reply, e := client.Transcribe(ctx, audio, "jfk.wav", fields)
		result.Requests++
		if e != nil || reply.Status != http.StatusOK {
			return result, errors.New("public request failed")
		}
		if (i+1)%10 == 0 || i+1 == count {
			if err = snapshot(); err != nil {
				return result, err
			}
		}
	}
	final, err := owner(ctx)
	if err != nil || final != initial {
		return result, errors.New("final identity unavailable or changed")
	}
	return result, nil
}

func TestP08RepeatedResources(t *testing.T) {
	if os.Getenv("P08_REPEATED_RESOURCES") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes workload")
	}
	address, model, fixtures, dir := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES"), os.Getenv("P08_RESOURCE_EVIDENCE_DIR")
	if !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(fixtures) || !filepath.IsAbs(dir) {
		t.Fatal("explicit loopback API, model and absolute fixture/evidence directories required")
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
	timeout := time.Duration(settings.Timeouts.OperationSeconds+settings.Timeouts.CleanupSeconds+5) * time.Second
	client, err := NewPublicClient(address, timeout, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 31*timeout+10*time.Minute)
	defer cancel()
	port := strconv.Itoa(settings.Backend.Port)
	initial, err := p08RepeatedOwner(ctx, target, model, port)
	if err != nil {
		t.Fatal("initial resident identity unavailable")
	}
	// The pinned fixture preflight validates speech before the measured requests.
	reply, err := client.Transcribe(ctx, audio, "jfk.wav", []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
	if checkPublicCase(reply, err, http.StatusOK, "") != nil {
		t.Fatal("public speech preflight failed")
	}
	owner := func(ctx context.Context) (p08RepeatedIdentity, error) {
		current, err := p08RepeatedOwner(ctx, target, model, port)
		if err != nil || current != initial {
			return p08RepeatedIdentity{}, errors.New("initial resident identity changed or unavailable")
		}
		return current, nil
	}
	evidence, decoder, measurementErr := p08MeasureDecoderDuringWork(ctx, func(sampleCtx context.Context) (*uint64, error) {
		return p08MemoryDecoderRSS(sampleCtx, target, initial, func(ctx context.Context) (p08RepeatedIdentity, error) {
			return p08SamplingOwner(ctx, target, model, port, initial)
		})
	}, func(workCtx context.Context) (p08RepeatedResources, error) {
		return measureP08RepeatedResources(workCtx, client, audio, model, 30, owner, target.observe)
	})
	evidence.Decoder = &decoder
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("resource evidence encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-repeated-resources.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("resource evidence creation failed")
	}
	_, writeErr := f.Write(append(bytes.TrimSpace(data), '\n'))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("resource evidence write failed")
	}
	if measurementErr != nil {
		t.Fatal(measurementErr)
	}
	t.Log("partial repeated resource observations recorded; memory stability not established")
}
