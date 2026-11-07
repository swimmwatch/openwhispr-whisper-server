package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestMultipartCleanupSharesTerminalResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "removed"
		if failed {
			name = "owner-closed"
		}
		t.Run(name, func(t *testing.T) {
			handler, err := New(handlerOptions(t), &transcriptionStub{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := handler.Close(); err != nil {
					t.Error(err)
				}
			})
			request := formRequest(t, [][2]string{{"model", "base"}}, []byte("audio"))
			input, cleanup, err := handler.ReadMultipart(context.Background(), httptest.NewRecorder(), request)
			if err != nil {
				t.Fatal(err)
			}
			if failed {
				// Simulate teardown of the filesystem owner before its external
				// cleanup callback is invoked. The callback must cache the failure.
				if err := handler.Close(); err != nil {
					t.Fatal(err)
				}
			}
			const callers = 8
			results := make(chan error, callers)
			for range callers {
				go func() { results <- cleanup() }()
			}
			first := <-results
			for range callers - 1 {
				if err := <-results; err != first {
					t.Fatal("concurrent cleanup did not preserve its first result")
				}
			}
			if failed {
				var failure *domain.Failure
				if !errors.As(first, &failure) || failure.Kind != domain.Internal {
					t.Fatal("closed owner cleanup lost its classified failure", first)
				}
			} else {
				if first != nil {
					t.Fatal(first)
				}
				if _, err := os.Stat(input.UploadPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("successful cleanup retained its owned upload", err)
				}
			}
			if err := cleanup(); err != first {
				t.Fatal("later cleanup changed the cached result")
			}
		})
	}
}
