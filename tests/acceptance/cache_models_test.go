package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/models"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// cacheModelSource counts requests before its deny trap, so an attempted offline
// download cannot be mistaken for no source access. Only one pinned file is served.
type cacheModelSource struct {
	file        string
	source      config.Source
	requests    atomic.Int64
	tlsAttempts atomic.Int64
	deny        atomic.Bool
}

func (s *cacheModelSource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	if s.deny.Load() {
		http.Error(w, "model source intentionally denied", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "unsupported source method", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/base.bin" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(s.file)
	if err != nil {
		http.Error(w, "public model fixture unavailable", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(s.source.ExpectedBytes, 10))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.CopyN(w, file, s.source.ExpectedBytes)
}

func TestCacheSourceFixtureCountsActualProvisioningAndOfflineDenial(t *testing.T) {
	// This is a minimal valid GGML metadata prefix, not a CUDA-loadable model.
	// It exercises the actual P01 artifact provisioner without claiming GPU use.
	data := binary.LittleEndian.AppendUint32(nil, 0x67676d6c)
	for _, value := range []uint32{51865, 1500, 512, 8, 6, 448, 512, 8, 6, 80, 1} {
		data = binary.LittleEndian.AppendUint32(data, value)
	}
	identity := sha256.Sum256(data)
	root := t.TempDir()
	file := filepath.Join(root, "public-model.bin")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	source := config.Source{Kind: "download", SHA256: hex.EncodeToString(identity[:]), ExpectedBytes: int64(len(data))}
	fixture := &cacheModelSource{file: file, source: source}
	server := httptest.NewTLSServer(fixture)
	defer server.Close()
	source.URL = server.URL + "/base.bin"
	opts := models.Options{
		Models:   config.Models{Catalog: []config.Model{{ID: "base", Source: source}}, Served: "base", DownloadSet: []string{"base"}},
		CacheDir: filepath.Join(root, "valid-cache"), Attempts: 1, Timeout: 5 * time.Second,
	}
	first, err := models.Provision(t.Context(), opts, server.Client())
	if err != nil || first.SHA256 != source.SHA256 || fixture.requests.Load() != 1 {
		t.Fatalf("cold model source provisioning failed: err=%v requests=%d", err, fixture.requests.Load())
	}
	fixture.deny.Store(true)
	hit, err := models.Provision(t.Context(), opts, server.Client())
	if err != nil || hit.SHA256 != first.SHA256 || fixture.requests.Load() != 1 {
		t.Fatal("valid cache hit attempted a source request or changed the artifact")
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || fixture.requests.Load() != 2 {
		t.Fatal("deny trap did not remain reachable and count the HTTP attempt")
	}
	opts.CacheDir = filepath.Join(root, "denied-cold-cache")
	if _, err := models.Provision(t.Context(), opts, server.Client()); err == nil || fixture.requests.Load() != 3 {
		t.Fatal("denied cold provisioning did not fail at the counted source boundary")
	}
}

// cacheLocalSource uses an explicitly selected bridge address. Its public CA is
// mounted only in the owned prepared directory; the private key stays in memory.
func cacheLocalSource(t *testing.T, e *localEnvironment, base config.Model) (*cacheModelSource, string) {
	t.Helper()
	ip := net.ParseIP(os.Getenv("P07_LOCAL_SOURCE_BIND_IP"))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		t.Fatal("an explicit verified Docker-bridge model source IP is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("cannot create owned model-source TLS key")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal("cannot create owned model-source certificate serial")
	}
	certificate := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "P07 owned model source"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour),
		IPAddresses: []net.IP{ip}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal("cannot create owned model-source certificate")
	}
	caPath := filepath.Join(e.prepared, "p07-source-ca.pem")
	if os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644) != nil || os.Chmod(caPath, 0644) != nil {
		t.Fatal("cannot write the owned public model-source CA for the readonly container UID")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal("cannot bind the selected model-source bridge address")
	}
	source := &cacheModelSource{file: filepath.Join(e.prepared, "base.bin"), source: base.Source}
	server := &http.Server{
		Handler: source, ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second, WriteTimeout: 2 * time.Minute,
		ErrorLog: log.New(io.Discard, "", 0),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
			GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
				source.tlsAttempts.Add(1)
				return nil, nil
			},
		},
	}
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			t.Error("model-source fixture required forced shutdown")
		}
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("model-source fixture stopped unexpectedly")
			}
		case <-ctx.Done():
			t.Error("model-source fixture did not stop within its owner budget")
		}
	})
	return source, "https://" + listener.Addr().String() + "/base.bin"
}

