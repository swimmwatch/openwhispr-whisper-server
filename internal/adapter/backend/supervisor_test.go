package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/models"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestNativeChild(t *testing.T) {
	if os.Getenv("WHISPER_TEST_NATIVE") != "1" {
		return
	}

	root := os.Getenv("WHISPER_TEST_STATE")
	pid := strconv.Itoa(os.Getpid())
	args := os.Args
	var port string
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}

	encoded, _ := json.Marshal(args)
	_ = os.WriteFile(filepath.Join(root, "start-"+pid), encoded, 0600)
	mode, _ := os.ReadFile(filepath.Join(root, "mode"))
	if string(mode) == "stubborn" {
		signal.Ignore(syscall.SIGTERM)
	}

	if string(mode) != "cpu" {
		fmt.Print(deviceLine, backendLine)
	}
	if string(mode) != "cpu" && string(mode) != "no-allocation" {
		fmt.Print(allocationLine)
	}
	fmt.Fprintln(os.Stderr, "PRIVATE_NATIVE_OUTPUT", strings.Repeat("PRIVATE", 1200))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(root, "bad-health")); err == nil {
			w.WriteHeader(503)
			return
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/inference", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		data, _ := json.Marshal(r.MultipartForm.Value)
		_ = os.WriteFile(filepath.Join(root, "call-"+pid), data, 0600)
		_ = os.WriteFile(filepath.Join(root, "active-"+pid), nil, 0600)
		defer os.Remove(filepath.Join(root, "active-"+pid))
		if r.FormValue("prompt") == "block" {
			for {
				if _, err := os.Stat(filepath.Join(root, "release")); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		if r.FormValue("prompt") == "crash" {
			os.Exit(9)
		}
		if r.FormValue("prompt") == "malformed" {
			_, _ = io.WriteString(w, "PRIVATE response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"fixture transcript"}`)
	})

	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		os.Exit(8)
	}
	os.Exit(0)
}

func TestGPUChild(t *testing.T) {
	if os.Getenv("WHISPER_TEST_NATIVE") != "1" {
		return
	}

	root := os.Getenv("WHISPER_TEST_STATE")
	if _, err := os.Stat(filepath.Join(root, "bad-gpu")); err == nil {
		fmt.Println("PRIVATE_GPU_FAILURE")
		os.Exit(1)
	}
	fmt.Println("GPU-11111111-1111-1111-1111-111111111111, NVIDIA GeForce RTX 2060, 7.5")
	os.Exit(0)
}

type lockedLog struct {
	mu sync.Mutex
	bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Buffer.Write(p)
}

func (l *lockedLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Buffer.String()
}

func supervisorOptions(t *testing.T) (Options, string, *lockedLog) {
	t.Helper()

	root := t.TempDir()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	path := filepath.Join(root, "PRIVATE-model.bin")
	if err := os.WriteFile(path, []byte("prepared fixture"), 0444); err != nil {
		t.Fatal(err)
	}

	log := &lockedLog{}
	opts := Options{
		NativeCommand: []string{os.Args[0], "-test.run=^TestNativeChild$", "--"},
		GPUCommand:    []string{os.Args[0], "-test.run=^TestGPUChild$", "--"},
		Environment:   append(os.Environ(), "WHISPER_TEST_NATIVE=1", "WHISPER_TEST_STATE="+root, "GORACE=atexit_sleep_ms=0"),
		Artifact: models.Artifact{
			ID:       "base",
			Path:     path,
			SHA256:   strings.Repeat("a", 64),
			Metadata: models.Metadata{VocabularySize: 51865, Multilingual: true},
		},
		Fingerprint:      strings.Repeat("b", 64),
		NativePort:       port,
		WorkRoot:         filepath.Join(root, "work"),
		OperationTimeout: 2 * time.Second,
		CleanupTimeout:   400 * time.Millisecond,
		ShutdownTimeout:  100 * time.Millisecond,
		MaxWAVBytes:      32044,
		MaxReplyBytes:    1 << 20,
		PromptBytes:      8192,
		Logger:           slog.New(slog.NewJSONHandler(log, nil)),
	}

	return opts, root, log
}

func runningSupervisor(t *testing.T, opts Options) *Supervisor {
	t.Helper()

	s, _ := runningSupervisorWithResult(t, opts)
	return s
}

func runningSupervisorWithResult(t *testing.T, opts Options) (*Supervisor, <-chan error) {
	t.Helper()

	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- s.Run(context.Background())
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error("shutdown", err)
		}
		select {
		case <-s.done:
		case <-ctx.Done():
			t.Error("Run did not finish")
		}
	})

	await(t, func() bool {
		h, _ := s.Health(context.Background())
		return h.Status == domain.BackendReady
	})

	return s, result
}

func await(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func starts(t *testing.T, root string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(root, "start-*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func requireKind(t *testing.T, err error, kind domain.FailureKind) {
	t.Helper()

	var f *domain.Failure
	if !errors.As(err, &f) || f.Kind != kind {
		t.Fatalf("expected %s, got %v", kind, err)
	}
	if strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("private diagnostic exposed")
	}
}

func wavFixture() []byte {
	data := make([]byte, 44+3200)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 3200)
	return data
}

func requestFor(s *Supervisor, prompt string) Request {
	h, _ := s.Health(context.Background())
	data := wavFixture()
	return Request{
		Audio:  bytes.NewReader(data),
		Size:   int64(len(data)),
		Fields: domain.InferenceFields{Language: "en", Prompt: prompt},
		Compatibility: domain.BackendCompatibility{
			ProtocolVersion:          h.ProtocolVersion,
			ConfigurationFingerprint: h.ConfigurationFingerprint,
			ProcessGeneration:        h.ProcessGeneration,
		},
	}
}

func TestResidentSupervisor(t *testing.T) {
	opts, root, log := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())

	for _, prompt := range []string{"PRIVATE prompt", ""} {
		result, err := s.Infer(context.Background(), requestFor(s, prompt))
		if err != nil || result.Text != "fixture transcript" {
			t.Fatalf("inference: %+v %v", result, err)
		}
	}

	last, _ := s.Health(context.Background())
	if first.ProcessGeneration != last.ProcessGeneration || len(starts(t, root)) != 1 {
		t.Fatal("normal success restarted native model")
	}
	if strings.Contains(log.text(), "PRIVATE") {
		t.Fatal("native/content output leaked into structured logging")
	}

	data, err := os.ReadFile(strings.Replace(starts(t, root)[0], "start-", "call-", 1))
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string][]string
	_ = json.Unmarshal(data, &fields)
	if len(fields["prompt"]) != 1 || fields["prompt"][0] != "" || fields["carry_initial_prompt"][0] != "false" ||
		fields["response_format"][0] != "json" {
		t.Fatalf("native defaults not reset: %v", fields)
	}
}

func TestStartupReadiness(t *testing.T) {
	for _, mode := range []string{"cpu", "no-allocation"} {
		t.Run(mode, func(t *testing.T) {
			opts, root, _ := supervisorOptions(t)
			opts.OperationTimeout = 100 * time.Millisecond
			if err := os.WriteFile(filepath.Join(root, "mode"), []byte(mode), 0600); err != nil {
				t.Fatal(err)
			}

			s, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = s.Run(ctx)
			requireKind(t, err, domain.GPUUnavailable)

			h, _ := s.Health(context.Background())
			if h.CUDAReady || h.Status == domain.BackendReady {
				t.Fatal("CPU fallback became ready")
			}

			for _, path := range starts(t, root) {
				pid, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "start-"))
				if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
					t.Fatalf("failed startup child survived: %v", err)
				}
			}
		})
	}
}
