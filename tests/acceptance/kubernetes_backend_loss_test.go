package acceptance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A success is a delivered identified transcription, not proof of when the
// backend loss happened. Unknown transport failures never become success.
func backendLossResult(r asyncPublicReply, active bool) (string, error) {
	if r.err != nil {
		return gracefulResult(r, active)
	}
	if r.reply.Status == http.StatusOK && r.reply.ErrorCode == "" && identifiedPublicSpeech(r.reply.Text) {
		return "success", nil
	}
	if r.reply.Text != "" {
		return "", errors.New("conflicting backend loss response")
	}
	if r.reply.Status == http.StatusServiceUnavailable && (r.reply.ErrorCode == "backend_unavailable" || r.reply.ErrorCode == "not_ready") {
		return r.reply.ErrorCode, nil
	}
	if r.reply.Status == http.StatusRequestTimeout && r.reply.ErrorCode == "operation_timeout" {
		return "operation_timeout", nil
	}
	return "", errors.New("unexpected backend loss result")
}

// This bounds and brackets the sampled logs. Only the API stream has workload
// anchors; the backend sample cannot establish complete privacy coverage.
func p08BackendLossPrivacy(ctx context.Context, target p08ResourceTarget, expected [2]p08ContainerIdentity, marker, activeID, waitingID string, afterReplacement bool) error {
	before, err := target.identity(ctx)
	if err != nil || before != expected {
		return errors.New("privacy identity unavailable")
	}
	for i, role := range []struct{ pod, container string }{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		data, e := target.command(ctx, "logs", role.pod, "-c", role.container, "--limit-bytes=1048577")
		if e != nil || len(data) == 0 || len(data) >= 1<<20 || data[len(data)-1] != '\n' || checkResidentLogPrivacy(data, marker) != nil {
			return errors.New("sampled request privacy unavailable or violated")
		}
		if i == 0 && p08QueueAnchors(data, activeID, waitingID, afterReplacement) != nil {
			return errors.New("privacy queue window unavailable")
		}
	}
	after, err := target.identity(ctx)
	if err != nil || after != before {
		return errors.New("privacy identity changed")
	}
	return nil
}