func TestCacheSourceTLSTrustAndDeniedAttemptObservation(t *testing.T) {
	// Only this fixture runs here: no Docker target, native worker or GPU is used.
	t.Setenv("P07_LOCAL_SOURCE_BIND_IP", "127.0.0.1")
	prepared := t.TempDir()
	data := []byte("public model-source transport fixture")
	identity := sha256.Sum256(data)
	if os.WriteFile(filepath.Join(prepared, "base.bin"), data, 0600) != nil {
		t.Fatal("cannot prepare owned source transport bytes")
	}
	e := &localEnvironment{prepared: prepared}
	model := config.Model{ID: "base", Source: config.Source{SHA256: hex.EncodeToString(identity[:]), ExpectedBytes: int64(len(data))}}
	source, address := cacheLocalSource(t, e, model)
	certificate, err := os.ReadFile(filepath.Join(prepared, "p07-source-ca.pem"))
	if err != nil {
		t.Fatal("owned public CA is unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("owned model-source CA could not be loaded")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("actual TLS fixture could not be reached with its scoped CA")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(data))+1))
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, data) || source.requests.Load() != 1 || source.tlsAttempts.Load() != 1 {
		t.Fatal("actual model source bytes/TLS/HTTP observations differ")
	}
	source.deny.Store(true)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal("live deny trap was unreachable")
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || source.requests.Load() != 2 {
		t.Fatal("live deny trap did not count the actual request")
	}
	before := cacheCounters(source)
	untrusted := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}}, Timeout: 2 * time.Second}
	defer untrusted.CloseIdleConnections()
	response, err = untrusted.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || source.requests.Load() != before.HTTP || source.tlsAttempts.Load() <= before.TLS {
		t.Fatal("a rejected TLS source attempt was incorrectly reported as no access")
	}
}

type cacheSourceCounters struct{ HTTP, TLS int64 }

func cacheCounters(source *cacheModelSource) cacheSourceCounters {
	return cacheSourceCounters{HTTP: source.requests.Load(), TLS: source.tlsAttempts.Load()}
}

func requireCacheSourceQuiet(t *testing.T, source *cacheModelSource, before cacheSourceCounters) {
	t.Helper()
	if after := cacheCounters(source); after != before {
		t.Fatalf("unexpected model-source access: HTTP %d -> %d; TLS %d -> %d", before.HTTP, after.HTTP, before.TLS, after.TLS)
	}
}

func cachedLocalSHA(t *testing.T, e *localEnvironment, source config.Source) string {
	t.Helper()
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal("cannot resolve the owned cache container")
	}
	data, err := e.target.run(e.ctx, "owned cached model SHA256", false, "exec", containers.BackendID, "sha256sum", filepath.Join(e.settings.Storage.CacheDir, "artifacts", source.SHA256+".bin"))
	if err != nil {
		t.Fatal("cannot measure the owned cached model checksum")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] != source.SHA256 {
		t.Fatal("previously valid model cache identity changed")
	}
	return fields[0]
}

func readyCacheModel(t *testing.T, e *localEnvironment, expected string) string {
	t.Helper()
	if err := waitLocalReady(e.ctx, e.target, e.address, time.Duration(e.settings.Timeouts.ProvisioningSeconds)*time.Second); err != nil {
		t.Fatal(err)
	}
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal("cannot resolve the owned ready model pair")
	}
	health, err := e.target.privateHealth(e.ctx, containers.APIID)
	if err != nil || health == nil || health.HTTPStatus != http.StatusOK || health.Status != "ready" || health.Model != expected || !health.CUDAReady || health.ProcessGeneration == "" {
		t.Fatal("the expected fixed CUDA model is not independently ready")
	}
	reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", expected}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || strings.TrimSpace(reply.Text) == "" {
		t.Fatal("the configured fixed model did not transcribe the public fixture")
	}
	return health.ProcessGeneration
}

