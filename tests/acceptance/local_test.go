package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// Local verification is deliberately a separate entry point and receipt schema.
// It cannot consume a release inventory or qualify the published candidate.
type localBuildReceipt struct {
	Scope                string `json:"scope"`
	RuntimeInputIdentity string `json:"runtime_input_identity"`
	APIImage             string `json:"api_image"`
	BackendImage         string `json:"backend_image"`
	APIID                string `json:"api_id"`
	BackendID            string `json:"backend_id"`
	BaseFile             string `json:"base_file"`
	TinyFile             string `json:"tiny_file"`
	FixtureDirectory     string `json:"fixture_directory"`
	RegistryPush         bool   `json:"registry_push"`
	ReleaseQualified     bool   `json:"release_qualified"`
}

type acceptanceEnvironment struct {
	ctx       context.Context
	target    *DockerTarget
	client    *PublicClient
	settings  config.Settings
	selection acceptanceSelection
	testName  string
	root      string
	workdir   string
	prepared  string
	config    string
	address   string
	audio     []byte
	startup   time.Duration
}

func openLocalEnvironment(t *testing.T, configure func(*config.Settings)) *acceptanceEnvironment {
	t.Helper()
	if os.Getenv("P07_LOCAL_TESTS") != "1" {
		t.Skip("local GPU checks require explicit P07_LOCAL_TESTS=1 and a local-only build receipt")
	}
	file, err := os.Open(os.Getenv("P07_LOCAL_RECEIPT"))
	if err != nil {
		t.Fatal("explicit local build receipt is unavailable")
	}
	bytes, readErr := io.ReadAll(io.LimitReader(file, 1<<20))
	closeErr := file.Close()
	var receipt localBuildReceipt
	if readErr != nil || closeErr != nil || len(bytes) == 1<<20 || json.Unmarshal(bytes, &receipt) != nil || receipt.Scope != "local_validation_not_release" || receipt.RegistryPush || receipt.ReleaseQualified || receipt.RuntimeInputIdentity == "" {
		t.Fatal("local verification requires a bounded, explicitly non-release receipt")
	}
	return openAcceptanceEnvironment(t, acceptanceSelection{
		Scope: localAcceptanceScope, RuntimeInputIdentity: receipt.RuntimeInputIdentity,
		APIImage: receipt.APIImage, BackendImage: receipt.BackendImage,
		APIID: receipt.APIID, BackendID: receipt.BackendID,
		BaseFile: receipt.BaseFile, TinyFile: receipt.TinyFile, FixtureDirectory: receipt.FixtureDirectory,
	}, configure)
}