// This is a parent-owned, opt-in deletion of one selected backend Pod, not a
// SIGKILL, PID-retirement, GPU lifetime or complete P08 assertion.
func TestP08PartialBackendLossActiveWaiting(t *testing.T) {
	if os.Getenv("P08_BACKEND_LOSS") != "1" {
		t.Skip("requires parent-owned backend replacement window")
	}
	address, model, fixtures := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES")
	target, err := readKubernetesPerformanceTarget(os.Getenv("P08_DEVELOPMENT_TARGET"))
	apiUID, backendUID := os.Getenv("P08_BACKEND_LOSS_API_UID"), os.Getenv("P08_BACKEND_LOSS_BACKEND_UID")
	container, containerID, imageID := os.Getenv("P08_BACKEND_LOSS_CONTAINER"), os.Getenv("P08_BACKEND_LOSS_CONTAINER_ID"), os.Getenv("P08_BACKEND_LOSS_IMAGE_ID")
	apiContainer := os.Getenv("P08_BACKEND_LOSS_API_CONTAINER")
	port, e1 := strconv.Atoi(os.Getenv("P08_BACKEND_LOSS_BACKEND_PORT"))
	operation, e2 := strconv.Atoi(os.Getenv("P08_BACKEND_LOSS_OPERATION_SECONDS"))
	cleanup, e3 := strconv.Atoi(os.Getenv("P08_BACKEND_LOSS_CLEANUP_SECONDS"))
	provisioning, e4 := strconv.Atoi(os.Getenv("P08_BACKEND_LOSS_PROVISIONING_SECONDS"))
	if err != nil || !filepath.IsAbs(target.Kubeconfig) || !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(fixtures) || apiUID == "" || backendUID == "" || container == "" || containerID == "" || imageID == "" || apiContainer == "" || e1 != nil || port < 1 || port > 65535 || e2 != nil || operation < 1 || operation > 3600 || e3 != nil || cleanup < 1 || cleanup > 600 || e4 != nil || provisioning < 1 || provisioning > 3600 {
		t.Fatal("complete explicit private target and bounded workload settings required")
	}
	long, err := readPublicCaseFixture(fixtures, "jfk-900.flac")
	if err != nil {
		t.Fatal("long fixture unavailable")
	}
	short, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal("short fixture unavailable")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal("request identity unavailable")
	}
	suffix := hex.EncodeToString(nonce[:])
	id := "p08-backend-loss-" + suffix
	marker := "P08_PRIVATE_" + suffix
	ctx, cancel := context.WithTimeout(t.Context(), time.Duration(operation+operation+cleanup+provisioning+60)*time.Second)
	defer cancel()
	resourceTarget := p08WorkResourceTarget(target, apiUID, backendUID)
	if apiContainer != resourceTarget.APIContainer || container != resourceTarget.BackendContainer {
		t.Fatal("selected container roles do not match the workload identity")
	}
	baselineCtx, stopBaseline := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
	observe := func(ctx context.Context) (*DockerPrivateHealth, error) { return target.health(ctx, strconv.Itoa(port)) }
	baselineIdentity, baselineScratch, baselineHealth, baselineErr := p08WorkBaseline(baselineCtx, resourceTarget, model, observe)
	if baselineErr == nil && p08CheckAPIResource(baselineCtx, resourceTarget, baselineIdentity) != nil {
		baselineErr = errors.New("API scratch unavailable")
	}
	stopBaseline()
	if baselineErr != nil || baselineIdentity[1].ID != containerID || !p08APIScratchClean(baselineScratch[0]) {
		t.Fatal("initial scratch or resident identity unavailable within cleanup budget")
	}
	timeout := time.Duration(operation+cleanup+10) * time.Second
	queue := queueWorkload{ctx: ctx, model: model, long: long, short: short, client: func(t *testing.T, id string) *PublicClient { return identifiedPublicClient(t, address, timeout, id) }, logs: func(ctx context.Context) ([]byte, error) {
		return resourceTarget.command(ctx, "logs", resourceTarget.APIPod, "-c", resourceTarget.APIContainer, "--limit-bytes=1048577")
	}}
	verify := func(ctx context.Context) error {
		if target.verify(ctx) != nil {
			return errors.New("target unavailable")
		}
		for name, uid := range map[string]string{target.APIPod: apiUID, target.BackendPod: backendUID} {
			data, e := target.command(ctx, "get", "pod", name, "-o", "jsonpath={.metadata.uid}")
			if e != nil || string(data) != uid {
				return errors.New("Pod identity changed")
			}
		}
		identity, e := resourceTarget.identity(ctx)
		h, he := target.health(ctx, strconv.Itoa(port))
		if e != nil || identity != baselineIdentity || he != nil || !sameResident(baselineHealth, h) || h.ProcessGeneration != baselineHealth.ProcessGeneration {
			return errors.New("initial resident or container identity changed")
		}
		return nil
	}
	deletion := gracefulDeleteTarget{target: target, apiUID: apiUID, backendUID: backendUID, role: "backend", container: container, containerID: containerID, imageID: imageID}
	waitingSucceeded := false
	start := func(ctx context.Context, requestID string, active bool) <-chan asyncPublicReply {
		audio := short
		if active {
			audio = long
		}
		client := queue.client(t, requestID)
		done := make(chan asyncPublicReply, 1)
		go func() {
			reply, e := client.Transcribe(ctx, audio, marker+".wav", []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}, {"prompt", marker}})
			done <- asyncPublicReply{reply: reply, err: e}
		}()
		return done
	}
	recoverPair := func(ctx context.Context, before *DockerPrivateHealth) error {
		for {
			a, ae := target.command(ctx, "get", "pod", target.APIPod, "-o", "jsonpath={.metadata.uid}")
			b, be := target.command(ctx, "get", "pod", target.BackendPod, "-o", "jsonpath={.metadata.uid}")
			h, he := target.health(ctx, strconv.Itoa(port))
			if ae == nil && be == nil && string(a) == apiUID && string(b) != "" && string(b) != backendUID && target.verify(ctx) == nil && he == nil && activeHealth(h, "ready", model) && sameResident(before, h) && h.ProcessGeneration != before.ProcessGeneration && waitDevelopmentReady(ctx, address, 5*time.Second) == nil {
				logs, e := queue.logs(ctx)
				if e != nil {
					return recoveryFailure("queue evidence unavailable")
				}
				dispatched := observedQueueIDs(logs, "queue_dispatched", id+"-waiting")
				if len(dispatched) > 1 || (waitingSucceeded && (len(dispatched) != 1 || dispatched[0] != id+"-waiting")) {
					return recoveryFailure("waiting success lacks unique queue dispatch")
				}
				replacement := p08WorkResourceTarget(target, apiUID, string(b))
				scratchCtx, stopScratch := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
				identity, scratch, scratchHealth, e := p08WorkBaseline(scratchCtx, replacement, model, observe)
				stopScratch()
				if e != nil || !sameResident(h, scratchHealth) || scratchHealth.ProcessGeneration != h.ProcessGeneration || identity[0] != baselineIdentity[0] || p08ScratchReleased(baselineScratch[0], scratch[0]) != nil {
					return recoveryFailure("scratch not released")
				}
				if p08BackendLossPrivacy(ctx, replacement, identity, marker, id+"-active", id+"-waiting", true) != nil {
					return recoveryFailure("post-replacement log privacy failed")
				}
				client := queue.client(t, id+"-recovery")
				reply, e := client.Transcribe(ctx, short, "jfk.wav", []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
				if e != nil || reply.Status != http.StatusOK || !identifiedPublicSpeech(reply.Text) {
					return recoveryFailure("post-replacement speech failed")
				}
				checkCtx, stopCheck := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
				e = p08WaitPairScratch(checkCtx, replacement, identity, scratch)
				stopCheck()
				if e != nil {
					return recoveryFailure("scratch not released")
				}
				after, e := target.health(ctx, strconv.Itoa(port))
				aa, ea := target.command(ctx, "get", "pod", target.APIPod, "-o", "jsonpath={.metadata.uid}")
				bb, eb := target.command(ctx, "get", "pod", target.BackendPod, "-o", "jsonpath={.metadata.uid}")
				if e != nil || !activeHealth(after, "ready", model) || !sameResident(h, after) || after.ProcessGeneration != h.ProcessGeneration || ea != nil || eb != nil || string(aa) != apiUID || string(bb) != string(b) || target.verify(ctx) != nil {
					return recoveryFailure("recovered pair identity changed")
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return recoveryFailure("recovery deadline exceeded")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	w := gracefulWork{queue: queue, activeID: id + "-active", waitingID: id + "-waiting", observe: func(ctx context.Context) (*DockerPrivateHealth, error) { return target.health(ctx, strconv.Itoa(port)) }, verify: verify, delete: func(ctx context.Context) error {
		if p08BackendLossPrivacy(ctx, resourceTarget, baselineIdentity, marker, id+"-active", id+"-waiting", false) != nil {
			return errors.New("pre-deletion log privacy failed")
		}
		return deletion.delete(ctx)
	}, recover: recoverPair, start: start, classify: func(r asyncPublicReply, active bool) (string, error) {
		s, e := backendLossResult(r, active)
		if !active && s == "success" {
			waitingSucceeded = true
		}
		return s, e
	}, fact: func(s string) { t.Log(s) }, actionFact: "selected backend Pod deletion accepted after active dispatch and waiting admission; process termination provenance not established", barrier: time.Duration(operation) * time.Second, outcome: time.Duration(operation+cleanup+5) * time.Second, recovery: time.Duration(provisioning) * time.Second}
	if err := gracefulWorkCheck(ctx, w); err != nil {
		t.Fatal(err)
	}
	t.Log("partial backend replacement observed; old backend logs sampled before deletion only; no SIGKILL, native PID or GPU lifetime proof")
}