// cacheBackendStarted projects bounded safe lifecycle events, not raw native
// diagnostics. A "backend_starting" event is emitted only after child Start.
func cacheBackendStarted(data []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 64<<10)
	for scanner.Scan() {
		line := scanner.Bytes()
		if start := bytes.IndexByte(line, '{'); start >= 0 {
			var event struct {
				Event string `json:"event"`
			}
			if json.Unmarshal(line[start:], &event) == nil && event.Event == "backend_starting" {
				return true
			}
		}
	}
	return false
}

// The pinned native loader accepts zero tensors as a testing model. Truncation
// alone therefore cannot guarantee initialization failure. The known native
// enum GGML_FTYPE_MOSTLY_Q4_1_SOME_F16 (4) explicitly maps to GGML_TYPE_COUNT;
// its unconditional GGML assertion fails before the empty-tensor fallback.
// Revisit this fixture when the pinned native source changes.
const cacheUnsupportedNativeFType uint32 = 4

func invalidateCacheNativeHeader(header [48]byte) [48]byte {
	binary.LittleEndian.PutUint32(header[44:], cacheUnsupportedNativeFType)
	return header
}

func TestCacheNativeFailureFixturePassesActualProvisioning(t *testing.T) {
	data := binary.LittleEndian.AppendUint32(nil, 0x67676d6c)
	for _, value := range []uint32{51865, 1500, 512, 8, 6, 448, 512, 8, 6, 80, 1} {
		data = binary.LittleEndian.AppendUint32(data, value)
	}
	header := invalidateCacheNativeHeader([48]byte(data))
	identity := sha256.Sum256(header[:])
	root := t.TempDir()
	path := filepath.Join(root, "invalid-native.bin")
	if os.WriteFile(path, header[:], 0600) != nil {
		t.Fatal("cannot write the owned invalid-native fixture")
	}
	source := config.Source{Kind: "prepared", Path: path, SHA256: hex.EncodeToString(identity[:]), ExpectedBytes: int64(len(header))}
	opts := models.Options{Models: config.Models{Catalog: []config.Model{{ID: "invalid-native", Source: source}}, Served: "invalid-native", DownloadSet: []string{}}, CacheDir: filepath.Join(root, "cache"), Attempts: 1, Timeout: time.Second}
	artifact, err := models.Provision(t.Context(), opts, &http.Client{Timeout: time.Second})
	if err != nil || artifact.SHA256 != source.SHA256 {
		t.Fatal("native-failure fixture was rejected before actual native initialization")
	}
	if binary.LittleEndian.Uint32(header[44:]) != cacheUnsupportedNativeFType {
		t.Fatal("native-failure fixture still permits the pinned empty-model path")
	}
}

type cacheFailureFacts struct {
	InitialReadinessStatus              int  `json:"initial_readiness_status"`
	ReadinessStatus                     int  `json:"readiness_status"`
	ReadinessReachable                  bool `json:"readiness_reachable"`
	LivenessStatus                      int  `json:"liveness_status"`
	LivenessReachable                   bool `json:"liveness_reachable"`
	APIInspectionKnown                  bool `json:"api_inspection_known"`
	APIRunning                          bool `json:"api_running"`
	APIImageMatchesSelection            bool `json:"api_image_matches_selection"`
	BackendInspectionKnown              bool `json:"backend_inspection_known"`
	BackendRunning                      bool `json:"backend_running"`
	BackendImageMatchesSelection        bool `json:"backend_image_matches_selection"`
	AddressKnown                        bool `json:"address_known"`
	AddressMatchesTarget                bool `json:"address_matches_target"`
	PrivateHealthKnown                  bool `json:"private_health_known"`
	PrivateHealthStatus                 int  `json:"private_health_status"`
	PrivateStateCode                    int  `json:"private_state_code"`
	PrivateModelMatchesConfigured       bool `json:"private_model_matches_configured"`
	FingerprintKnown                    bool `json:"fingerprint_known"`
	PrivateFingerprintMatchesConfigured bool `json:"private_fingerprint_matches_configured"`
	BackendLogsKnown                    bool `json:"backend_logs_known"`
	NativeStartObserved                 bool `json:"native_start_observed"`
}

