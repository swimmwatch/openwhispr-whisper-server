package acceptance

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestLocalQueuedOperationDeadline(t *testing.T) {
	e := openLocalEnvironment(t, func(s *config.Settings) { s.Timeouts.OperationSeconds = 1; s.Queue.WaitingCapacity = 2 })
	// The client observes the complete documented cleanup window; this does not
	// extend the actual service's one-second reduced operation deadline.
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	longest := readLocalAudio(t, filepath.Join(e.receipt.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
	active := submitIdentified(t, e, ctx, "expiry-active", longest, "longest.flac")
	waitLocalQueueEvent(t, e, "queue_dispatched", "expiry-active")
	waiting := submitIdentified(t, e, ctx, "expiry-waiting", e.audio, "jfk.wav")
	waitLocalQueueEvent(t, e, "queue_admitted", "expiry-waiting")
	for _, done := range []<-chan asyncPublicReply{active, waiting} {
		select {
		case result := <-done:
			if result.err != nil || result.reply.Status != http.StatusRequestTimeout || result.reply.ErrorCode != "operation_timeout" {
				t.Fatal("reduced active/queued operation did not return its canonical deadline failure")
			}
		case <-time.After(time.Duration(e.settings.Timeouts.CleanupSeconds+e.settings.Timeouts.OperationSeconds+5) * time.Second):
			t.Fatal("deadline workload exceeded its declared operation plus cleanup observation window")
		}
	}
	waitLifecycleHealthy(t, e)
	logs, err := e.target.Logs(e.ctx, "api")
	if err != nil {
		t.Fatal(err)
	}
	ids := observedQueueIDs(logs, "queue_dispatched", "expiry-")
	if len(ids) != 1 || ids[0] != "expiry-active" {
		t.Fatal("expired waiting work entered native inference")
	}
	state, err := e.target.Observe(e.ctx)
	if err != nil || releasedAPIResources(state) != nil {
		t.Fatal("operation expiry retained API request resources")
	}
}

func TestLocalActiveAPIShutdown(t *testing.T) {
	for _, mode := range []string{"graceful_term", "forced_kill"} {
		t.Run(mode, func(t *testing.T) {
			e := openLocalEnvironment(t, nil)
			before := readyLifecycleSnapshot(t, e)
			finishTrace := watchLifecycleReplacement(t, e, before)
			longest := readLocalAudio(t, filepath.Join(e.receipt.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
			active := startLifecycleCall(t, e, longest, "longest.flac")
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
		})
	}
}
