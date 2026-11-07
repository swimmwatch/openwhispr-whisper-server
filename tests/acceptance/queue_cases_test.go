package acceptance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Only these observations vary between Docker and parent-owned Kubernetes runs.
// Neither binding may replace unknown resource ownership with measured zero.
type queueWorkload struct {
	ctx            context.Context
	model          string
	short, long    []byte
	client         func(*testing.T, string) *PublicClient
	logs           func(context.Context) ([]byte, error)
	released       func(context.Context) error
	cleanupTimeout time.Duration
}

func runQueueWorkload(t *testing.T, work queueWorkload) {
	t.Helper()
	if work.cleanupTimeout <= 0 {
		t.Fatal("queue request cleanup budget is unavailable")
	}
	ctx, cancel := context.WithCancel(work.ctx)
	var pending []<-chan struct{}
	defer func() {
		cancel()
		timer := time.NewTimer(work.cleanupTimeout)
		defer timer.Stop()
		for _, done := range pending {
			select {
			case <-done:
			case <-timer.C:
				t.Error("queue request cleanup exceeded its budget")
				return
			}
		}
	}()
	submit := func(ctx context.Context, id string, audio []byte, filename string) <-chan asyncPublicReply {
		client := work.client(t, id)
		result := make(chan asyncPublicReply, 1)
		done := make(chan struct{})
		pending = append(pending, done)
		go func() {
			defer close(done)
			reply, err := client.Transcribe(ctx, audio, filename, []FormField{{"model", work.model}, {"language", "en"}, {"temperature", "0"}})
			result <- asyncPublicReply{reply, err}
		}()
		return result
	}
	wait := func(event, id string) { waitQueueEvent(t, ctx, work.logs, event, id) }
	active := submit(ctx, "fifo-active", work.long, "longest.flac")
	wait("queue_dispatched", "fifo-active")
	second := submit(ctx, "fifo-second", work.short, "jfk.wav")
	wait("queue_admitted", "fifo-second")
	third := submit(ctx, "fifo-third", work.short, "jfk.wav")
	wait("queue_admitted", "fifo-third")
	overflow := work.client(t, "fifo-overflow")
	reply, err := overflow.Transcribe(ctx, work.short, "jfk.wav", []FormField{{"model", work.model}})
	if err != nil || reply.Status != http.StatusTooManyRequests || reply.ErrorCode != "queue_full" {
		t.Fatal("declared waiting-capacity overflow did not exercise queue_full")
	}
	for _, done := range []<-chan asyncPublicReply{active, second, third} {
		select {
		case result := <-done:
			if result.err != nil || result.reply.Status != http.StatusOK {
				t.Fatal("admitted FIFO request did not complete")
			}
		case <-ctx.Done():
			t.Fatal("FIFO workload cancelled")
		}
	}
	logs, err := work.logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := observedQueueIDs(logs, "queue_dispatched", "fifo-")
	if len(ids) != 3 || ids[0] != "fifo-active" || ids[1] != "fifo-second" || ids[2] != "fifo-third" {
		t.Fatal("actual private-dispatch order differs from admitted FIFO order or overflow dispatched")
	}
	// A queued disconnect must remove its owner, not enter native inference later.
	active = submit(ctx, "cancel-queue-active", work.long, "longest.flac")
	wait("queue_dispatched", "cancel-queue-active")
	waitingCtx, stopWaiting := context.WithCancel(ctx)
	defer stopWaiting()
	waiting := submit(waitingCtx, "cancel-queue-waiting", work.short, "jfk.wav")
	wait("queue_admitted", "cancel-queue-waiting")
	stopWaiting()
	result := <-waiting
	if result.err == nil || !errors.Is(result.err, context.Canceled) {
		t.Fatal("queued disconnect did not preserve cancellation")
	}
	if result = <-active; result.err != nil || result.reply.Status != http.StatusOK {
		t.Fatal("queued disconnect disturbed active inference")
	}
	logs, err = work.logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range observedQueueIDs(logs, "queue_dispatched", "cancel-queue-") {
		if id == "cancel-queue-waiting" {
			t.Fatal("cancelled queued work entered native dispatch")
		}
	}
	if work.released(ctx) != nil {
		t.Fatal("FIFO/cancellation retained resources or observations were unavailable")
	}
}
