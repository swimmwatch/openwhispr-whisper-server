package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// The parent owns the deployment, port forward, target authorization and test window.
// This is public steady-state timing only, not P08 or native memory qualification.
type kubernetesPublicPerformanceEvidence struct {
	Scope                 string             `json:"scope"`
	FixtureSHA256         string             `json:"fixture_sha256"`
	DeclaredSamples       int                `json:"declared_samples"`
	Concurrency           int                `json:"concurrency"`
	SpeechPreflightPassed bool               `json:"speech_preflight_passed"`
	Summary               MeasurementSummary `json:"summary"`
	Complete              bool               `json:"complete"`
	Limitation            string             `json:"limitation"`
}

func measureKubernetesPublicPerformance(ctx context.Context, client *PublicClient, audio []byte, model string, samples int) (kubernetesPublicPerformanceEvidence, error) {
	evidence := kubernetesPublicPerformanceEvidence{
		Scope: "partial_public_steady_state_not_full_P08", FixtureSHA256: publicJFKIdentity,
		DeclaredSamples: samples, Concurrency: 1,
		Limitation: "preflight checks identified speech; steady samples check response status and decoding, not transcript content; CUDA activity, memory, queue and backend timing are not measured",
	}
	if client == nil || len(audio) == 0 || model == "" || samples < 30 || samples > 1000 {
		return evidence, errors.New("invalid public performance workload")
	}
	workload, err := measureSharedResidentWorkload(ctx, client, audio, model, samples)
	evidence.SpeechPreflightPassed = workload.PreflightPassed
	evidence.Summary = workload.Summary
	evidence.Complete = err == nil
	if !evidence.Complete {
		return evidence, errors.New("public steady-state measurement incomplete")
	}
	return evidence, nil
}

func writeKubernetesPublicPerformance(dir string, evidence kubernetesPublicPerformanceEvidence) error {
	if !filepath.IsAbs(dir) {
		return errors.New("absolute private evidence directory required")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("existing private evidence directory with mode 0700 required")
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return errors.New("cannot encode performance evidence")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-public-steady-state.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private performance evidence")
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write private performance evidence")
	}
	return nil
}

func readKubernetesPerformanceTarget(path string) (developmentQueueTarget, error) {
	var target developmentQueueTarget
	if !filepath.IsAbs(path) {
		return target, errors.New("absolute private target selection required")
	}
	f, err := os.Open(path)
	if err != nil {
		return target, errors.New("private target selection unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return target, errors.New("private target selection must be a regular file with mode 0600")
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if err != nil || len(data) > 64<<10 {
		return target, errors.New("private target selection unavailable or too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&target) != nil || d.Decode(new(any)) != io.EOF || target.Kubeconfig == "" || target.Context == "" || target.Namespace == "" || target.NamespaceUID == "" || target.Release == "" || target.APIPod == "" || target.BackendPod == "" {
		return target, errors.New("complete private target selection required")
	}
	return target, nil
}

func TestP08PublicSteadyState(t *testing.T) {
	if os.Getenv("P08_PUBLIC_STEADY_STATE") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes performance window")
	}
	address := os.Getenv("P08_DEVELOPMENT_API_URL")
	u, err := url.Parse(address)
	if err != nil || u.ForceQuery || !isLoopbackAPIRoot(address) {
		t.Fatal("explicit credential-free loopback API root required")
	}
	model, fixtures, dir := os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES"), os.Getenv("P08_PERFORMANCE_EVIDENCE_DIR")
	samples, err := strconv.Atoi(os.Getenv("P08_PERFORMANCE_SAMPLES"))
	if model == "" || !filepath.IsAbs(fixtures) || !filepath.IsAbs(dir) || err != nil || samples < 30 || samples > 1000 {
		t.Fatal("explicit model, absolute fixture and evidence directories and 30 to 1000 samples required")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("existing private evidence directory with mode 0700 required")
	}
	target, err := readKubernetesPerformanceTarget(os.Getenv("P08_DEVELOPMENT_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	audio, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("canonical workload bounds unavailable")
	}
	timeout := time.Duration(settings.Timeouts.OperationSeconds+settings.Timeouts.CleanupSeconds+5) * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), time.Duration(samples+1)*timeout+30*time.Second)
	defer cancel()
	if target.verify(ctx) != nil {
		t.Fatal("development target ownership verification failed")
	}
	port := strconv.Itoa(settings.Backend.Port)
	before, err := target.health(ctx, port)
	if err != nil || before.HTTPStatus != http.StatusOK || before.Status != "ready" || !before.CUDAReady || before.Model != model || before.ConfigurationFingerprint == "" || before.ProcessGeneration == "" {
		t.Fatal("selected target is not a ready CUDA resident of the selected model")
	}
	client, err := NewPublicClient(address, timeout, 1<<20)
	if err != nil {
		t.Fatal("cannot construct public workload client")
	}
	defer client.Close()
	evidence, measureErr := measureKubernetesPublicPerformance(ctx, client, audio, model, samples)
	after, healthErr := target.health(ctx, port)
	identityErr := target.verify(ctx)
	stable := healthErr == nil && identityErr == nil && after.HTTPStatus == http.StatusOK && after.Status == "ready" && after.CUDAReady && after.Model == model && after.ConfigurationFingerprint == before.ConfigurationFingerprint && after.ProcessGeneration == before.ProcessGeneration
	if !stable {
		evidence.Complete = false
	}
	if err := writeKubernetesPublicPerformance(dir, evidence); err != nil {
		t.Fatal(err)
	}
	if measureErr != nil || !stable {
		t.Fatal("partial public performance measurement or resident identity verification failed")
	}
	t.Log("public steady-state measurement complete; full P08 and GPU activity remain unproven")
}
