package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func privateRequest(t *testing.T, s *Supervisor, url string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "PRIVATE-filename.wav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(wavFixture())
	_ = form.WriteField("language", "en")
	_ = form.Close()

	r, err := http.NewRequest(http.MethodPost, url+"/inference", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-Request-ID", "test-request-01")

	h, _ := s.Health(context.Background())
	r.Header.Set("X-Whisper-Protocol", h.ProtocolVersion)
	r.Header.Set("X-Whisper-Configuration", h.ConfigurationFingerprint)
	r.Header.Set("X-Whisper-Generation", h.ProcessGeneration)
	r.Header.Set("X-Whisper-Timeout-Ms", "500")

	return r
}

func TestPrivateRoutes(t *testing.T) {
	opts, root, log := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for _, path := range []string{"/load", "/", "/v1/audio/transcriptions", "/inference/", "/metrics"} {
		r, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.StatusCode != 404 || !strings.Contains(string(data), "route_not_found") {
			t.Fatalf("private route escaped allowlist: %s %d %s", path, r.StatusCode, data)
		}
	}

	r, err := server.Client().Do(privateRequest(t, s, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 || result.Text != "fixture transcript" || r.Header.Get("X-Request-ID") != "test-request-01" {
		t.Fatalf("private inference: %d %+v", r.StatusCode, result)
	}
	if len(starts(t, root)) != 1 || strings.Contains(log.text(), "PRIVATE") {
		t.Fatal("route caused restart or leaked content")
	}
}

func TestGenerationMismatch(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for _, header := range []string{
		"X-Whisper-Protocol",
		"X-Whisper-Configuration",
		"X-Whisper-Generation",
		"X-Whisper-Timeout-Ms",
	} {
		r := privateRequest(t, s, server.URL)
		r.Header.Set(header, "PRIVATE-invalid")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode < 400 || strings.Contains(string(body), "PRIVATE") {
			t.Fatalf("invalid compatibility accepted/leaked: %s %d", header, response.StatusCode)
		}
	}

	r := privateRequest(t, s, server.URL)
	r.Header.Add("X-Whisper-Generation", r.Header.Get("X-Whisper-Generation"))
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode < 400 {
		t.Fatal("duplicate compatibility header accepted")
	}

	files, _ := filepath.Glob(filepath.Join(root, "call-*"))
	if len(files) != 0 {
		t.Fatal("mismatched request reached native inference")
	}
}

func TestPrivateBodyValidation(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for _, scenario := range []string{
		"unknown",
		"duplicate",
		"path",
		"bad-wave",
		"too-large",
		"language",
		"temperature",
		"stream",
	} {
		t.Run(scenario, func(t *testing.T) {
			r := privateRequest(t, s, server.URL)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			field, _ := form.CreateFormFile("file", "../../PRIVATE")
			wave := wavFixture()
			if scenario == "bad-wave" {
				wave = []byte("PRIVATE invalid audio")
			}
			if scenario == "too-large" {
				wave = make([]byte, opts.MaxWAVBytes+1)
			}

			_, _ = field.Write(wave)
			switch scenario {
			case "unknown":
				_ = form.WriteField("PRIVATE", "x")
			case "duplicate":
				f, _ := form.CreateFormFile("file", "other.wav")
				_, _ = f.Write(wavFixture())
			case "path":
				_ = form.WriteField("file", "/etc/PRIVATE")
			case "language":
				_ = form.WriteField("language", "en zh")
			case "temperature":
				_ = form.WriteField("temperature", "NaN")
			case "stream":
				_ = form.WriteField("stream", "false")
			}

			_ = form.Close()
			r.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
			r.ContentLength = int64(body.Len())
			r.Header.Set("Content-Type", form.FormDataContentType())

			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}

			data, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode < 400 || strings.Contains(string(data), "PRIVATE") {
				t.Fatalf("invalid private body accepted/leaked: %d %s", response.StatusCode, data)
			}
		})
	}

	files, _ := filepath.Glob(filepath.Join(root, "call-*"))
	if len(files) != 0 {
		t.Fatal("invalid body reached native")
	}

	h, _ := s.Health(context.Background())
	if h.Status != domain.BackendReady || len(starts(t, root)) != 1 {
		t.Fatal("invalid input changed residency")
	}
}

func TestHealthDuringInference(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	result := make(chan error, 1)
	go func() {
		_, err := s.Infer(context.Background(), requestFor(s, "block"))
		result <- err
	}()
	await(t, func() bool {
		files, _ := filepath.Glob(filepath.Join(root, "active-*"))
		return len(files) == 1
	})

	started := time.Now()
	response, err := server.Client().Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var health struct {
		Status string `json:"status"`
		CUDA   bool   `json:"cuda_ready"`
	}
	_ = json.NewDecoder(response.Body).Decode(&health)
	if response.StatusCode != 200 || health.Status != "busy" || !health.CUDA || time.Since(started) > 200*time.Millisecond {
		t.Fatalf("health blocked or lied: %d %+v", response.StatusCode, health)
	}

	_, err = s.Infer(context.Background(), requestFor(s, ""))
	requireKind(t, err, domain.NotReady)

	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestPrivateRemainingDeadline(t *testing.T) {
	opts, _, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for _, value := range []string{
		"0",
		"-1",
		strconv.FormatInt(int64(opts.OperationTimeout/time.Millisecond)+1, 10),
		"9223372036854775807",
	} {
		r := privateRequest(t, s, server.URL)
		r.Header.Set("X-Whisper-Timeout-Ms", value)
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("invalid residual deadline accepted: %s", value)
		}
	}
}
