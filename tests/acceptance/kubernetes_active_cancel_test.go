package acceptance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This partial check observes supervisor state, not native PID retirement or GPU
// kernel activity. Sampling does not prove continuous state or qualify P08.
type activeCancelWork struct {
	model                            string
	observe                          func(context.Context) (*DockerPrivateHealth, error)
	verify                           func(context.Context) error
	logs                             func(context.Context) ([]byte, error)
	released                         func(context.Context) error
	ready                            func(context.Context) error
	start                            func(context.Context, string) <-chan asyncPublicReply
	speech                           func(context.Context) (PublicReply, error)
	fact                             func(string)
	id                               string
	operation, cleanup, provisioning time.Duration
}

func activeHealth(h *DockerPrivateHealth, state, model string) bool {
	return h != nil && h.HTTPStatus == http.StatusOK && h.Status == state && h.CUDAReady && h.Model == model && h.ProcessGeneration != "" && h.ProtocolVersion != "" && h.ConfigurationFingerprint != ""
}

func sameResident(a, b *DockerPrivateHealth) bool {
	return a != nil && b != nil && a.Model == b.Model && a.ProtocolVersion == b.ProtocolVersion && a.ConfigurationFingerprint == b.ConfigurationFingerprint
}

func activeCancelCheck(ctx context.Context, w activeCancelWork) (resultErr error) {
	if w.model == "" || w.id == "" || w.operation <= 0 || w.cleanup <= 0 || w.provisioning <= 0 || w.observe == nil || w.verify == nil || w.logs == nil || w.released == nil || w.ready == nil || w.start == nil || w.speech == nil {
		return errors.New("incomplete active cancellation observation")
	}
	if w.verify(ctx) != nil {
		return errors.New("selected target identity unavailable")
	}
	before, err := w.observe(ctx)
	if err != nil || !activeHealth(before, "ready", w.model) {
		return errors.New("initial ready CUDA resident unavailable")
	}
	record := func(fact string) {
		if w.fact != nil {
			w.fact(fact)
		}
	}
	record("selected target and initial CUDA-ready resident observed")
	requestCtx, cancel := context.WithCancel(ctx)
	done := w.start(requestCtx, w.id)
	consumed := false
	defer func() {
		cancel()
		if !consumed {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				resultErr = errors.Join(resultErr, errors.New("owned request did not stop within cleanup bound"))
			}
		}
	}()
	barrier, stopBarrier := context.WithTimeout(ctx, w.operation)
	defer stopBarrier()
	for {
		select {
		case <-done:
			consumed = true
			return errors.New("request finished before active cancellation barrier")
		default:
		}
		logs, e := w.logs(barrier)
		if e != nil {
			return errors.New("queue dispatch observation unavailable")
		}
		dispatched := false
		for _, id := range observedQueueIDs(logs, "queue_dispatched", w.id) {
			if id == w.id {
				dispatched = true
			}
		}
		if dispatched {
			h, e := w.observe(barrier)
			if e != nil {
				return errors.New("busy observation unavailable")
			}
			if h != nil && h.Status == "busy" {
				if !activeHealth(h, "busy", w.model) || !sameResident(before, h) || h.ProcessGeneration != before.ProcessGeneration {
					return errors.New("busy resident identity changed")
				}
				if w.verify(barrier) != nil {
					return errors.New("target identity changed before cancellation")
				}
				select {
				case <-done:
					consumed = true
					return errors.New("request finished before cancellation")
				default:
				}
				break
			}
		}
		select {
		case <-barrier.Done():
			return errors.New("active cancellation barrier timed out")
		case <-time.After(100 * time.Millisecond):
		}
	}
	record("identified dispatch and same-generation CUDA-ready busy observed while request pending")
	cleanupDeadline := time.Now().Add(w.cleanup)
	cancel()
	select {
	case result := <-done:
		consumed = true
		if !errors.Is(result.err, context.Canceled) || result.reply.Status != 0 {
			return errors.New("public request did not return canonical transport cancellation")
		}
	case <-time.After(w.cleanup):
		return errors.New("public request cancellation exceeded cleanup bound")
	case <-ctx.Done():
		return errors.New("public cancellation observation timed out")
	}
	record("canonical context cancellation observed within cleanup bound")
	recovery, stopRecovery := context.WithTimeout(ctx, w.provisioning)
	defer stopRecovery()
	var replacement *DockerPrivateHealth
	for {
		h, e := w.observe(recovery)
		if e == nil && activeHealth(h, "ready", w.model) && sameResident(before, h) && h.ProcessGeneration != before.ProcessGeneration {
			replacement = h
			break
		}
		// A restarting backend may temporarily refuse health connections. The
		// selected Pods must still be the same; unknown health never counts as
		// ready and is retried only within the declared provisioning budget.
		if w.verify(recovery) != nil {
			return errors.New("target identity changed during recovery")
		}
		select {
		case <-recovery.Done():
			return errors.New("replacement ready generation not observed")
		case <-time.After(100 * time.Millisecond):
		}
	}
	record("replacement CUDA-ready supervisor generation observed")
	if w.verify(recovery) != nil {
		return errors.New("target identity unavailable after recovery")
	}
	if w.ready(recovery) != nil {
		return errors.New("public readiness unavailable after recovery")
	}
	cleanupCtx, stopCleanup := context.WithDeadline(recovery, cleanupDeadline)
	defer stopCleanup()
	if err := waitP08ScratchRelease(cleanupCtx, w.released); err != nil {
		return errors.New("scratch release unavailable within cleanup bound")
	}
	record("public readiness and API/backend scratch release observed")
	reply, err := w.speech(recovery)
	if err != nil || reply.Status != http.StatusOK || !identifiedPublicSpeech(reply.Text) {
		return errors.New("post-recovery identified public speech failed")
	}
	after, err := w.observe(recovery)
	if err != nil || !activeHealth(after, "ready", w.model) || !sameResident(before, after) || after.ProcessGeneration != replacement.ProcessGeneration {
		return errors.New("post-recovery resident identity unavailable")
	}
	if w.verify(recovery) != nil {
		return errors.New("post-recovery target identity unavailable")
	}
	postCleanup, stopPostCleanup := context.WithTimeout(recovery, w.cleanup)
	defer stopPostCleanup()
	if waitP08ScratchRelease(postCleanup, w.released) != nil {
		return errors.New("post-recovery scratch release unavailable")
	}
	record("identified public speech and stable replacement state observed")
	return nil
}