func openAcceptanceEnvironment(t *testing.T, selection acceptanceSelection, configure func(*config.Settings)) *acceptanceEnvironment {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("canonical default settings could not be loaded")
	}
	runRoot := filepath.Join(root, ".local", "p07", "run")
	if os.MkdirAll(runRoot, 0700) != nil {
		t.Fatal("cannot create private acceptance output directory")
	}
	runPrefix, projectPrefix := "local-", "whisper-p07-local-"
	if selection.Scope == publishedAcceptanceScope {
		runPrefix, projectPrefix = "published-", "whisper-p07-published-"
	}
	workdir, err := os.MkdirTemp(runRoot, runPrefix)
	if err != nil {
		t.Fatal("cannot create acceptance evidence directory")
	}
	prepared := filepath.Join(workdir, "prepared")
	if os.Mkdir(prepared, 0755) != nil || os.Chmod(prepared, 0755) != nil {
		t.Fatal("cannot create test-owned prepared directory")
	}
	base := settings.Models.Catalog[0]
	copyPreparedModel(t, selection.BaseFile, filepath.Join(prepared, "base.bin"), base.Source)
	base.Source.Kind, base.Source.URL, base.Source.Path = "prepared", "", "/prepared/base.bin"
	settings.Models.Catalog = []config.Model{base}
	settings.Models.DownloadSet = []string{}
	settings.API.RateLimit.Enabled = false
	settings.Logging.Level = "debug"
	if configure != nil {
		configure(&settings)
	}
	if err := settings.Validate(); err != nil {
		t.Fatal("selected acceptance scenario settings are invalid")
	}
	configuration, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workdir, "config.json")
	if os.WriteFile(configPath, configuration, 0644) != nil || os.Chmod(configPath, 0644) != nil {
		t.Fatal("cannot write isolated acceptance scenario configuration")
	}
	if _, err := config.Load([]string{"--config", configPath}, []string{}); err != nil {
		t.Fatal("generated acceptance scenario configuration is rejected by the actual configuration loader")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	t.Cleanup(cancel)
	target, err := newSelectedDockerTarget(selection, DockerOptions{
		Root: root, Project: projectPrefix + strconv.FormatInt(time.Now().UnixNano(), 10),
		ConfigFile: configPath, PreparedDir: prepared, APIImage: selection.APIImage, BackendImage: selection.BackendImage,
		GPUDevice: "0", APIPort: settings.API.Port, BackendPort: settings.Backend.Port,
		CommandTimeout: 2 * time.Minute, StopTimeoutSeconds: settings.Timeouts.ShutdownSeconds + settings.Timeouts.CleanupSeconds + 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	environment := &acceptanceEnvironment{ctx: ctx, target: target, settings: settings, selection: selection, testName: t.Name(), root: root, workdir: workdir, prepared: prepared, config: configPath}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stop()
		if selection.Scope == publishedAcceptanceScope && target.owned {
			if err := recordPublishedBinding(cleanup, environment); err != nil {
				t.Error("final candidate binding failed", err)
			}
		}
		for _, role := range []string{"api", "backend"} {
			if logs, err := target.Logs(cleanup, role); err == nil {
				_ = os.WriteFile(filepath.Join(workdir, role+".log"), logs, 0600)
			}
		}
		if err := target.Down(cleanup); err != nil {
			t.Error(err)
		}
	})
	environment.audio = readLocalAudio(t, filepath.Join(selection.FixtureDirectory, "jfk.wav"), settings.Limits.AudioBytes)
	identity := sha256.Sum256(environment.audio)
	if hex.EncodeToString(identity[:]) != "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e" {
		t.Fatal("real GPU checks require the pinned public JFK fixture")
	}
	if selection.Scope == publishedAcceptanceScope {
		ids, err := selectedPublishedImageIDs(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		environment.selection.APIID, environment.selection.BackendID = ids["api"], ids["backend"]
	}
	started := time.Now()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	environment.address, err = target.APIAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitLocalReady(ctx, target, environment.address, time.Duration(settings.Timeouts.ProvisioningSeconds)*time.Second); err != nil {
		t.Fatal(err)
	}
	environment.startup = time.Since(started)
	for role, expected := range map[string]string{"api": environment.selection.APIID, "backend": environment.selection.BackendID} {
		actual, err := target.Inspect(ctx, role)
		if err != nil || actual.Image != expected || !actual.Running || !actual.ReadonlyRootFS || actual.User != "10001:10001" {
			t.Fatal("running selected component does not match selected immutable image or execution boundaries")
		}
	}
	environment.client, err = NewPublicClient(environment.address, time.Duration(settings.Timeouts.OperationSeconds+5)*time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(environment.client.Close)
	if selection.Scope == publishedAcceptanceScope {
		if err := recordPublishedBinding(ctx, environment); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("selected acceptance slice only; full GPU qualification is not established")
	return environment
}

func copyPreparedModel(t *testing.T, source, destination string, expected config.Source) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal("verified public model fixture is unavailable")
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal("cannot create owned prepared model copy")
	}
	// The model is mounted read-only for container UID 10001, not the creating
	// host UID. Creation mode alone is masked by an operator's restrictive umask.
	if err := output.Chmod(0644); err != nil {
		_ = output.Close()
		t.Fatal("cannot grant readonly prepared model access to the separate container UID")
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, expected.ExpectedBytes+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written != expected.ExpectedBytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		t.Fatal("owned prepared model copy does not match public model content identity")
	}
}

func readLocalAudio(t *testing.T, path string, limit int64) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("declared public audio fixture is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		t.Fatal("public fixture exceeds its declared input bound")
	}
	return data
}

