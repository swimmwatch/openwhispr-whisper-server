package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
)

type panicUpload struct{}

func (panicUpload) Read([]byte) (int, error) {
	panic("PRIVATE upload panic")
}

func TestPrivacyCleanup(t *testing.T) {
	for _, scenario := range []string{"body-panic", "service-panic", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			s := &transcriptionStub{}
			e, opts, logs := transcriptionFixture(t, s, nil)
			r := formRequest(t, [][2]string{{"model", "base"}}, []byte("PRIVATE audio"))
			if scenario == "body-panic" {
				data, _ := io.ReadAll(r.Body)
				cut := bytes.Index(data, []byte("PRIVATE audio")) + 7
				if cut < 7 {
					t.Fatal("missing upload fixture")
				}

				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data[:cut]), panicUpload{}))
			}
			if scenario == "service-panic" {
				s.admit = func(context.Context, transcription.Input) (transcription.Result, error) {
					panic("PRIVATE service panic")
				}
			}
			if scenario == "malformed" {
				data, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(data[:len(data)/2]))
			}

			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			if w.Code < http.StatusBadRequest || strings.Contains(w.Body.String()+logs.String(), "PRIVATE") {
				t.Fatalf("failure exposed content or fabricated success: %d %s", w.Code, w.Body.String())
			}

			files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
			if len(files) != 0 {
				t.Fatal("failed upload retained its owned session")
			}

			fds, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}

			for _, fd := range fds {
				path, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
				if strings.HasPrefix(path, opts.WorkRoot+"/run-") &&
					(strings.HasSuffix(path, "/upload") ||
						strings.HasSuffix(path, "/upload (deleted)")) {
					t.Fatal("failed upload retained an open audio descriptor", filepath.Base(path))
				}
			}
		})
	}
}

func TestPublicStalledUpload(t *testing.T) {
	s := &transcriptionStub{}
	e, opts, _ := transcriptionFixture(t, s, func(o *Options) {
		o.OperationTimeout = 100 * time.Millisecond
	})
	server := httptest.NewServer(e)
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, err = fmt.Fprint(conn, "POST /v1/audio/transcriptions HTTP/1.1\r\nHost: test\r\nContent-Type: multipart/form-data; boundary=stalled\r\nContent-Length: 10000\r\n\r\n--stalled\r\nContent-Disposition: form-data; name=\"file\"; filename=\"audio.wav\"\r\n\r\npartial")
	if err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	data, readErr := io.ReadAll(conn)
	if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
		t.Fatal("upload timeout left a live stalled connection")
	}
	successStatus := fmt.Sprintf("%d %s", http.StatusOK, http.StatusText(http.StatusOK))
	if bytes.Contains(data, []byte(successStatus)) || s.calls.Load() != 0 {
		t.Fatal("stalled upload fabricated success or reached admission")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Config.Shutdown(ctx); err != nil {
		t.Fatal("stalled upload blocked HTTP shutdown", err)
	}

	files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*"))
	if len(files) != 0 {
		t.Fatal("stalled upload retained scratch")
	}
}

func TestAbandonedUploads(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	stale := filepath.Join(root, "run-"+strings.Repeat("A", 26))
	other := filepath.Join(root, "run-"+strings.Repeat("B", 26))
	for _, dir := range []string{stale, other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "PRIVATE"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stale, ".owner"), []byte(uploadMarker), 0600); err != nil {
		t.Fatal(err)
	}

	s := &transcriptionStub{}
	_, _, _ = transcriptionFixture(t, s, func(o *Options) {
		o.WorkRoot = root
	})
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("abandoned owned upload was retained", err)
	}
	if _, err := os.Stat(filepath.Join(other, "PRIVATE")); err != nil {
		t.Fatal("unrelated data was removed", err)
	}
}