func TestP08ActiveClientCancelRecovery(t *testing.T) {
	if os.Getenv("P08_ACTIVE_CANCEL") != "1" {
		t.Skip("requires parent-owned Kubernetes cancellation window")
	}
	address := os.Getenv("P08_DEVELOPMENT_API_URL")
	u, err := url.Parse(address)
	if err != nil || u.ForceQuery || !isLoopbackAPIRoot(address) {
		t.Fatal("credential-free loopback API root required")
	}
	model, fixtures := os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES")
	apiUID, backendUID := os.Getenv("P08_ACTIVE_CANCEL_API_UID"), os.Getenv("P08_ACTIVE_CANCEL_BACKEND_UID")
	port, e1 := strconv.Atoi(os.Getenv("P08_ACTIVE_CANCEL_BACKEND_PORT"))
	operation, e2 := strconv.Atoi(os.Getenv("P08_ACTIVE_CANCEL_OPERATION_SECONDS"))
	cleanup, e3 := strconv.Atoi(os.Getenv("P08_ACTIVE_CANCEL_CLEANUP_SECONDS"))
	provisioning, e4 := strconv.Atoi(os.Getenv("P08_ACTIVE_CANCEL_PROVISIONING_SECONDS"))
	if model == "" || !filepath.IsAbs(fixtures) || apiUID == "" || backendUID == "" || e1 != nil || port < 1 || port > 65535 || e2 != nil || operation < 1 || operation > 3600 || e3 != nil || cleanup < 1 || cleanup > 600 || e4 != nil || provisioning < 1 || provisioning > 3600 {
		t.Fatal("explicit bounded target, pod identities, model, fixtures and timeout inputs required")
	}
	target, err := readKubernetesPerformanceTarget(os.Getenv("P08_DEVELOPMENT_TARGET"))
	if err != nil || !filepath.IsAbs(target.Kubeconfig) {
		t.Fatal("complete private target selection with absolute kubeconfig required")
	}
	long, err := readPublicCaseFixture(fixtures, "jfk-900.flac")
	if err != nil {
		t.Fatal("long public fixture unavailable")
	}
	short, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal("short public fixture unavailable")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal("request identity unavailable")
	}
	id := "p08-active-" + hex.EncodeToString(nonce[:])
	timeout := time.Duration(operation+cleanup+5) * time.Second
	client := identifiedPublicClient(t, address, timeout, id)
	recoveryClient := identifiedPublicClient(t, address, timeout, id+"-recovery")
	ctx, cancel := context.WithTimeout(t.Context(), time.Duration(operation+cleanup+provisioning+30)*time.Second)
	defer cancel()
	resourceTarget := p08ResourceTarget{Kubeconfig: target.Kubeconfig, Context: target.Context, Namespace: target.Namespace, NamespaceUID: target.NamespaceUID, Release: target.Release, APIPod: target.APIPod, APIUID: apiUID, APIContainer: "api", BackendPod: target.BackendPod, BackendUID: backendUID, BackendContainer: "backend"}
	identity, err := resourceTarget.identity(ctx)
	if err != nil {
		t.Fatal("initial target identity unavailable")
	}
	verify := func(ctx context.Context) error {
		current, err := resourceTarget.identity(ctx)
		if err != nil || current != identity {
			return errors.New("selected target or container identity changed")
		}
		return nil
	}
	snapshot := func(ctx context.Context) ([2]p08ScratchSnapshot, error) {
		var result [2]p08ScratchSnapshot
		if verify(ctx) != nil {
			return result, errors.New("target identity unavailable")
		}
		for i, item := range []struct{ pod, container string }{{target.APIPod, "api"}, {target.BackendPod, "backend"}} {
			data, err := target.command(ctx, "exec", item.pod, "-c", item.container, "--", "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", "/tmp", "/proc", "1")
			if err != nil {
				return result, errors.New("scratch probe unavailable for " + item.container)
			}
			result[i], err = parseP08Scratch(data)
			if err != nil {
				return result, errors.New("scratch probe response invalid for " + item.container)
			}
		}
		if verify(ctx) != nil {
			return result, errors.New("target identity changed during scratch observation")
		}
		return result, nil
	}
	var baseline [2]p08ScratchSnapshot
	var baselineErr error
	baselineCtx, stopBaseline := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
	err = waitP08ScratchRelease(baselineCtx, func(observeCtx context.Context) error {
		baseline, baselineErr = snapshot(observeCtx)
		if baselineErr == nil && (len(baseline[0].deleted) != 0 || len(baseline[1].deleted) != 0) {
			baselineErr = errors.New("baseline contains deleted-open scratch")
		}
		return baselineErr
	})
	stopBaseline()
	if err != nil {
		// snapshot returns only fixed, non-sensitive diagnostic categories.
		t.Fatal("initial scratch identity unavailable within cleanup budget:", baselineErr)
	}
	work := activeCancelWork{model: model, id: id, operation: time.Duration(operation) * time.Second, cleanup: time.Duration(cleanup) * time.Second, provisioning: time.Duration(provisioning) * time.Second,
		verify: verify,
		fact:   func(fact string) { t.Log(fact) },
		ready: func(ctx context.Context) error {
			return waitDevelopmentReady(ctx, address, time.Duration(provisioning)*time.Second)
		},
		observe: func(ctx context.Context) (*DockerPrivateHealth, error) { return target.health(ctx, strconv.Itoa(port)) },
		logs: func(ctx context.Context) ([]byte, error) {
			return target.command(ctx, "logs", target.APIPod, "--limit-bytes=1048576")
		},
		released: func(ctx context.Context) error {
			current, err := snapshot(ctx)
			if err != nil {
				return err
			}
			for i := range baseline {
				if err := p08ScratchReleased(baseline[i], current[i]); err != nil {
					return err
				}
			}
			return nil
		},
		start: func(ctx context.Context, _ string) <-chan asyncPublicReply {
			return submitPublicRequest(ctx, client, model, long, "jfk-900.flac")
		},
		speech: func(ctx context.Context) (PublicReply, error) {
			return recoveryClient.Transcribe(ctx, short, "jfk.wav", []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
		},
	}
	if err := activeCancelCheck(ctx, work); err != nil {
		t.Fatal(err)
	}
	t.Log("partial active cancellation and recovery passed; GPU kernel activity, native PID retirement and complete P08 NOT_RUN")
}
