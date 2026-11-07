package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const residentNativeTestSpeech = "My fellow Americans, ask what you can do for your country."

// Only the Docker execution boundary is replaced. The real installed curl owns
// multipart encoding, HTTP transfer, response framing and its own timeout.
func residentNativeCurlFixture(t *testing.T) (*DockerTarget, string) {
	t.Helper()
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatal("native comparison CPU checks require an existing curl executable")
	}
	root := t.TempDir()
	prepared := filepath.Join(root, "prepared")
	if err := os.Mkdir(prepared, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P07_NATIVE_REAL_CURL", curl)
	// A task-local curlrc would change the verb/replay policy if --disable were
	// not the first curl option. This exercises behavior, not an argument string.
	t.Setenv("CURL_HOME", root)
	if err := os.WriteFile(filepath.Join(root, ".curlrc"), []byte("request = GET\nlocation\nretry = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "fake-docker")
	script := `#!/bin/bash
set -euo pipefail
if [[ "$1" == compose ]]; then
  printf 'down\n' >> "$WHISPER_PREPARED_DIR/lifecycle"
  exit "${P07_NATIVE_DOWN_EXIT:-0}"
fi
[[ "$1" == exec && "$2" == owned-native-backend && "$3" == curl ]] || exit 7
shift 3
args=()
for value in "$@"; do
  case "$value" in
    file=@/prepared/*) value="file=@$WHISPER_PREPARED_DIR/${value#file=@/prepared/}" ;;
    @/prepared/*) value="@$WHISPER_PREPARED_DIR/${value#@/prepared/}" ;;
  esac
  args+=("$value")
done
exec "$P07_NATIVE_REAL_CURL" "${args[@]}"
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return &DockerTarget{opts: DockerOptions{DockerExecutable: executable, Root: root, PreparedDir: prepared, CommandTimeout: time.Second, StopTimeoutSeconds: 1}}, prepared
}

func residentNativeTestPort(t *testing.T, address string) int {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestResidentNativeCurlUsesNativeMultipartAndOracle(t *testing.T) {
	target, prepared := residentNativeCurlFixture(t)
	payload := []byte("synthetic public PCM transport fixture; not decoding evidence")
	if err := os.WriteFile(filepath.Join(prepared, "native.wav"), payload, 0644); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/inference" || r.Method != http.MethodPost || r.Header.Get("X-Whisper-Protocol") != "" || r.Header.Get("Expect") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(4096); err != nil {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		defer r.MultipartForm.RemoveAll()
		expected := map[string]string{"language": "en", "prompt": "", "temperature": "0", "carry_initial_prompt": "false", "response_format": "json"}
		if len(r.MultipartForm.Value) != len(expected) || len(r.MultipartForm.File) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for name, value := range expected {
			values := r.MultipartForm.Value[name]
			if len(values) != 1 || values[0] != value {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil || header.Filename != "audio.wav" || header.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(body, payload) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": residentNativeTestSpeech})
	}))
	defer server.Close()
	result, err := residentNativeSample(t.Context(), target, "owned-native-backend", residentNativeTestPort(t, server.URL), "native.wav", time.Second)
	if err != nil || requests.Load() != 1 || result.TranscriptCharacters != len(residentNativeTestSpeech) || result.TranscriptSHA256 != strings.TrimPrefix(acceptanceSHA256([]byte(residentNativeTestSpeech)), "sha256:") || result.Duration <= 0 || !result.Speech.Fellow {
		t.Fatalf("real curl native multipart/oracle failed: success=%t requests=%d", err == nil, requests.Load())
	}
	data, err := json.Marshal(result)
	if err != nil || strings.Contains(string(data), residentNativeTestSpeech) || strings.Contains(string(data), prepared) {
		t.Fatal("native observation retained raw response or private mount path")
	}
}

func TestResidentNativeCurlRejectsFailedResponsesWithoutRetry(t *testing.T) {
	for _, mode := range []string{"http", "redirect", "wrong-media", "malformed", "trailing-json", "empty", "unrecognized", "truncated", "overflow", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			target, prepared := residentNativeCurlFixture(t)
			if err := os.WriteFile(filepath.Join(prepared, "native.wav"), []byte("synthetic transport fixture"), 0644); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				body := `{"text":"` + residentNativeTestSpeech + `"}`
				switch mode {
				case "http":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "redirect":
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "wrong-media":
					w.Header().Set("Content-Type", "text/plain")
				case "malformed":
					body = `{"text":123}`
				case "trailing-json":
					body += `{}`
				case "empty":
					body = `{"text":" "}`
				case "unrecognized":
					body = `{"text":"unrelated speech"}`
				case "truncated":
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+10))
				case "overflow":
					body = strings.Repeat("PRIVATE_RESPONSE_MARKER", dockerOutputLimit)
				case "timeout":
					<-r.Context().Done()
					return
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			budget := time.Second
			if mode == "timeout" {
				budget = 150 * time.Millisecond
			}
			result, err := residentNativeSample(t.Context(), target, "owned-native-backend", residentNativeTestPort(t, server.URL), "native.wav", budget)
			if mode == "unrecognized" && (result.HTTPStatus == nil || *result.HTTPStatus != http.StatusOK || !result.SpeechObserved || result.FailureStage != "speech" || result.TranscriptCharacters != len("unrelated speech") || result.TranscriptSHA256 != strings.TrimPrefix(acceptanceSHA256([]byte("unrelated speech")), "sha256:")) {
				t.Fatal("failed recognition discarded actual sanitized transcript facts")
			}
			if err == nil || requests.Load() != 1 || strings.Contains(err.Error(), "PRIVATE_") || strings.Contains(err.Error(), residentNativeTestSpeech) || strings.Contains(err.Error(), prepared) {
				t.Fatalf("failed native result accepted/retried/leaked: failed=%t requests=%d", err != nil, requests.Load())
			}
		})
	}
}

func TestResidentNativeSeriesStopsOwnedPairBeforeReturningFirstFailure(t *testing.T) {
	for _, downFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(downFails), func(t *testing.T) {
			target, prepared := residentNativeCurlFixture(t)
			target.owned = true
			if downFails {
				t.Setenv("P07_NATIVE_DOWN_EXIT", "7")
			}
			if err := os.WriteFile(filepath.Join(prepared, "native.wav"), []byte("synthetic public transport fixture"), 0644); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			series, err := collectResidentNativeSeries(t.Context(), target, "owned-native-backend", residentNativeTestPort(t, server.URL), "native.wav", 11*time.Second, time.Second, 30)
			lifecycle, readErr := os.ReadFile(filepath.Join(prepared, "lifecycle"))
			if err == nil || requests.Load() != 1 || len(series.Samples) != 1 || series.Successes != 0 || series.Errors != 1 || !series.TeardownAttempted || series.TeardownSucceeded == downFails || target.owned != downFails || readErr != nil || string(lifecycle) != "down\n" {
				t.Fatal("native failure retried, returned before owned teardown, or hid teardown failure")
			}
		})
	}
}

func TestResidentNativeSeriesUsesSerialNativeOnlyRequests(t *testing.T) {
	target, prepared := residentNativeCurlFixture(t)
	target.owned = true
	if err := os.WriteFile(filepath.Join(prepared, "native.wav"), []byte("synthetic public transport fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	var active, peak, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		if current > peak.Load() {
			peak.Store(current)
		}
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path != "/inference" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": residentNativeTestSpeech})
	}))
	defer server.Close()
	series, err := collectResidentNativeSeries(t.Context(), target, "owned-native-backend", residentNativeTestPort(t, server.URL), "native.wav", 11*time.Second, time.Second, 3)
	if err != nil || requests.Load() != 3 || peak.Load() != 1 || series.Successes != 3 || series.OracleSuccesses != 3 || series.Errors != 0 || series.HTTPWall == nil || series.HTTPWall.Count != 3 || series.HTTPWallAudioRatio == nil || series.HTTPWallAudioRatio.Count != 3 || series.TeardownAttempted || !target.owned {
		t.Fatal("native comparison lost serial sampling, oracle evidence or distinct HTTP-wall scope")
	}
	if _, err := os.Stat(filepath.Join(prepared, "lifecycle")); !os.IsNotExist(err) {
		t.Fatal("successful native series unexpectedly stopped the owned target")
	}
}

func TestResidentNativeExpiredBudgetNeverStartsCurl(t *testing.T) {
	target, _ := residentNativeCurlFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := residentNativeSample(ctx, target, "owned-native-backend", 8082, "native.wav", time.Second); err == nil {
		t.Fatal("cancelled native call unexpectedly succeeded")
	}
}
