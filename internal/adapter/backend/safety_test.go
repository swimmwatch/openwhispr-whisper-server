package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestSupervisorOptionBounds(t *testing.T) {
	for _, field := range []string{"wave", "prompt"} {
		t.Run(field, func(t *testing.T) {
			opts, _, _ := supervisorOptions(t)
			if field == "wave" {
				opts.MaxWAVBytes = math.MaxInt64
			} else {
				opts.PromptBytes = math.MaxInt
			}

			_, err := New(opts)
			requireKind(t, err, domain.InvalidInput)
		})
	}
}

type pendingDeadline struct {
	context.Context
	deadline time.Time
}

func (c pendingDeadline) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func TestElapsedReadDeadline(t *testing.T) {
	// Socket deadlines may wake a reader before the context timer is delivered.
	op := &operation{
		ctx:    pendingDeadline{context.Background(), time.Now().Add(-time.Second)},
		parent: context.Background(),
		cancel: func() {
		},
		unlink: func() bool {
			return true
		},
		finished: make(chan struct{}),
	}

	s := &Supervisor{active: op, status: domain.BackendBusy}

	err := s.finish(op, failure(domain.InvalidInput, os.ErrDeadlineExceeded), nil)
	requireKind(t, err, domain.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline cause lost")
	}
}

type abortBody struct{}

func (abortBody) Read([]byte) (int, error) {
	panic(http.ErrAbortHandler)
}

func TestPrivateAbortCleanup(t *testing.T) {
	opts, _, _ := supervisorOptions(t)
	s := runningSupervisor(t, opts)
	r := privateRequest(t, s, "http://private")

	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()

	end := bytes.LastIndex(data, []byte("\r\n--"))
	if end < 0 {
		t.Fatal("missing multipart fixture boundary")
	}

	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data[:end]), abortBody{}))

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	}()

	if recovered != http.ErrAbortHandler {
		t.Error("private adapter swallowed HTTP abort")
	}

	files, _ := filepath.Glob(filepath.Join(opts.WorkRoot, "run-*", "request-*.wav"))
	if len(files) != 0 {
		t.Error("aborted multipart retained owned audio")
	}

	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		target, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if strings.HasPrefix(target, opts.WorkRoot+string(filepath.Separator)) && strings.Contains(target, "request-") {
			t.Error("aborted multipart retained an open audio descriptor")
		}
	}

	if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
		t.Fatal("abort left backend unusable", err)
	}
}
