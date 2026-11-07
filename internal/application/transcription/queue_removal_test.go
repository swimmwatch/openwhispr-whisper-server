package transcription

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestMiddleWaitingCancellationPreservesFIFO(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan string, 4)
		release := make(chan struct{})
		defer closeBarrier(release)
		backend := &testBackend{health: readyHealth(), infer: func(_ context.Context, input Normalized) (Result, error) {
			entered <- input.RequestID
			if input.RequestID == "active" {
				<-release
			}
			return Result{Text: input.RequestID}, nil
		}}
		opts := serviceOptions()
		opts.WaitingCapacity = 3
		service, normalizer := newTestService(t, opts, backend)
		awaitReady(t, service)
		active := submit(service, context.Background(), "active")
		if <-entered != "active" {
			t.Fatal("first request did not dispatch")
		}
		first := submit(service, context.Background(), "first")
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		middle := submit(service, ctx, "middle")
		synctest.Wait()
		last := submit(service, context.Background(), "last")
		synctest.Wait()
		cancel()
		requireFailure(t, <-middle, domain.DeadlineExceeded)
		normalizer.released(t, "middle")
		replacement := submit(service, context.Background(), "replacement")
		synctest.Wait()
		select {
		case err := <-replacement:
			t.Fatal("cancelled middle request retained queue capacity", err)
		default:
		}
		close(release)
		for _, result := range []<-chan error{active, first, last, replacement} {
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		}
		for _, want := range []string{"first", "last", "replacement"} {
			if got := <-entered; got != want {
				t.Fatalf("middle cancellation changed FIFO: got %q, want %q", got, want)
			}
		}
		if backend.calls.Load() != 4 {
			t.Fatal("cancelled request dispatched or live request was lost")
		}
		normalizer.released(t, "active", "first", "last", "replacement")
	})
}
