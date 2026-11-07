package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A lost connection is evidence only of a lost response, never successful work.
func gracefulResult(r asyncPublicReply, active bool) (string, error) {
	if r.err != nil {
		var transport *publicRequestError
		if r.reply.Status == 0 && r.reply.Text == "" && r.reply.ErrorCode == "" && !errors.Is(r.err, context.Canceled) && !errors.Is(r.err, context.DeadlineExceeded) && errors.As(r.err, &transport) && (errors.Is(r.err, io.EOF) || errors.Is(r.err, syscall.ECONNRESET) || errors.Is(r.err, syscall.EPIPE)) {
			return "lost_response", nil
		}
		return "", errors.New("unknown or conflicting public result")
	}
	if active && r.reply.Status == http.StatusOK && identifiedPublicSpeech(r.reply.Text) && r.reply.ErrorCode == "" {
		return "success", nil
	}
	if !active && r.reply.Status == http.StatusServiceUnavailable && r.reply.ErrorCode == "shutting_down" && r.reply.Text == "" {
		return "shutting_down", nil
	}
	return "", errors.New("unexpected shutdown response")
}

// recoveryFailure contains only fixed, non-sensitive diagnostic categories.
type recoveryFailure string

func (e recoveryFailure) Error() string { return string(e) }

func recoveryDiagnostic(err error) error {
	var category recoveryFailure
	if errors.As(err, &category) {
		switch category {
		case "recovery deadline exceeded", "replacement identity or resident unavailable", "public readiness unavailable", "pre-speech scratch unavailable or retained", "post-speech scratch unavailable", "post-speech scratch retained", "post-speech Pod identities changed", "post-recovery public speech or resident verification failed", "post-replacement log privacy failed", "queue evidence unavailable", "waiting success lacks unique queue dispatch", "post-replacement speech failed", "scratch observation unavailable", "scratch not released", "recovered pair identity changed":
			return errors.New("post-deletion recovery evidence unavailable: " + string(category))
		}
	}
	return errors.New("post-deletion recovery evidence unavailable")
}

func p08WorkResourceTarget(target developmentQueueTarget, apiUID, backendUID string) p08ResourceTarget {
	return p08ResourceTarget{Kubeconfig: target.Kubeconfig, Context: target.Context, Namespace: target.Namespace, NamespaceUID: target.NamespaceUID, Release: target.Release, APIPod: target.APIPod, APIUID: apiUID, APIContainer: "api", BackendPod: target.BackendPod, BackendUID: backendUID, BackendContainer: "backend"}
}

func p08BracketBaseline(ctx context.Context, model string, observe func(context.Context) (*DockerPrivateHealth, error), sample func(context.Context) ([2]p08ContainerIdentity, [2]p08ScratchSnapshot, error)) ([2]p08ContainerIdentity, [2]p08ScratchSnapshot, *DockerPrivateHealth, error) {
	var identity [2]p08ContainerIdentity
	var snapshots [2]p08ScratchSnapshot
	var health *DockerPrivateHealth
	err := waitP08ScratchRelease(ctx, func(ctx context.Context) error {
		before, err := observe(ctx)
		if err != nil || !activeHealth(before, "ready", model) {
			return errors.New("resident unavailable before scratch")
		}
		identity, snapshots, err = sample(ctx)
		if err != nil {
			return err
		}
		after, err := observe(ctx)
		if err != nil || !activeHealth(after, "ready", model) || !sameResident(before, after) || before.ProcessGeneration != after.ProcessGeneration {
			return errors.New("resident changed during scratch")
		}
		health = after
		return nil
	})
	return identity, snapshots, health, err
}

func p08WorkBaseline(ctx context.Context, target p08ResourceTarget, model string, observe func(context.Context) (*DockerPrivateHealth, error)) ([2]p08ContainerIdentity, [2]p08ScratchSnapshot, *DockerPrivateHealth, error) {
	return p08BracketBaseline(ctx, model, observe, func(ctx context.Context) ([2]p08ContainerIdentity, [2]p08ScratchSnapshot, error) {
		identity, err := target.identity(ctx)
		if err != nil {
			return identity, [2]p08ScratchSnapshot{}, err
		}
		snapshots, err := p08PairScratch(ctx, target, identity)
		return identity, snapshots, err
	})
}

// p08PairScratch brackets both observations with Pod and container identity.
func p08PairScratch(ctx context.Context, target p08ResourceTarget, expected [2]p08ContainerIdentity) ([2]p08ScratchSnapshot, error) {
	var result [2]p08ScratchSnapshot
	identity, err := target.identity(ctx)
	if err != nil || identity != expected {
		return result, errors.New("scratch identity unavailable")
	}
	for i, item := range []struct{ pod, container string }{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		data, err := target.command(ctx, "exec", item.pod, "-c", item.container, "--", "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", "/tmp", "/proc", "1")
		if err != nil {
			return result, errors.New("scratch observation unavailable")
		}
		result[i], err = parseP08Scratch(data)
		if err != nil || len(result[i].deleted) != 0 {
			return result, errors.New("scratch observation unavailable or deleted-open")
		}
	}
	identity, err = target.identity(ctx)
	if err != nil || identity != expected {
		return result, errors.New("scratch identity changed")
	}
	return result, nil
}

