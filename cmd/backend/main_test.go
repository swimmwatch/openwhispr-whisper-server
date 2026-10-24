package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestCommandChild(t *testing.T) {
	if os.Getenv("P02_COMMAND_CHILD") != "1" {
		return
	}

	root := os.Getenv("P02_COMMAND_ROOT")
	runtime := runtimeOptions{
		nativeCommand: []string{os.Args[0], "-test.run=^TestCommandNative$", "--"},
		gpuCommand:    []string{os.Args[0], "-test.run=^TestCommandGPU$", "--"},
		workRoot:      filepath.Join(root, "work"),
	}
	os.Exit(mainExit([]string{"--config", filepath.Join(root, "config.json")}, os.Environ(), os.Stdout, runtime))
}

func TestCommandNative(t *testing.T) {
	if os.Getenv("P02_COMMAND_CHILD") != "1" {
		return
	}

	root := os.Getenv("P02_COMMAND_ROOT")
	port := ""
	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}

	_ = os.WriteFile(filepath.Join(root, "native-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	fmt.Println("  Device 0: NVIDIA GeForce RTX 2060, compute capability 7.5, VMM: yes, VRAM: 6144 MiB")
	fmt.Println("whisper_backend_init_gpu: using CUDA0 backend")
	fmt.Println("whisper_model_load:        CUDA0 total size =   147.95 MB")
	fmt.Fprintln(os.Stderr, "PRIVATE_COMMAND_NATIVE", strings.Repeat("PRIVATE", 2000))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		os.Exit(9)
	}
}

func TestCommandGPU(t *testing.T) {
	if os.Getenv("P02_COMMAND_CHILD") != "1" {
		return
	}
	if os.Getenv("P02_BAD_GPU") == "1" {
		fmt.Println("PRIVATE_GPU_FAILURE")
		os.Exit(2)
	}
	fmt.Println("GPU-11111111-1111-1111-1111-111111111111, NVIDIA GeForce RTX 2060, 7.5")
	os.Exit(0)
}

func commandSettings(t *testing.T) (config.Settings, []string, runtimeOptions) {
	t.Helper()
	c, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	var model bytes.Buffer
	for _, value := range []int32{0x67676d6c, 51865, 1500, 512, 8, 6, 448, 512, 8, 6, 80, 1} {
		if err := binary.Write(&model, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	model.WriteString("fixture tensor")
	path := filepath.Join(root, "PRIVATE-model.bin")
	if err := os.WriteFile(path, model.Bytes(), 0400); err != nil {
		t.Fatal(err)
	}

	c.Models = config.Models{
		Catalog:     []config.Model{{ID: "command-model", Source: config.Source{Kind: "prepared", Path: path}}},
		Served:      "command-model",
		DownloadSet: []string{},
	}
	c.Storage.CacheDir = filepath.Join(root, "cache")
	c.Timeouts.OperationSeconds, c.Timeouts.CleanupSeconds, c.Timeouts.ShutdownSeconds = 2, 1, 1
	c.Backend.Port, c.Backend.NativePort = commandPort(t), commandPort(t)
	for c.Backend.Port == c.Backend.NativePort {
		c.Backend.NativePort = commandPort(t)
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	environ := []string{}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "WHISPER_") {
			environ = append(environ, variable)
		}
	}

	environ = append(environ, "P02_COMMAND_CHILD=1", "P02_COMMAND_ROOT="+root, "GORACE=atexit_sleep_ms=0")
	runtime := runtimeOptions{
		nativeCommand: []string{os.Args[0], "-test.run=^TestCommandNative$", "--"},
		gpuCommand:    []string{os.Args[0], "-test.run=^TestCommandGPU$", "--"},
		workRoot:      filepath.Join(root, "work"),
	}

	return c, environ, runtime
}

func commandPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return port
}

func TestCommandValidation(t *testing.T) {
	c, environ, runtime := commandSettings(t)
	var logs bytes.Buffer
	err := run(
		context.Background(),
		[]string{
			"--config",
			filepath.Join(filepath.Dir(c.Models.Catalog[0].Source.Path), "config.json"),
			"--mode",
			"PRIVATE-invalid",
		},
		environ,
		&logs,
		runtime,
	)
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("invalid configuration accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(runtime.workRoot), "native-pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid configuration started native work")
	}
	checkCommandLogs(t, logs.Bytes(), "configuration_rejected")
}

