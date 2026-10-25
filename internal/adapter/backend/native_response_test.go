package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestNativeResponseValidation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		body   string
		short  bool
		valid  bool
	}{
		{
			name:   "empty-text",
			status: http.StatusOK,
			body:   `{"text":""}`,
			valid:  true,
		},
		{
			name:   "status",
			status: http.StatusServiceUnavailable,
			body:   "PRIVATE native failure",
		},
		{
			name:   "malformed",
			status: http.StatusOK,
			body:   "PRIVATE native failure",
		},
		{
			name:   "missing-text",
			status: http.StatusOK,
			body:   `{}`,
		},
		{
			name:   "null-text",
			status: http.StatusOK,
			body:   `{"text":null}`,
		},
		{
			name:   "wrong-text-type",
			status: http.StatusOK,
			body:   `{"text":123}`,
		},
		{
			name:   "invalid-utf8",
			status: http.StatusOK,
			body:   "{\"text\":\"\xff\"}",
		},
		{
			name:   "oversize",
			status: http.StatusOK,
			body:   `{"text":"` + strings.Repeat("x", 128) + `"}`,
		},
		{
			name:   "interrupted",
			status: http.StatusOK,
			body:   `{"text":"ok"}`,
			short:  true,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			wave := wavFixture()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/inference" || r.Method != http.MethodPost {
					t.Error("unexpected native route")
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()

				file, _, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()

				data, err := io.ReadAll(file)
				if err != nil || !bytes.Equal(data, wave) {
					t.Error("native request changed normalized audio", err)
				}

				expectedFields := map[string]string{
					"language":             "en",
					"prompt":               "PRIVATE prompt",
					"temperature":          "0.25",
					"carry_initial_prompt": "false",
					"response_format":      "json",
				}
				if len(r.MultipartForm.Value) != len(expectedFields) {
					t.Error("native fields differ from the fixed allowlist")
				}
				for key, expected := range expectedFields {
					values := r.MultipartForm.Value[key]
					if len(values) != 1 || values[0] != expected {
						t.Errorf("unexpected native field %q", key)
					}
				}

				if scenario.short {
					w.Header().Set("Content-Length", "1000")
				}
				w.WriteHeader(scenario.status)
				_, _ = io.WriteString(w, scenario.body)
			}))
			defer server.Close()

			opts, _, _ := supervisorOptions(t)
			opts.NativePort = server.Listener.Addr().(*net.TCPAddr).Port
			opts.MaxReplyBytes = 64

			s, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer s.transport.CloseIdleConnections()

			request := Request{
				Audio: bytes.NewReader(wave),
				Size:  int64(len(wave)),
				Fields: domain.InferenceFields{
					Language:    "en",
					Prompt:      "PRIVATE prompt",
					Temperature: 0.25,
				},
			}

			result, err := s.callNative(&operation{ctx: context.Background()}, request)
			if scenario.valid {
				if err != nil || result.Text != "" {
					t.Fatalf("valid empty transcript rejected: %+v %v", result, err)
				}
			} else {
				requireKind(t, err, domain.BackendUnavailable)
				if result.Text != "" {
					t.Fatal("failed native reply returned transcript")
				}
			}
		})
	}
}

func TestInvalidCanonicalWAVPreservesResidentChild(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())

	for _, scenario := range []struct {
		name   string
		offset int
		value  uint32
	}{
		{
			name:   "riff-magic",
			offset: 0,
			value:  0,
		},
		{
			name:   "riff-size",
			offset: 4,
			value:  0,
		},
		{
			name:   "format-chunk-size",
			offset: 16,
			value:  18,
		},
		{
			name:   "stereo",
			offset: 22,
			value:  2,
		},
		{
			name:   "sample-rate",
			offset: 24,
			value:  44100,
		},
		{
			name:   "byte-rate",
			offset: 28,
			value:  64000,
		},
		{
			name:   "block-alignment",
			offset: 32,
			value:  4,
		},
		{
			name:   "data-size",
			offset: 40,
			value:  0,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			wave := wavFixture()
			if scenario.offset == 22 || scenario.offset == 32 {
				binary.LittleEndian.PutUint16(wave[scenario.offset:], uint16(scenario.value))
			} else {
				binary.LittleEndian.PutUint32(wave[scenario.offset:], scenario.value)
			}

			request := requestFor(s, "")
			request.Audio = bytes.NewReader(wave)
			_, err := s.Infer(context.Background(), request)
			requireKind(t, err, domain.UnsupportedAudio)
		})
	}

	last, _ := s.Health(context.Background())
	if !last.CUDAReady || last.ProcessGeneration != first.ProcessGeneration || len(starts(t, root)) != 1 {
		t.Fatal("invalid WAV changed resident child")
	}
	if files, err := filepath.Glob(filepath.Join(root, "call-*")); err != nil || len(files) != 0 {
		t.Fatal("invalid WAV reached native inference", err)
	}
	if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
		t.Fatal("invalid WAV left the admission slot occupied", err)
	}
}

func TestInvalidSupervisorOptionsHaveNoSideEffects(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(*Options)
	}{
		{
			name: "missing-command",
			mutate: func(o *Options) {
				o.NativeCommand = nil
			},
		},
		{
			name: "missing-logger",
			mutate: func(o *Options) {
				o.Logger = nil
			},
		},
		{
			name: "port",
			mutate: func(o *Options) {
				o.NativePort = 65536
			},
		},
		{
			name: "relative-work-path",
			mutate: func(o *Options) {
				o.WorkRoot = "relative"
			},
		},
		{
			name: "invalid-fingerprint",
			mutate: func(o *Options) {
				o.Fingerprint = "PRIVATE-invalid"
			},
		},
		{
			name: "invalid-artifact",
			mutate: func(o *Options) {
				o.Artifact.Metadata.VocabularySize = 0
			},
		},
		{
			name: "missing-time-budget",
			mutate: func(o *Options) {
				o.CleanupTimeout = 0
			},
		},
		{
			name: "invalid-audio-bound",
			mutate: func(o *Options) {
				o.MaxWAVBytes = canonicalWAVMinimumBytes - 1
			},
		},
		{
			name: "model-inside-disposable-work",
			mutate: func(o *Options) {
				o.Artifact.Path = filepath.Join(o.WorkRoot, "PRIVATE-model.bin")
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opts, root, _ := supervisorOptions(t)
			work := opts.WorkRoot

			scenario.mutate(&opts)

			s, err := New(opts)
			requireKind(t, err, domain.InvalidInput)
			if s != nil {
				t.Fatal("invalid options returned a supervisor")
			}
			if _, err := os.Stat(work); !os.IsNotExist(err) {
				t.Fatal("invalid options created disposable work", err)
			}
			if len(starts(t, root)) != 0 {
				t.Fatal("invalid options started native work")
			}
		})
	}
}