func p08APIScratchClean(snapshot p08ScratchSnapshot) bool {
	if !snapshot.valid || len(snapshot.deleted) != 0 || len(snapshot.files) > 1 {
		return false
	}
	if len(snapshot.files) == 0 {
		return true
	}
	path := sha256.Sum256([]byte("/tmp/whisper-api/.owner.lock"))
	empty := sha256.Sum256(nil)
	fields := strings.Fields(snapshot.files[0])
	return len(fields) == 4 && fields[0] == "F" && fields[1] == fmt.Sprintf("%x", path) && fields[3] == fmt.Sprintf("%x", empty)
}

func p08CheckAPIResource(ctx context.Context, target p08ResourceTarget, identity [2]p08ContainerIdentity) error {
	before, err := target.identity(ctx)
	if err != nil || before != identity {
		return errors.New("API identity unavailable")
	}
	data, err := target.command(ctx, "exec", target.APIPod, "-c", target.APIContainer, "--", "sh", "-eu", "-c", dockerResourceProbe, "development-observe", "api")
	if err != nil {
		return errors.New("API resource unavailable")
	}
	resource, _, err := parseResourceSnapshot(data, "api")
	if err != nil || releasedScratchResources(resource) != nil {
		return errors.New("API scratch not clean")
	}
	after, err := target.identity(ctx)
	if err != nil || after != identity {
		return errors.New("API identity changed")
	}
	return nil
}

func p08WaitPairScratch(ctx context.Context, target p08ResourceTarget, identity [2]p08ContainerIdentity, before [2]p08ScratchSnapshot) error {
	return waitP08ScratchRelease(ctx, func(ctx context.Context) error {
		after, err := p08PairScratch(ctx, target, identity)
		if err != nil {
			return err
		}
		return p08ScratchPairReleased(before, after)
	})
}

func p08ScratchPairReleased(before, after [2]p08ScratchSnapshot) error {
	for i := range before {
		if len(before[i].deleted) != 0 || len(after[i].deleted) != 0 || p08ScratchReleased(before[i], after[i]) != nil {
			return errors.New("scratch identity changed or deleted-open scratch retained")
		}
	}
	return nil
}

// Queue anchors establish only the observed interval, conditional on an ordered,
// unbroken log stream; they do not prove the runtime retained every log record.
func p08QueueWindow(logs []byte, activeID, waitingID string) error {
	return p08QueueAnchors(logs, activeID, waitingID, false)
}

func p08QueueAnchors(logs []byte, activeID, waitingID string, allowWaitingDispatch bool) error {
	if len(logs) == 0 || len(logs) >= 1<<20 || logs[len(logs)-1] != '\n' || activeID == "" || waitingID == "" || activeID == waitingID {
		return errors.New("bounded queue window unavailable")
	}
	active, waiting := false, false
	for _, line := range strings.Split(string(logs), "\n") {
		index := strings.IndexByte(line, '{')
		if index < 0 {
			continue
		}
		var item queueLogEvent
		if json.Unmarshal([]byte(line[index:]), &item) != nil {
			continue
		}
		switch {
		case item.Event == "queue_dispatched" && item.RequestID == activeID:
			if active || waiting {
				return errors.New("ambiguous queue window")
			}
			active = true
		case item.Event == "queue_admitted" && item.RequestID == waitingID:
			if !active || waiting {
				return errors.New("ambiguous queue window")
			}
			waiting = true
		case item.Event == "queue_dispatched" && item.RequestID == waitingID:
			if !waiting || !allowWaitingDispatch {
				return errors.New("waiting request already dispatched")
			}
		}
	}
	if !active || !waiting {
		return errors.New("queue window anchors unavailable")
	}
	return nil
}

type gracefulWork struct {
	queue                      queueWorkload
	activeID, waitingID        string
	observe                    func(context.Context) (*DockerPrivateHealth, error)
	verify                     func(context.Context) error
	delete                     func(context.Context) error
	recover                    func(context.Context, *DockerPrivateHealth) error
	start                      func(context.Context, string, bool) <-chan asyncPublicReply
	fact                       func(string)
	classify                   func(asyncPublicReply, bool) (string, error)
	actionFact                 string
	barrier, outcome, recovery time.Duration
}