func cacheProbeStatus(ctx context.Context, client *http.Client, endpoint string) (int, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, false
	}
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
		return response.StatusCode, err == nil
	}
	return 0, false
}

func TestCacheFailureProbeRetainsActualWireStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/not-ready":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/false-ready":
			w.WriteHeader(http.StatusOK)
		case "/redirect":
			http.Redirect(w, r, "/false-ready", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/not-ready", http.StatusServiceUnavailable},
		{"/false-ready", http.StatusOK},
		{"/redirect", http.StatusTemporaryRedirect},
	} {
		status, reachable := cacheProbeStatus(t.Context(), client, server.URL+test.path)
		if !reachable || status != test.status {
			t.Fatalf("failure probe lost actual wire status: got=%d reachable=%t want=%d", status, reachable, test.status)
		}
	}
	server.Close()
	if status, reachable := cacheProbeStatus(t.Context(), client, server.URL+"/false-ready"); reachable || status != 0 {
		t.Fatal("unreachable failure probe reused a prior ready result")
	}
}

// Diagnose before cleanup replaces failing containers. Facts are deliberately
// numeric/boolean: no response bodies, addresses, model names, paths or IDs leave
// the observation. State codes: 0 unknown, 1 starting, 2 ready, 3 busy,
// 4 recovering, 5 stopping, 6 failed. This does not relax the false-ready guard.
func diagnoseCacheStartup(t *testing.T, e *localEnvironment, initialStatus int, nativeMustStart bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	facts := cacheFailureFacts{InitialReadinessStatus: initialStatus}
	facts.ReadinessStatus, facts.ReadinessReachable = cacheProbeStatus(ctx, client, e.address+"/health/ready")
	facts.LivenessStatus, facts.LivenessReachable = cacheProbeStatus(ctx, client, e.address+"/health/live")
	if state, err := e.target.Inspect(ctx, "api"); err == nil {
		facts.APIInspectionKnown, facts.APIRunning = true, state.Running
		facts.APIImageMatchesSelection = state.Reference == e.target.opts.APIImage
	}
	if state, err := e.target.Inspect(ctx, "backend"); err == nil {
		facts.BackendInspectionKnown, facts.BackendRunning = true, state.Running
		facts.BackendImageMatchesSelection = state.Reference == e.target.opts.BackendImage
	}
	if address, err := e.target.APIAddress(ctx); err == nil {
		facts.AddressKnown, facts.AddressMatchesTarget = true, address == e.address
	}
	if containers, err := e.target.Containers(ctx); err == nil {
		if health, err := e.target.privateHealth(ctx, containers.APIID); err == nil && health != nil {
			facts.PrivateHealthKnown, facts.PrivateHealthStatus = true, health.HTTPStatus
			switch health.Status {
			case "starting":
				facts.PrivateStateCode = 1
			case "ready":
				facts.PrivateStateCode = 2
			case "busy":
				facts.PrivateStateCode = 3
			case "recovering":
				facts.PrivateStateCode = 4
			case "stopping":
				facts.PrivateStateCode = 5
			case "failed":
				facts.PrivateStateCode = 6
			}
			facts.PrivateModelMatchesConfigured = health.Model == e.settings.Models.Served
			if fingerprint, err := config.CanonicalFingerprint(e.settings); err == nil {
				facts.FingerprintKnown = true
				facts.PrivateFingerprintMatchesConfigured = health.ConfigurationFingerprint == fingerprint
			}
		}
	}
	if data, err := e.target.Logs(ctx, "backend"); err == nil && len(data) != 0 {
		facts.BackendLogsKnown, facts.NativeStartObserved = true, cacheBackendStarted(data)
	}
	data, err := json.Marshal(facts)
	if err != nil {
		t.Error("cannot encode safe model-startup failure facts")
		return
	}
	t.Logf("model-startup failure facts: %s", data)
	dir := filepath.Join(e.root, ".local", "p07", "cache")
	phase := "provision"
	if nativeMustStart {
		phase = "native"
	}
	if os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "failure-"+phase+"-"+filepath.Base(e.workdir)+".json"), data, 0600) != nil {
		t.Error("cannot retain safe model-startup failure facts")
	}
}

