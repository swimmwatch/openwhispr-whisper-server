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

// The parent must restore its public connection after replacement. This check
// performs only the explicitly selected preconditioned Pod deletion.
func TestP08GracefulAPIDeletion(t *testing.T) {
	if os.Getenv("P08_GRACEFUL_API_DELETE") != "1" {
		t.Skip("requires parent-owned explicit Kubernetes deletion window")
	}
	address, model, fixtures := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_DEVELOPMENT_FIXTURES")
	target, err := readKubernetesPerformanceTarget(os.Getenv("P08_DEVELOPMENT_TARGET"))
	apiUID, backendUID := os.Getenv("P08_GRACEFUL_API_UID"), os.Getenv("P08_GRACEFUL_BACKEND_UID")
	container, containerID, imageID := os.Getenv("P08_GRACEFUL_CONTAINER"), os.Getenv("P08_GRACEFUL_CONTAINER_ID"), os.Getenv("P08_GRACEFUL_IMAGE_ID")
	port, e1 := strconv.Atoi(os.Getenv("P08_GRACEFUL_BACKEND_PORT"))
	operation, e2 := strconv.Atoi(os.Getenv("P08_GRACEFUL_OPERATION_SECONDS"))
	shutdown, e3 := strconv.Atoi(os.Getenv("P08_GRACEFUL_SHUTDOWN_SECONDS"))
	cleanup, e4 := strconv.Atoi(os.Getenv("P08_GRACEFUL_CLEANUP_SECONDS"))
	provisioning, e5 := strconv.Atoi(os.Getenv("P08_GRACEFUL_PROVISIONING_SECONDS"))
	if err != nil || !filepath.IsAbs(target.Kubeconfig) || !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(fixtures) || apiUID == "" || backendUID == "" || container == "" || containerID == "" || imageID == "" || e1 != nil || port < 1 || port > 65535 || e2 != nil || operation < 1 || operation > 3600 || e3 != nil || shutdown < 1 || shutdown > 3600 || e4 != nil || cleanup < 1 || cleanup > 600 || e5 != nil || provisioning < 1 || provisioning > 3600 {
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
	id := "p08-shutdown-" + hex.EncodeToString(nonce[:])
	ctx, cancel := context.WithTimeout(t.Context(), time.Duration(operation+shutdown+cleanup+provisioning+60)*time.Second)
	defer cancel()
	resourceTarget := p08WorkResourceTarget(target, apiUID, backendUID)
	baselineCtx, stopBaseline := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
	observe := func(ctx context.Context) (*DockerPrivateHealth, error) { return target.health(ctx, strconv.Itoa(port)) }
	baselineIdentity, baselineScratch, baselineHealth, baselineErr := p08WorkBaseline(baselineCtx, resourceTarget, model, observe)
	stopBaseline()
	if baselineErr != nil || baselineIdentity[0].ID != containerID {
		t.Fatal("initial scratch or resident identity unavailable within cleanup budget")
	}
	timeout := time.Duration(operation+shutdown+cleanup+10) * time.Second
	queue := queueWorkload{ctx: ctx, model: model, long: long, short: short, client: func(t *testing.T, id string) *PublicClient { return identifiedPublicClient(t, address, timeout, id) }, logs: func(ctx context.Context) ([]byte, error) {
		return target.command(ctx, "logs", target.APIPod, "-c", container, "--limit-bytes=1048576")
	}}
	deleteTarget := gracefulDeleteTarget{target: target, apiUID: apiUID, backendUID: backendUID, container: container, containerID: containerID, imageID: imageID}
	verify := func(ctx context.Context) error {
		if target.verify(ctx) != nil {
			return errors.New("target unavailable")
		}
		for name, uid := range map[string]string{target.APIPod: apiUID, target.BackendPod: backendUID} {
			data, err := target.command(ctx, "get", "pod", name, "-o", "jsonpath={.metadata.uid}")
			if err != nil || string(data) != uid {
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
	recoverState := func(ctx context.Context, before *DockerPrivateHealth) error {
		lastFailure := recoveryFailure("replacement identity or resident unavailable")
		for {
			// Replacement and the parent's public forwarding can legitimately lag deletion.
			api, ae := target.command(ctx, "get", "pod", target.APIPod, "-o", "jsonpath={.metadata.uid}")
			backend, be := target.command(ctx, "get", "pod", target.BackendPod, "-o", "jsonpath={.metadata.uid}")
			h, he := target.health(ctx, strconv.Itoa(port))
			lastFailure = "replacement identity or resident unavailable"
			if ae == nil && be == nil && string(api) != "" && string(api) != apiUID && string(backend) == backendUID && target.verify(ctx) == nil && he == nil && activeHealth(h, "ready", model) && sameResident(before, h) && h.ProcessGeneration == before.ProcessGeneration {
				lastFailure = "public readiness unavailable"
				if waitDevelopmentReady(ctx, address, 5*time.Second) == nil {
					lastFailure = "pre-speech scratch unavailable or retained"
					replacement := p08WorkResourceTarget(target, string(api), backendUID)
					scratchCtx, stopScratch := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
					identity, scratch, scratchHealth, e := p08WorkBaseline(scratchCtx, replacement, model, observe)
					if e == nil && p08CheckAPIResource(scratchCtx, replacement, identity) != nil {
						e = errors.New("API scratch unavailable")
					}
					stopScratch()
					if e == nil && sameResident(before, scratchHealth) && scratchHealth.ProcessGeneration == before.ProcessGeneration && identity[1] == baselineIdentity[1] && p08APIScratchClean(scratch[0]) && p08ScratchReleased(baselineScratch[1], scratch[1]) == nil {
						client := identifiedPublicClient(t, address, timeout, id+"-recovery")
						reply, e := client.Transcribe(ctx, short, "jfk.wav", []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
						after, he := target.health(ctx, strconv.Itoa(port))
						if e == nil && reply.Status == http.StatusOK && identifiedPublicSpeech(reply.Text) && he == nil && activeHealth(after, "ready", model) && sameResident(before, after) && after.ProcessGeneration == before.ProcessGeneration && target.verify(ctx) == nil {
							checkCtx, stopCheck := context.WithTimeout(ctx, time.Duration(cleanup)*time.Second)
							e := p08WaitPairScratch(checkCtx, replacement, identity, scratch)
							stopCheck()
							if e != nil {
								return recoveryFailure("post-speech scratch retained")
							}
							finalAPI, apiErr := target.command(ctx, "get", "pod", target.APIPod, "-o", "jsonpath={.metadata.uid}")
							finalBackend, backendErr := target.command(ctx, "get", "pod", target.BackendPod, "-o", "jsonpath={.metadata.uid}")
							if apiErr != nil || backendErr != nil || string(finalAPI) != string(api) || string(finalBackend) != backendUID {
								return recoveryFailure("post-speech Pod identities changed")
							}
							return nil
						}
						return recoveryFailure("post-recovery public speech or resident verification failed")
					}
				}
			}
			select {
			case <-ctx.Done():
				return lastFailure
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	w := gracefulWork{queue: queue, activeID: id + "-active", waitingID: id + "-waiting", observe: func(ctx context.Context) (*DockerPrivateHealth, error) { return target.health(ctx, strconv.Itoa(port)) }, verify: verify, delete: deleteTarget.delete, recover: recoverState, start: gracefulStart(t, queue), fact: func(s string) { t.Log(s) }, barrier: time.Duration(operation) * time.Second, outcome: time.Duration(shutdown+cleanup+5) * time.Second, recovery: time.Duration(provisioning) * time.Second}
	if err := gracefulWorkCheck(ctx, w); err != nil {
		t.Fatal(err)
	}
	t.Log("partial graceful deletion observation complete; native PID provenance and full P08 remain unproven")
}
