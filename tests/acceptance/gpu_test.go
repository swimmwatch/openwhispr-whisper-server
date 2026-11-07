package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// This is a resident baseline, not the complete P07 qualification suite. In
// particular it does not claim model-I/O tracing, client interoperability,
// cancellation/recovery or the calibrated resource envelope from Q-RESIDENT.
func TestDockerGPUResidentBaseline(t *testing.T) {
	if os.Getenv("P07_GPU_TESTS") != "1" {
		t.Skip("real candidate baseline requires explicit P07_GPU_TESTS=1 and fixture/inventory inputs")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	inventoryBytes, err := os.ReadFile(os.Getenv("P07_RELEASE_INVENTORY"))
	if err != nil {
		t.Fatal("selected candidate inventory is unavailable")
	}
	var inventory struct {
		Source string            `json:"candidate_source_sha"`
		Images map[string]string `json:"image_manifest_digests"`
	}
	if json.Unmarshal(inventoryBytes, &inventory) != nil || len(inventory.Source) != 40 {
		t.Fatal("selected candidate inventory is invalid")
	}
	file, err := os.Open(os.Getenv("P07_AUDIO_FIXTURE"))
	if err != nil {
		t.Fatal("public audio fixture is unavailable")
	}
	defer file.Close()
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("canonical application settings are unavailable")
	}
	audio, err := io.ReadAll(io.LimitReader(file, settings.Limits.AudioBytes+1))
	identity := sha256.Sum256(audio)
	if err != nil || int64(len(audio)) > settings.Limits.AudioBytes || hex.EncodeToString(identity[:]) != os.Getenv("P07_AUDIO_SHA256") {
		t.Fatal("public audio fixture does not match the selected bounded content identity")
	}
	// The initial baseline uses the pinned eleven-second upstream JFK fixture.
	if os.Getenv("P07_AUDIO_SHA256") != "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e" {
		t.Fatal("this baseline requires the identified eleven-second public JFK fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	gpu := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,driver_version,memory.total", "--format=csv,noheader")
	gpuIdentity, err := gpu.Output()
	if err != nil || !strings.Contains(string(gpuIdentity), "RTX 5070 Ti Laptop") {
		t.Fatal("the required local GPU/driver is unavailable; enumeration is not inference evidence")
	}
	workdir, err := os.MkdirTemp(filepath.Join(root, ".local", "p07", "run"), "baseline-")
	if err != nil {
		t.Fatal("cannot create private baseline evidence directory")
	}
	prepared := filepath.Join(workdir, "prepared")
	if os.Mkdir(prepared, 0755) != nil {
		t.Fatal("cannot create isolated prepared mount")
	}
	// Explicit queue/resource workload override; separate enabled-limiter tests
	// remain required for complete acceptance. No server implementation is changed.
	settings.API.RateLimit.Enabled = false
	configuration, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workdir, "config.json")
	if os.WriteFile(configPath, configuration, 0644) != nil {
		t.Fatal("cannot write isolated application configuration")
	}
	project := "whisper-p07-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	target, err := NewDockerTarget(DockerOptions{
		Root: root, Project: project, ConfigFile: configPath, PreparedDir: prepared,
		APIImage:     "ghcr.io/swimmwatch/openwhispr-whisper-server-api@" + inventory.Images["api"],
		BackendImage: "ghcr.io/swimmwatch/openwhispr-whisper-server-backend@" + inventory.Images["backend"],
		GPUDevice:    "0", HostPort: 0, APIPort: settings.API.Port, BackendPort: settings.Backend.Port,
		CommandTimeout: 2 * time.Minute, StopTimeoutSeconds: settings.Timeouts.ShutdownSeconds + settings.Timeouts.CleanupSeconds + 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stop()
		for _, role := range []string{"api", "backend"} {
			if logs, err := target.Logs(cleanup, role); err == nil {
				_ = os.WriteFile(filepath.Join(workdir, role+".log"), logs, 0600)
			}
		}
		if err := target.Down(cleanup); err != nil {
			t.Error(err)
		}
	})
	started := time.Now()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	address, err := target.APIAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probe := &http.Client{Timeout: 2 * time.Second}
	defer probe.CloseIdleConnections()
	deadline := time.Now().Add(time.Duration(settings.Timeouts.ProvisioningSeconds) * time.Second)
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, address+"/health/ready", nil)
		response, probeErr := probe.Do(request)
		ready := probeErr == nil && response.StatusCode == http.StatusOK
		if response != nil {
			_ = response.Body.Close()
		}
		if ready {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			// Raw logs are kept private for failure diagnosis, not copied to t.Fatal.
			for _, role := range []string{"api", "backend"} {
				logs, logErr := target.Logs(ctx, role)
				if logErr == nil {
					_ = os.WriteFile(filepath.Join(workdir, role+".log"), logs, 0600)
				}
			}
			t.Fatal("candidate did not become ready within its provisioning budget")
		}
		select {
		case <-ctx.Done():
			t.Fatal("baseline cancelled while waiting for readiness")
		case <-time.After(time.Second):
		}
	}
	startup := time.Since(started)
	before, err := target.Processes(ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	var nativePID int
	for _, process := range before {
		if process.Command == "whisper-server" {
			if nativePID != 0 {
				t.Fatal("overlapping native workers observed")
			}
			nativePID = process.PID
		}
	}
	if nativePID == 0 {
		t.Fatal("native resident worker was not observed")
	}
	for _, role := range []string{"api", "backend"} {
		state, err := target.Inspect(ctx, role)
		if err != nil || !state.Running || state.User != "10001:10001" || !state.ReadonlyRootFS {
			t.Fatal("actual candidate violates execution identity or filesystem boundaries")
		}
		if role == "api" && len(state.DeviceRequests) != 0 || role == "backend" && (len(state.DeviceRequests) != 1 || len(state.PortBindings) != 0) {
			t.Fatal("GPU or private listener publication boundary changed")
		}
	}
	client, err := NewPublicClient(address, time.Duration(settings.Timeouts.OperationSeconds+5)*time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reply, err := client.Transcribe(ctx, audio, "jfk.wav", []FormField{{"model", settings.Models.Served}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || strings.TrimSpace(reply.Text) == "" || !identifiedPublicSpeech(reply.Text) {
		t.Fatalf("actual public candidate speech mismatch: status=%d transport_error=%t speech=%+v", reply.Status, err != nil, observePublicSpeech(reply.Text))
	}
	summary, err := MeasurePublicSteadyState(ctx, client, TranscriptionWorkload{
		Audio: audio, Filename: "jfk.wav", Fields: []FormField{{"model", settings.Models.Served}, {"language", "en"}, {"temperature", "0"}},
		AudioDuration: 11 * time.Second, Samples: 60,
	})
	if err != nil || summary.Successes != 60 || summary.Errors != 0 {
		t.Fatalf("actual repeated public workload incomplete: successes=%d errors=%d summarization_error=%t", summary.Successes, summary.Errors, err != nil)
	}
	after, err := target.Processes(ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, process := range after {
		if process.Command == "whisper-server" {
			if process.PID != nativePID || found {
				t.Fatal("successful requests replaced or overlapped the resident native worker")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("resident worker disappeared after successful requests")
	}
	summary.Startup = &startup
	report, err := json.MarshalIndent(map[string]any{
		"scope": "resident_baseline_only_not_complete_P07", "candidate_source_sha": inventory.Source,
		"image_manifest_digests": inventory.Images, "audio_sha256": hex.EncodeToString(identity[:]),
		"model": settings.Models.Served, "samples": summary, "rate_limit_enabled": false,
	}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(workdir, "baseline.json"), report, 0600) != nil {
		t.Fatal("cannot preserve sanitized baseline observations")
	}
	t.Log("public candidate transcription and sixty-request resident process baseline passed; full P07 remains independently required")
}