func unavailableCacheModel(t *testing.T, e *localEnvironment, nativeMustStart bool) {
	t.Helper()
	budget := time.Duration(e.settings.Timeouts.CleanupSeconds+e.settings.Timeouts.ShutdownSeconds+5) * time.Second
	ctx, cancel := context.WithTimeout(e.ctx, budget)
	defer cancel()
	probe := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer probe.CloseIdleConnections()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.address+"/health/ready", nil)
		if err != nil {
			t.Fatal("cannot build the explicit failure readiness probe")
		}
		response, probeErr := probe.Do(request)
		if response != nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusServiceUnavailable {
				diagnoseCacheStartup(t, e, response.StatusCode, nativeMustStart)
				t.Fatalf("failed model startup exposed false readiness or an invalid probe status: readiness_status=%d", response.StatusCode)
			}
		}
		state, err := e.target.Inspect(ctx, "backend")
		if err != nil {
			t.Fatal("failed model container state could not be observed")
		}
		if !state.Running {
			// A connection failure before listening is permitted, but a successful
			// transcript from the old model is never an acceptable failure outcome.
			callCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			reply, callErr := e.client.Transcribe(callCtx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"language", "en"}})
			stop()
			if reply.Status != http.StatusServiceUnavailable && (callErr == nil || reply.Status != 0) {
				diagnoseCacheStartup(t, e, 0, nativeMustStart)
				t.Fatalf("failed model startup returned a non-unavailable transcription result: transcription_status=%d", reply.Status)
			}
			logs, err := e.target.Logs(ctx, "backend")
			if err != nil || cacheBackendStarted(logs) != nativeMustStart {
				diagnoseCacheStartup(t, e, 0, nativeMustStart)
				t.Fatal("startup failure did not occur at the expected provisioning/native boundary")
			}
			apiState, err := e.target.Inspect(ctx, "api")
			if err != nil {
				t.Fatal("API startup failure state could not be observed")
			}
			t.Logf("failed fixed-model startup observed: API running=%t; public readiness reachable=%t; native started=%t", apiState.Running, probeErr == nil, nativeMustStart)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("invalid model did not fail within its bounded startup observation")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

type cacheLocalCheck struct {
	Name               string `json:"check"`
	Before, After      cacheSourceCounters
	Model, CacheSHA256 string
}

