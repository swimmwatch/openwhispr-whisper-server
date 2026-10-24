package backend

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateMultipartFieldLimits(t *testing.T) {
	opts, root, _ := supervisorOptions(t)
	opts.PromptBytes = 8
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for _, scenario := range []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "prompt-limit",
			key:   "prompt",
			value: strings.Repeat("x", opts.PromptBytes+1),
		},
		{
			name:  "language-limit",
			key:   "language",
			value: strings.Repeat("x", maxMultipartFieldBytes+1),
		},
		{
			name:  "temperature-syntax",
			key:   "temperature",
			value: "PRIVATE-invalid",
		},
		{
			name:  "response-format",
			key:   "response_format",
			value: "PRIVATE-invalid",
		},
		{
			name:  "missing-file",
			key:   "language",
			value: "en",
		},
		{
			name:  "incomplete-boundary",
			key:   "language",
			value: "en",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			if scenario.name != "missing-file" {
				part, err := form.CreateFormFile("file", "audio.wav")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(wavFixture()); err != nil {
					t.Fatal(err)
				}
			}
			if err := form.WriteField(scenario.key, scenario.value); err != nil {
				t.Fatal(err)
			}
			if scenario.name != "incomplete-boundary" {
				if err := form.Close(); err != nil {
					t.Fatal(err)
				}
			}

			r := privateRequest(t, s, server.URL)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
			r.ContentLength = int64(body.Len())
			r.Header.Set("Content-Type", form.FormDataContentType())

			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}

			data, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != http.StatusBadRequest || strings.Contains(string(data), "PRIVATE") {
				t.Fatalf("invalid private fields accepted/leaked: status=%d body=%q err=%v", response.StatusCode, data, err)
			}
			if files, err := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav")); err != nil || len(files) != 0 {
				t.Fatal("field rejection retained staged audio", err)
			}
		})
	}

	last, _ := s.Health(context.Background())
	if !last.CUDAReady || last.ProcessGeneration != first.ProcessGeneration || len(starts(t, root)) != 1 {
		t.Fatal("field rejection changed the resident child")
	}
	if calls, err := filepath.Glob(filepath.Join(root, "call-*")); err != nil || len(calls) != 0 {
		t.Fatal("invalid private fields reached native inference", err)
	}
}