func waitLocalReady(ctx context.Context, target *DockerTarget, address string, budget time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(budget)
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/health/ready", nil)
		if err != nil {
			return errors.New("invalid explicit local endpoint")
		}
		response, err := client.Do(request)
		ready := err == nil && response.StatusCode == http.StatusOK
		if response != nil {
			_ = response.Body.Close()
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return errors.New("local target did not become ready within its declared budget")
		}
		for _, role := range []string{"api", "backend"} {
			state, err := target.Inspect(ctx, role)
			if err != nil || !state.Running {
				return errors.New("local component exited before readiness; inspect owned private startup logs")
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("local readiness wait cancelled")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestLocalResidentBaseline(t *testing.T) {
	runResidentBaseline(t, openLocalEnvironment(t, nil))
}

func runResidentBaseline(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	before, err := e.target.Processes(e.ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	native := 0
	for _, process := range before {
		if process.Command == "whisper-server" {
			if native != 0 {
				t.Fatal("more than one native worker is resident")
			}
			native = process.PID
		}
	}
	if native == 0 {
		t.Fatal("no native worker was observed")
	}
	reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || strings.TrimSpace(reply.Text) == "" || !identifiedPublicSpeech(reply.Text) {
		t.Fatalf("local speech transcription failed: status=%d transport_error=%t speech=%+v", reply.Status, err != nil, observePublicSpeech(reply.Text))
	}
	summary, err := MeasurePublicSteadyState(e.ctx, e.client, TranscriptionWorkload{Audio: e.audio, Filename: "jfk.wav", Fields: []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}}, AudioDuration: 11 * time.Second, Samples: 60})
	if err != nil || summary.Successes != 60 || summary.Errors != 0 {
		t.Fatalf("local repeated workload failed: successes=%d errors=%d summary_error=%t", summary.Successes, summary.Errors, err != nil)
	}
	after, err := e.target.Processes(e.ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, process := range after {
		if process.Command == "whisper-server" {
			count++
			if process.PID != native {
				t.Fatal("successful local requests replaced the resident native worker")
			}
		}
	}
	if count != 1 {
		t.Fatal("resident native worker count changed")
	}
	summary.Startup = &e.startup
	report, err := json.MarshalIndent(map[string]any{"scope": e.selection.Scope, "runtime_input_identity": e.selection.RuntimeInputIdentity, "api_image": e.selection.APIImage, "backend_image": e.selection.BackendImage, "settings": map[string]any{"rate_limit_enabled": false, "logging": e.settings.Logging.Level, "model": e.settings.Models.Served, "operation_seconds": e.settings.Timeouts.OperationSeconds}, "summary": summary}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "baseline.json"), report, 0600) != nil {
		t.Fatal("cannot retain safe local baseline observations")
	}
	t.Log("selected baseline passed sixty sequential requests with one resident native worker; no release qualified")
}

func TestLocalPublicAndClients(t *testing.T) {
	e := openLocalEnvironment(t, nil)
	t.Run("public_contract", func(t *testing.T) {
		runPublicCases(t, e.ctx, e.client, e.selection.FixtureDirectory, e.settings.Models.Served)
	})
	t.Run("real_clients", func(t *testing.T) {
		runClientCases(t, e.ctx, e.address, filepath.Join(e.selection.FixtureDirectory, "jfk.wav"), e.workdir)
	})
}

func TestLocalLifecycle(t *testing.T) {
	e := openLocalEnvironment(t, nil)
	runLifecycleCases(t, e)
}