func TestCommandListenerFailure(t *testing.T) {
	c, environ, runtime := commandSettings(t)
	l, err := net.Listen("tcp", ":"+strconv.Itoa(c.Backend.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	var logs bytes.Buffer
	err = run(
		context.Background(),
		[]string{"--config", filepath.Join(filepath.Dir(runtime.workRoot), "config.json")},
		environ,
		&logs,
		runtime,
	)
	var failure *domain.Failure
	if !errors.As(err, &failure) || failure.Kind != domain.BackendUnavailable {
		t.Fatalf("listener failure lost classification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(runtime.workRoot), "native-pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("occupied private port started native work")
	}
	checkCommandLogs(t, logs.Bytes(), "backend_failed")
}

func TestCommandProcess(t *testing.T) {
	for _, mode := range []string{"ready-sigterm", "gpu-failure"} {
		t.Run(mode, func(t *testing.T) {
			c, environ, runtime := commandSettings(t)
			if mode == "gpu-failure" {
				environ = append(environ, "P02_BAD_GPU=1")
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestCommandChild$")
			cmd.Env = environ
			var logs bytes.Buffer
			cmd.Stdout, cmd.Stderr = &logs, &logs
			preparedBefore, err := os.ReadFile(c.Models.Catalog[0].Source.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}

			result := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				result <- cmd.Wait()
				close(done)
			}()
			t.Cleanup(func() {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					_ = cmd.Process.Kill()
					<-done
				}
				if data, err := os.ReadFile(filepath.Join(filepath.Dir(runtime.workRoot), "native-pid")); err == nil {
					if pid, err := strconv.Atoi(string(data)); err == nil {
						_ = syscall.Kill(-pid, syscall.SIGKILL)
					}
				}
			})
			if mode == "ready-sigterm" {
				client := &http.Client{Timeout: 100 * time.Millisecond}
				deadline := time.Now().Add(5 * time.Second)
				for {
					select {
					case err := <-result:
						t.Fatalf("command exited before readiness: %v; %s", err, logs.Bytes())
					default:
					}

					response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(c.Backend.Port) + "/health")
					if err == nil {
						var health struct {
							Model       string `json:"model"`
							Fingerprint string `json:"configuration_fingerprint"`
							CUDAReady   bool   `json:"cuda_ready"`
						}
						decodeErr := json.NewDecoder(response.Body).Decode(&health)
						_ = response.Body.Close()
						if response.StatusCode == 200 {
							fingerprint, _ := config.CanonicalFingerprint(c)
							if decodeErr != nil || health.Model != c.Models.Served || health.Fingerprint != fingerprint || !health.CUDAReady {
								t.Fatal("command did not wire the prepared model/fingerprint/GPU")
							}
							break
						}
					}
					if time.Now().After(deadline) {
						t.Fatal("command never became ready")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}

			select {
			case err := <-result:
				if (err == nil) != (mode == "ready-sigterm") {
					t.Fatalf("wrong process exit: %v; %s", err, logs.Bytes())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command did not stop within its budgets")
			}

			pidData, err := os.ReadFile(filepath.Join(filepath.Dir(runtime.workRoot), "native-pid"))
			if err != nil {
				t.Fatal("native fixture was not started", err)
			}

			pid, _ := strconv.Atoi(string(pidData))
			if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("native group survived command exit", err)
			}

			files, _ := filepath.Glob(filepath.Join(runtime.workRoot, "run-*"))
			if len(files) != 0 {
				t.Fatal("command retained owned scratch")
			}

			preparedAfter, err := os.ReadFile(c.Models.Catalog[0].Source.Path)
			if err != nil || !bytes.Equal(preparedBefore, preparedAfter) {
				t.Fatal("prepared model was changed", err)
			}

			event := "backend_stopped"
			if mode == "gpu-failure" {
				event = "backend_failed"
			}
			checkCommandLogs(t, logs.Bytes(), event)
		})
	}
}

func checkCommandLogs(t *testing.T, data []byte, event string) {
	t.Helper()
	if bytes.Contains(data, []byte("PRIVATE")) {
		t.Fatal("private marker leaked to command logs")
	}

	found := false
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var line map[string]any
		err := decoder.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal("command output is not JSON", err)
		}
		if line["event"] == event {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing lifecycle event %s: %s", event, data)
	}
}
