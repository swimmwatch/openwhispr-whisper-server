package acceptance

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestLocalQueuedOperationDeadline(t *testing.T) {
	runQueuedOperationDeadline(t, openLocalEnvironment(t, func(s *config.Settings) { s.Timeouts.OperationSeconds = 1; s.Queue.WaitingCapacity = 2 }))
}

func runQueuedOperationDeadline(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	runQueuedDeadlineWorkload(t, queueWorkload{
		ctx: e.ctx, model: e.settings.Models.Served, short: e.audio,
		long:   readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes),
		client: func(t *testing.T, id string) *PublicClient { return identifiedLocalClient(t, e, id) },
		logs:   func(ctx context.Context) ([]byte, error) { return e.target.Logs(ctx, "api") },
		released: func(ctx context.Context) error {
			state, err := e.target.Observe(ctx)
			if err != nil {
				return err
			}
			return releasedAPIResources(state)
		},
	}, time.Duration(e.settings.Timeouts.CleanupSeconds+e.settings.Timeouts.OperationSeconds+5)*time.Second, func() { waitLifecycleHealthy(t, e) })
}

func runQueuedDeadlineWorkload(t *testing.T, work queueWorkload, observationWindow time.Duration, recoverHealth func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(work.ctx)
	defer cancel()
	active := submitPublicRequest(ctx, work.client(t, "expiry-active"), work.model, work.long, "longest.flac")
	waitQueueEvent(t, ctx, work.logs, "queue_dispatched", "expiry-active")
	waiting := submitPublicRequest(ctx, work.client(t, "expiry-waiting"), work.model, work.short, "jfk.wav")
	waitQueueEvent(t, ctx, work.logs, "queue_admitted", "expiry-waiting")
	for _, done := range []<-chan asyncPublicReply{active, waiting} {
		select {
		case result := <-done:
			if result.err != nil || result.reply.Status != http.StatusRequestTimeout || result.reply.ErrorCode != "operation_timeout" {
				t.Fatal("reduced active/queued operation did not return its canonical deadline failure")
			}
		case <-time.After(observationWindow):
			t.Fatal("deadline workload exceeded its declared operation plus cleanup observation window")
		case <-ctx.Done():
			t.Fatal("deadline workload cancelled")
		}
	}
	recoverHealth()
	logs, err := work.logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDeadlineQueueOrder(logs); err != nil {
		t.Fatal(err)
	}
	if work.released(ctx) != nil {
		t.Fatal("operation expiry retained API request resources or observation was unavailable")
	}
}

func validateDeadlineQueueOrder(logs []byte) error {
	admitted := observedQueueIDs(logs, "queue_admitted", "expiry-")
	dispatched := observedQueueIDs(logs, "queue_dispatched", "expiry-")
	if len(admitted) != 2 || admitted[0] != "expiry-active" || admitted[1] != "expiry-waiting" ||
		len(dispatched) != 1 || dispatched[0] != "expiry-active" {
		return errors.New("deadline admission or dispatch order was incorrect")
	}
	return nil
}

func TestLocalActiveAPIShutdown(t *testing.T) {
	for _, mode := range []string{"graceful_term", "forced_kill"} {
		t.Run(mode, func(t *testing.T) { runActiveAPIShutdown(t, openLocalEnvironment(t, nil), mode) })
	}
}

func runActiveAPIShutdown(t *testing.T, e *acceptanceEnvironment, mode string) {
	t.Helper()
	before := readyLifecycleSnapshot(t, e)
	finishTrace := watchLifecycleReplacement(t, e, before)
	longest := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
	active := startLifecycleCall(t, e, before, longest, "longest.flac")
	waitLifecycleBusy(t, e, active, before.PrivateHealth.ProcessGeneration)
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	waiting := submitIdentified(t, e, ctx, "shutdown-waiting", e.audio, "jfk.wav")
	waitLocalQueueEvent(t, e, "queue_admitted", "shutdown-waiting")
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	signal := "SIGTERM"
	if mode == "forced_kill" {
		signal = "SIGKILL"
	}
	started := time.Now()
	if _, err := e.target.run(e.ctx, "active API termination", false, "kill", "--signal", signal, containers.APIID); err != nil {
		t.Fatal(err)
	}
	if err := waitLifecycleUnready(e.ctx, e.address, true, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	activeResult := awaitLifecycleCall(t, active, time.Duration(e.settings.Timeouts.ShutdownSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second)
	var queued asyncPublicReply
	select {
	case queued = <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting owner did not terminate with API shutdown")
	}
	if mode == "graceful_term" {
		if activeResult.err != nil || activeResult.reply.Status != http.StatusOK {
			t.Fatal("graceful API termination failed to drain its active operation")
		}
		if queued.err != nil || queued.reply.Status != http.StatusServiceUnavailable || queued.reply.ErrorCode != "shutting_down" {
			t.Fatal("graceful API termination did not reject its waiting owner")
		}
	} else if activeResult.err == nil && activeResult.reply.Status == http.StatusOK || queued.err == nil && queued.reply.Status == http.StatusOK {
		t.Fatal("forced API death returned false successful work")
	}
	deadline := started.Add(time.Duration(e.settings.Timeouts.ShutdownSeconds+e.settings.Timeouts.CleanupSeconds+5) * time.Second)
	for {
		state, err := e.target.Inspect(e.ctx, "api")
		if err != nil {
			t.Fatal(err)
		}
		if !state.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("API did not stop within configured shutdown plus cleanup")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := e.target.Restart(e.ctx, "api"); err != nil {
		t.Fatal(err)
	}
	if err := refreshLifecycleClient(e.ctx, e); err != nil {
		t.Fatal(err)
	}
	waitLifecycleHealthy(t, e)
	after := readyLifecycleSnapshot(t, e)
	if mode == "graceful_term" && (*after.NativeHostPID != *before.NativeHostPID || after.PrivateHealth.ProcessGeneration != before.PrivateHealth.ProcessGeneration) {
		t.Fatal("graceful API drain replaced its healthy backend model")
	}
	trace := finishTrace()
	lifecycleTracePassed(t, trace, mode == "forced_kill")
	if err := releasedAPIResources(after); err != nil {
		t.Fatal(err)
	}
	lifecycleReport(t, e, "active-api-"+mode, map[string]any{"operation_seconds": e.settings.Timeouts.OperationSeconds, "shutdown_seconds": e.settings.Timeouts.ShutdownSeconds, "cleanup_seconds": e.settings.Timeouts.CleanupSeconds, "active_status": activeResult.reply.Status, "queued_status": queued.reply.Status, "active_transport_error": activeResult.err != nil, "queued_transport_error": queued.err != nil}, &trace)
}