// Sampled busy and dispatch do not prove native PID provenance or uninterrupted
// residency. The parent owns target selection and the surrounding test window.
func gracefulWorkCheck(ctx context.Context, w gracefulWork) (err error) {
	if w.queue.model == "" || w.queue.logs == nil || w.activeID == "" || w.waitingID == "" || w.activeID == w.waitingID || w.observe == nil || w.verify == nil || w.delete == nil || w.recover == nil || w.start == nil || w.barrier <= 0 || w.outcome <= 0 || w.recovery <= 0 {
		return errors.New("incomplete graceful workload")
	}
	if w.verify(ctx) != nil {
		return errors.New("initial target verification failed")
	}
	before, e := w.observe(ctx)
	if e != nil || !activeHealth(before, "ready", w.queue.model) {
		return errors.New("initial resident unavailable")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	active := w.start(requestCtx, w.activeID, true)
	var waiting <-chan asyncPublicReply
	activeDone, waitingDone := false, false
	defer func() {
		cancel()
		for _, item := range []struct {
			ch   <-chan asyncPublicReply
			done bool
		}{{active, activeDone}, {waiting, waitingDone}} {
			if item.ch != nil && !item.done {
				select {
				case <-item.ch:
				case <-time.After(5 * time.Second):
					err = errors.Join(err, errors.New("request cleanup timed out"))
				}
			}
		}
	}()
	barrier, stop := context.WithTimeout(ctx, w.barrier)
	defer stop()
	wait := func(event, id string, checkBusy bool) error {
		for {
			select {
			case <-active:
				activeDone = true
				return errors.New("active finished before deletion")
			default:
			}
			if waiting != nil {
				select {
				case <-waiting:
					waitingDone = true
					return errors.New("waiting finished before deletion")
				default:
				}
			}
			data, e := w.queue.logs(barrier)
			if e != nil {
				return errors.New("queue observation unavailable")
			}
			found := false
			for _, value := range observedQueueIDs(data, event, id) {
				if value == id {
					found = true
				}
			}
			if found {
				if !checkBusy {
					return nil
				}
				h, e := w.observe(barrier)
				if e != nil {
					return errors.New("busy observation unavailable")
				}
				if h != nil && h.Status == "busy" {
					if !activeHealth(h, "busy", w.queue.model) || !sameResident(before, h) || h.ProcessGeneration != before.ProcessGeneration {
						return errors.New("busy resident identity changed")
					}
					return nil
				}
			}
			select {
			case <-barrier.Done():
				return errors.New("queue barrier timed out")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err := wait("queue_dispatched", w.activeID, true); err != nil {
		return err
	}
	waiting = w.start(requestCtx, w.waitingID, false)
	if err := wait("queue_admitted", w.waitingID, false); err != nil {
		return err
	}
	data, e := w.queue.logs(barrier)
	if e != nil {
		return errors.New("queue evidence unavailable")
	}
	if p08QueueWindow(data, w.activeID, w.waitingID) != nil {
		return errors.New("waiting admission conflicts with dispatch observation")
	}
	h, e := w.observe(barrier)
	if e != nil || !activeHealth(h, "busy", w.queue.model) || !sameResident(before, h) || h.ProcessGeneration != before.ProcessGeneration || w.verify(barrier) != nil {
		return errors.New("target or busy resident changed before deletion")
	}
	select {
	case <-active:
		activeDone = true
		return errors.New("active finished before deletion")
	case <-waiting:
		waitingDone = true
		return errors.New("waiting finished before deletion")
	default:
	}
	if w.delete(barrier) != nil {
		return errors.New("selected graceful deletion failed")
	}
	if w.fact != nil {
		fact := w.actionFact
		if fact == "" {
			fact = "observed graceful deletion after active dispatch and waiting admission; termination provenance is not established"
		}
		w.fact(fact)
	}
	deadline, stopOutcome := context.WithTimeout(ctx, w.outcome)
	defer stopOutcome()
	for _, item := range []struct {
		ch     <-chan asyncPublicReply
		active bool
		done   *bool
	}{{active, true, &activeDone}, {waiting, false, &waitingDone}} {
		select {
		case result := <-item.ch:
			*item.done = true
			classify := w.classify
			if classify == nil {
				classify = gracefulResult
			}
			outcome, e := classify(result, item.active)
			if e != nil {
				return e
			}
			if w.fact != nil {
				if item.active {
					w.fact("active: " + outcome)
				} else {
					w.fact("waiting: " + outcome)
				}
			}
		case <-deadline.Done():
			return errors.New("shutdown result exceeded bound")
		}
	}
	recovery, stopRecovery := context.WithTimeout(ctx, w.recovery)
	defer stopRecovery()
	if e := w.recover(recovery, before); e != nil {
		return recoveryDiagnostic(e)
	}
	return nil
}

// Keep the production workload's identity-bearing client and fixture assembly.
func gracefulStart(t *testing.T, w queueWorkload) func(context.Context, string, bool) <-chan asyncPublicReply {
	return func(ctx context.Context, id string, active bool) <-chan asyncPublicReply {
		audio, name := w.short, "jfk.wav"
		if active {
			audio, name = w.long, "jfk-900.flac"
		}
		return submitPublicRequest(ctx, w.client(t, id), w.model, audio, name)
	}
}