func TestLocalCacheAndModels(t *testing.T) {
	e := openLocalEnvironment(t, nil)
	originalSettings, originalImage := e.settings, e.target.opts.BackendImage
	base := originalSettings.Models.Catalog[0]
	caImage := os.Getenv("P07_LOCAL_CA_BACKEND_IMAGE")
	if caImage == "" {
		t.Fatal("the explicitly verified local source-CA derivative image is required")
	}
	caOptions := e.target.opts
	caOptions.BackendImage = caImage
	if _, err := NewLocalDockerTarget(caOptions); err != nil {
		t.Fatal("source-CA image must be an explicit immutable local backend reference")
	}
	source, sourceURL := cacheLocalSource(t, e, base)
	trap := config.Model{ID: "unselected-download", Source: config.Source{Kind: "download", URL: sourceURL, SHA256: base.Source.SHA256, ExpectedBytes: base.Source.ExpectedBytes}}
	prepared := originalSettings
	prepared.Models.Catalog = []config.Model{base, trap}
	prepared.Models.DownloadSet = []string{}
	checks := []cacheLocalCheck{}
	modified, cachePopulated := false, false
	t.Cleanup(func() {
		data, err := json.MarshalIndent(struct {
			Scope                string            `json:"scope"`
			RuntimeInputIdentity string            `json:"runtime_input_identity"`
			SourceCABackendImage string            `json:"source_ca_backend_image"`
			OriginalBackendImage string            `json:"original_backend_image"`
			Checks               []cacheLocalCheck `json:"observed_checks"`
		}{"local_validation_not_release", e.receipt.RuntimeInputIdentity, caImage, originalImage, checks}, "", "  ")
		dir := filepath.Join(e.root, ".local", "p07", "cache")
		if err != nil || os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "cache-models-"+filepath.Base(e.workdir)+".json"), data, 0600) != nil {
			t.Error("cannot retain safe owned cache/model observations")
		}
	})
	t.Cleanup(func() {
		if !modified {
			return
		}
		// testing.T cancels its Context before cleanup callbacks. Restoration owns
		// a separate bounded context, just like the target's final Down cleanup.
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		previousContext := e.ctx
		e.ctx = cleanup
		defer func() { e.ctx = previousContext }()
		e.target.opts.BackendImage = originalImage
		writeAndRecreateLocal(t, e, originalSettings)
		readyCacheModel(t, e, base.ID)
		if cachePopulated {
			cachedLocalSHA(t, e, base.Source)
		}
	})
	change := func(t *testing.T, settings config.Settings) {
		t.Helper()
		modified = true
		writeAndRecreateLocal(t, e, settings)
	}
	observe := func(name, model string, before cacheSourceCounters, checksum string) {
		checks = append(checks, cacheLocalCheck{Name: name, Model: model, Before: before, After: cacheCounters(source), CacheSHA256: checksum})
	}
	if !t.Run("prepared_has_no_source_traffic", func(t *testing.T) {
		e.target.opts.BackendImage = caImage
		before := cacheCounters(source)
		change(t, prepared)
		readyCacheModel(t, e, base.ID)
		state, err := e.target.Inspect(e.ctx, "backend")
		if err != nil || state.Reference != caImage {
			t.Fatal("the selected task-only source-CA image was not used")
		}
		requireCacheSourceQuiet(t, source, before)
		observe("prepared_no_source", base.ID, before, "")
	}) {
		return
	}
	download := prepared
	downloaded := base
	downloaded.Source.Kind, downloaded.Source.Path, downloaded.Source.URL = "download", "", sourceURL
	download.Models.Catalog = []config.Model{downloaded, trap}
	download.Models.DownloadSet = []string{base.ID}
	var coldGeneration string
	if !t.Run("cold_pinned_download", func(t *testing.T) {
		before := cacheCounters(source)
		change(t, download)
		coldGeneration = readyCacheModel(t, e, base.ID)
		if source.requests.Load()-before.HTTP != 1 || source.tlsAttempts.Load() <= before.TLS {
			t.Fatal("cold model preparation did not make one observed HTTPS download")
		}
		checksum := cachedLocalSHA(t, e, base.Source)
		cachePopulated = true
		observe("cold_download", base.ID, before, checksum)
	}) {
		return
	}
	if !t.Run("offline_restart_reuses_cache", func(t *testing.T) {
		source.deny.Store(true)
		before := cacheCounters(source)
		change(t, download)
		if readyCacheModel(t, e, base.ID) == coldGeneration {
			t.Fatal("offline cache check did not actually replace the native generation")
		}
		requireCacheSourceQuiet(t, source, before)
		observe("offline_restart_no_source", base.ID, before, cachedLocalSHA(t, e, base.Source))
	}) {
		return
	}
	if !t.Run("prepared_with_deny_trap", func(t *testing.T) {
		e.target.opts.BackendImage = originalImage
		before := cacheCounters(source)
		change(t, prepared)
		readyCacheModel(t, e, base.ID)
		requireCacheSourceQuiet(t, source, before)
		observe("prepared_deny_no_source", base.ID, before, cachedLocalSHA(t, e, base.Source))
	}) {
		return
	}
	if !t.Run("missing_prepared_does_not_fallback", func(t *testing.T) {
		missing := base
		missing.Source.Path = "/prepared/p07-missing-model.bin"
		settings := prepared
		settings.Models.Catalog = []config.Model{missing, trap}
		before := cacheCounters(source)
		change(t, settings)
		unavailableCacheModel(t, e, false)
		requireCacheSourceQuiet(t, source, before)
		observe("missing_prepared_unavailable", base.ID, before, "")
	}) {
		return
	}
	if !t.Run("pinned_tiny_requires_configuration_restart", func(t *testing.T) {
		tiny := config.Model{ID: "tiny", Source: config.Source{Kind: "prepared", Path: "/prepared/tiny.bin", SHA256: "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21", ExpectedBytes: 77691713}}
		copyPreparedModel(t, e.receipt.TinyFile, filepath.Join(e.prepared, "tiny.bin"), tiny.Source)
		settings := prepared
		settings.Models.Catalog, settings.Models.Served = []config.Model{tiny, trap}, tiny.ID
		before := cacheCounters(source)
		change(t, settings)
		tinyGeneration := readyCacheModel(t, e, tiny.ID)
		reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", base.ID}})
		if err != nil || reply.Status != http.StatusNotFound || reply.ErrorCode != "model_not_found" {
			t.Fatal("a request attempted to select the old configured model")
		}
		if readyCacheModel(t, e, tiny.ID) != tinyGeneration {
			t.Fatal("rejected request model changed the configured tiny resident process")
		}
		requireCacheSourceQuiet(t, source, before)
		observe("pinned_tiny_restart", tiny.ID, before, cachedLocalSHA(t, e, base.Source))
	}) {
		return
	}
	if !t.Run("native_init_rejects_unsupported_ftype", func(t *testing.T) {
		file, err := os.Open(filepath.Join(e.prepared, "base.bin"))
		if err != nil {
			t.Fatal("cannot open owned public model metadata fixture")
		}
		var prefix [48]byte
		_, readErr := io.ReadFull(file, prefix[:])
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("cannot read the actual public GGML prefix")
		}
		prefix = invalidateCacheNativeHeader(prefix)
		if _, err := models.ReadMetadata(e.ctx, bytes.NewReader(prefix[:])); err != nil {
			t.Fatal("invalid-model probe would fail before reaching native initialization")
		}
		identity := sha256.Sum256(prefix[:])
		invalid := config.Model{ID: "invalid-native", Source: config.Source{Kind: "prepared", Path: "/prepared/invalid-native.bin", SHA256: hex.EncodeToString(identity[:]), ExpectedBytes: int64(len(prefix))}}
		invalidPath := filepath.Join(e.prepared, "invalid-native.bin")
		if os.WriteFile(invalidPath, prefix[:], 0644) != nil || os.Chmod(invalidPath, 0644) != nil {
			t.Fatal("cannot create the owned invalid native artifact for the readonly container UID")
		}
		fixtureFacts, err := json.Marshal(struct {
			Scope                 string `json:"scope"`
			NativeSourceCommit    string `json:"native_source_commit"`
			FType                 uint32 `json:"ftype"`
			PreparedSHA256        string `json:"prepared_sha256"`
			PreparedBytes         int    `json:"prepared_bytes"`
			GoMetadataAccepted    bool   `json:"go_metadata_accepted"`
			ExpectedNativeFailure string `json:"expected_native_failure"`
		}{"local_validation_not_release", "6ac14546046ffb17d144a08bcad65337e535cf5f", cacheUnsupportedNativeFType, invalid.Source.SHA256, len(prefix), true, "unsupported_ftype_assert"})
		dir := filepath.Join(e.root, ".local", "p07", "cache")
		if err != nil || os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "native-fixture-"+filepath.Base(e.workdir)+".json"), fixtureFacts, 0600) != nil {
			t.Fatal("cannot retain safe native-failure fixture facts")
		}
		settings := prepared
		settings.Models.Catalog, settings.Models.Served = []config.Model{invalid, trap}, invalid.ID
		before := cacheCounters(source)
		change(t, settings)
		unavailableCacheModel(t, e, true)
		requireCacheSourceQuiet(t, source, before)
		observe("native_init_failure_unavailable", invalid.ID, before, "")
	}) {
		return
	}
	if !t.Run("restore_base_preserves_prior_cache", func(t *testing.T) {
		before := cacheCounters(source)
		change(t, originalSettings)
		readyCacheModel(t, e, base.ID)
		requireCacheSourceQuiet(t, source, before)
		observe("restored_base", base.ID, before, cachedLocalSHA(t, e, base.Source))
		modified = false
	}) {
		return
	}
	t.Log("owned local cache/model cases completed; published candidate/GPU qualification remains separate")
}
