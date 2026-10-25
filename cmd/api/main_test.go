package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
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

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestInvalidAPIConfiguration(t *testing.T) {
	var out bytes.Buffer
	root := filepath.Join(t.TempDir(), "work")
	if code := mainExit(
		[]string{"--mode=ram-cache"},
		[]string{},
		&out,
		runtimeOptions{decoderCommand: []string{"ffmpeg"}, workRoot: root},
	); code != 1 {
		t.Fatal("invalid settings did not fail startup")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid settings initialized runtime storage")
	}
	if !strings.Contains(out.String(), "configuration_rejected") {
		t.Fatal("configuration failure was not safely logged")
	}
}

func TestAPIAssembly(t *testing.T) {
	const transcriptionTimeout = 10 * time.Second

	var fingerprint atomic.Value
	fingerprint.Store("")
	var inference, health atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			health.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":                    "ready",
				"protocol_version":          "1",
				"configuration_fingerprint": fingerprint.Load(),
				"process_generation":        "generation-1",
				"model":                     "base",
				"multilingual":              true,
				"cuda_ready":                true,
			})

			return
		}
		inference.Add(1)
		if r.URL.Path != "/inference" ||
			r.Header.Get("X-Request-ID") != "command-request-1" ||
			r.Header.Get("X-Whisper-Configuration") != fingerprint.Load().(string) {
			t.Error("actual private adapter/ID/compatibility wiring was bypassed")
		}
		if err := r.ParseMultipartForm(64 << 10); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		defer r.MultipartForm.RemoveAll()

		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}
		defer file.Close()

		data, _ := io.ReadAll(file)
		if len(data) != 3244 ||
			string(data[:4]) != "RIFF" ||
			binary.LittleEndian.Uint32(data[24:]) != 16000 ||
			r.FormValue("language") != "ru" ||
			r.FormValue("prompt") != "PRIVATE prompt" {
			t.Error("real decoder/field normalization was bypassed")
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			t.Error("API cancelled active work before its graceful shutdown bound")
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{"text": "PRIVATE transcript"})
	}))
	defer private.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	root := t.TempDir()
	args := []string{
		"--api-host=127.0.0.1",
		"--api-port=" + strconv.Itoa(port),
		"--backend-url=" + private.URL,
		"--cache-dir=" + filepath.Join(root, "cache"),
		"--shutdown-seconds=1",
		"--cleanup-seconds=1",
		"--operation-seconds=" + strconv.Itoa(int(transcriptionTimeout/time.Second)),
		"--log-level=debug",
		"--rate-limit-burst=1",
		"--rate-limit-requests-per-second=0.01",
	}
	settings, err := config.Load(args, []string{})
	if err != nil {
		t.Fatal(err)
	}

	value, err := config.CanonicalFingerprint(settings)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Store(value)
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(
			ctx,
			args,
			[]string{},
			&logs,
			runtimeOptions{decoderCommand: []string{"ffmpeg"}, workRoot: filepath.Join(root, "uploads")},
		)
	}()

	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("API runner did not stop after test failure")
			}
		}
	})
	client := &http.Client{Timeout: 500 * time.Millisecond}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	ready := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get(base + "/health/ready")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}

		select {
		case err := <-done:
			joined = true
			t.Fatalf("API exited before ready: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("assembled API did not become ready")
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "PRIVATE-audio.bin")
	if err != nil {
		t.Fatal(err)
	}

	wave := make([]byte, 1644)
	copy(wave, "RIFF")
	binary.LittleEndian.PutUint32(wave[4:], uint32(len(wave)-8))
	copy(wave[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wave[16:], 16)
	binary.LittleEndian.PutUint16(wave[20:], 1)
	binary.LittleEndian.PutUint16(wave[22:], 1)
	binary.LittleEndian.PutUint32(wave[24:], 8000)
	binary.LittleEndian.PutUint32(wave[28:], 16000)
	binary.LittleEndian.PutUint16(wave[32:], 2)
	binary.LittleEndian.PutUint16(wave[34:], 16)
	copy(wave[36:], "data")
	binary.LittleEndian.PutUint32(wave[40:], uint32(len(wave)-44))
	_, _ = file.Write(wave)
	for _, field := range [][2]string{{"model", "base"}, {"language", "russian"}, {"prompt", "PRIVATE prompt"}} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodPost, base+"/v1/audio/transcriptions", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Request-ID", "command-request-1")
	type outcome struct {
		response *http.Response
		err      error
	}
	responseDone := make(chan outcome, 1)
	transcriptionClient := &http.Client{Timeout: transcriptionTimeout}
	go func() {
		response, err := transcriptionClient.Do(request)
		responseDone <- outcome{response, err}
	}()

	select {
	case <-entered:
	case completed := <-responseDone:
		if completed.err != nil {
			t.Fatalf("transcription failed before private inference: %v", completed.err)
		}

		_ = completed.response.Body.Close()
		t.Fatalf("transcription returned HTTP %d before private inference", completed.response.StatusCode)
	case <-time.After(transcriptionTimeout):
		t.Fatal("private inference was not entered")
	}

	for _, path := range []string{"/health/live", "/health/ready"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal("probe blocked behind active inference", err)
		}

		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("active health/ID contract failed")
		}
	}

	denied, err := client.Post(base+"/v1/audio/transcriptions", "multipart/form-data", strings.NewReader("PRIVATE invalid upload"))
	if err != nil {
		t.Fatal(err)
	}

	_ = denied.Body.Close()
	if denied.StatusCode != http.StatusTooManyRequests || denied.Header.Get("Retry-After") == "" || inference.Load() != 1 {
		t.Fatal("assembled rate limiter did not reject before body validation/inference")
	}
	cancel()
	close(release)
	completed := <-responseDone
	response, err := completed.response, completed.err
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil ||
		response.StatusCode != http.StatusOK ||
		response.Header.Get("X-Request-ID") != "command-request-1" ||
		string(bytes.TrimSpace(data)) != `{"text":"PRIVATE transcript"}` {
		t.Fatalf("public result/wiring mismatch: status=%d error=%v", response.StatusCode, err)
	}
	if inference.Load() != 1 || health.Load() < 2 {
		t.Fatal("actual application/private calls were not exercised")
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("API shutdown exceeded its bounds")
	}
	if _, err := os.Stat(filepath.Join(root, "cache")); !os.IsNotExist(err) {
		t.Fatal("API touched backend-owned model storage")
	}

	files, err := filepath.Glob(filepath.Join(root, "uploads", "run-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("API retained request scratch", err)
	}
	if strings.Contains(logs.String(), "PRIVATE") {
		t.Fatal("assembly logged private request content")
	}

	scanner := bufio.NewScanner(&logs)
	correlated := false
	readyEvent := false
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			t.Fatal("runtime diagnostic was not JSON")
		}
		if event["event"] == "queue_dispatched" && event["request_id"] == "command-request-1" {
			correlated = true
		}
		if event["event"] == "api_ready" {
			readyEvent = true
		}
	}
	if !correlated {
		t.Fatal("application lost request correlation")
	}
	if !readyEvent {
		t.Fatal("application readiness was not safely logged")
	}
}
