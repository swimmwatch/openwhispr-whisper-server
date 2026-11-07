package acceptance

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

type requestIdentityTransport struct {
	base http.RoundTripper
	id   string
}

func (r requestIdentityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("X-Request-ID", r.id)
	return r.base.RoundTrip(clone)
}
func (r requestIdentityTransport) CloseIdleConnections() {
	if owner, ok := r.base.(interface{ CloseIdleConnections() }); ok {
		owner.CloseIdleConnections()
	}
}

func identifiedLocalClient(t *testing.T, e *acceptanceEnvironment, id string) *PublicClient {
	t.Helper()
	client, err := NewPublicClient(e.address, time.Duration(e.settings.Timeouts.OperationSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = requestIdentityTransport{client.client.Transport, id}
	t.Cleanup(client.Close)
	return client
}

type queueLogEvent struct {
	Event     string `json:"event"`
	RequestID string `json:"request_id"`
}

func observedQueueIDs(logs []byte, event, prefix string) []string {
	var ids []string
	for _, line := range strings.Split(string(logs), "\n") {
		index := strings.IndexByte(line, '{')
		if index < 0 {
			continue
		}
		var item queueLogEvent
		if json.Unmarshal([]byte(line[index:]), &item) == nil && item.Event == event && strings.HasPrefix(item.RequestID, prefix) {
			ids = append(ids, item.RequestID)
		}
	}
	return ids
}

func waitLocalQueueEvent(t *testing.T, e *acceptanceEnvironment, event, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		logs, err := e.target.Logs(e.ctx, "api")
		if err != nil {
			t.Fatal(err)
		}
		for _, observed := range observedQueueIDs(logs, event, id) {
			if observed == id {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("controlled queue operation did not reach its required phase")
		}
		select {
		case <-e.ctx.Done():
			t.Fatal("queue phase cancelled")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

type asyncPublicReply struct {
	reply PublicReply
	err   error
}

func submitIdentified(t *testing.T, e *acceptanceEnvironment, ctx context.Context, id string, audio []byte, filename string) <-chan asyncPublicReply {
	t.Helper()
	client := identifiedLocalClient(t, e, id)
	done := make(chan asyncPublicReply, 1)
	go func() {
		reply, err := client.Transcribe(ctx, audio, filename, []FormField{{"model", e.settings.Models.Served}, {"language", "en"}, {"temperature", "0"}})
		done <- asyncPublicReply{reply, err}
	}()
	return done
}

func TestLocalInputBoundsAndIsolation(t *testing.T) {
	runInputBoundsAndIsolation(t, openLocalEnvironment(t, nil))
}

func runInputBoundsAndIsolation(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	t.Run("exact_file_cap_and_one_byte_excess", func(t *testing.T) {
		cap := e.settings.Limits.AudioBytes
		if int64(len(e.audio))+8 > cap {
			t.Fatal("public WAV fixture cannot be padded to the selected cap")
		}
		padded := make([]byte, int(cap))
		copy(padded, e.audio)
		binary.LittleEndian.PutUint32(padded[4:8], uint32(cap-8))
		copy(padded[len(e.audio):], "JUNK")
		binary.LittleEndian.PutUint32(padded[len(e.audio)+4:len(e.audio)+8], uint32(cap-int64(len(e.audio))-8))
		reply, err := e.client.Transcribe(e.ctx, padded, "exact.wav", []FormField{{"model", "base"}, {"language", "en"}})
		if err != nil || reply.Status != http.StatusOK {
			t.Fatal("valid WAV at the exact selected file cap was rejected")
		}
		excess := append(padded, 0)
		reply, err = e.client.Transcribe(e.ctx, excess, "excess.wav", []FormField{{"model", "base"}})
		if err != nil || reply.Status != http.StatusRequestEntityTooLarge || reply.ErrorCode != "payload_too_large" {
			t.Fatal("one-byte excess did not fail the actual upload boundary")
		}
	})
	t.Run("decoded_duration_and_prompt_limits", func(t *testing.T) {
		overlong := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-901.flac"), e.settings.Limits.AudioBytes)
		reply, err := e.client.Transcribe(e.ctx, overlong, "overlong.flac", []FormField{{"model", "base"}})
		if err != nil || reply.Status != http.StatusBadRequest || reply.ErrorCode != "audio_duration_limit" {
			t.Fatal("verified 901-second lossless input did not reject without truncation")
		}
		reply, err = e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}, {"prompt", strings.Repeat("p", e.settings.Limits.PromptBytes+1)}})
		if err != nil || reply.Status != http.StatusBadRequest || reply.ErrorCode != "invalid_request" {
			t.Fatal("oversized prompt did not fail its actual byte limit")
		}
	})
	t.Run("distinct_recordings_and_prompt_reset", func(t *testing.T) {
		for _, name := range []string{"jfk-first.wav", "jfk-last.wav", "jfk-first.wav"} {
			audio := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, name), e.settings.Limits.AudioBytes)
			reply, err := e.client.Transcribe(e.ctx, audio, name, []FormField{{"model", "base"}, {"language", "en"}, {"temperature", "0"}})
			if err != nil || reply.Status != http.StatusOK || reply.Text == "" {
				t.Fatal("distinct recording did not transcribe")
			}
			text := strings.ToLower(reply.Text)
			if name == "jfk-first.wav" && strings.Contains(text, "country") || name == "jfk-last.wav" && !strings.Contains(text, "country") {
				t.Fatal("recording result did not match its isolated speech control")
			}
		}
		marker := "P07_SYNTHETIC_PRIVACY_MARKER"
		for _, prompt := range []string{marker, ""} {
			reply, err := e.client.Transcribe(e.ctx, e.audio, marker+".wav", []FormField{{"model", "base"}, {"prompt", prompt}, {"language", "en"}, {"temperature", "0"}})
			if err != nil || reply.Status != http.StatusOK {
				t.Fatal("prompt/reset control failed")
			}
		}
		for _, role := range []string{"api", "backend"} {
			logs, err := e.target.Logs(e.ctx, role)
			if err != nil || strings.Contains(string(logs), marker) {
				t.Fatal("privacy marker appeared in emitted logs or log observation failed")
			}
			id, err := e.target.container(e.ctx, role)
			if err != nil {
				t.Fatal(err)
			}
			const fileProbe = `pattern=$1; shift; if grep -rFq -- "$pattern" "$@"; then printf leak; else code=$?; [ "$code" -eq 1 ] || exit "$code"; printf clean; fi`
			args := []string{"exec", id, "sh", "-ec", fileProbe, "p07-privacy", marker, "/tmp"}
			if role == "backend" {
				args = append(args, e.settings.Storage.CacheDir)
			}
			result, err := e.target.run(e.ctx, "owned runtime file marker observation", false, args...)
			if err != nil || string(result) != "clean" {
				t.Fatal("privacy marker appeared in owned runtime files or file observation is unavailable")
			}
		}
	})
}

func TestLocalFIFOAndQueuedCancellation(t *testing.T) {
	runFIFOAndQueuedCancellation(t, openLocalEnvironment(t, func(settings *config.Settings) { settings.Queue.WaitingCapacity = 2 }))
}

func runFIFOAndQueuedCancellation(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	longest := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	active := submitIdentified(t, e, ctx, "fifo-active", longest, "longest.flac")
	waitLocalQueueEvent(t, e, "queue_dispatched", "fifo-active")
	second := submitIdentified(t, e, ctx, "fifo-second", e.audio, "jfk.wav")
	waitLocalQueueEvent(t, e, "queue_admitted", "fifo-second")
	third := submitIdentified(t, e, ctx, "fifo-third", e.audio, "jfk.wav")
	waitLocalQueueEvent(t, e, "queue_admitted", "fifo-third")
	overflow := identifiedLocalClient(t, e, "fifo-overflow")
	reply, err := overflow.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", "base"}})
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
	logs, err := e.target.Logs(e.ctx, "api")
	if err != nil {
		t.Fatal(err)
	}
	ids := observedQueueIDs(logs, "queue_dispatched", "fifo-")
	if len(ids) != 3 || ids[0] != "fifo-active" || ids[1] != "fifo-second" || ids[2] != "fifo-third" {
		t.Fatal("actual private-dispatch order differs from admitted FIFO order or overflow dispatched")
	}
	// A queued disconnect must remove its owner, not enter native inference later.
	active = submitIdentified(t, e, ctx, "cancel-queue-active", longest, "longest.flac")
	waitLocalQueueEvent(t, e, "queue_dispatched", "cancel-queue-active")
	waitingCtx, stopWaiting := context.WithCancel(ctx)
	waiting := submitIdentified(t, e, waitingCtx, "cancel-queue-waiting", e.audio, "jfk.wav")
	waitLocalQueueEvent(t, e, "queue_admitted", "cancel-queue-waiting")
	stopWaiting()
	result := <-waiting
	if result.err == nil || !errors.Is(result.err, context.Canceled) {
		t.Fatal("queued disconnect did not preserve cancellation")
	}
	if result = <-active; result.err != nil || result.reply.Status != http.StatusOK {
		t.Fatal("queued disconnect disturbed active inference")
	}
	logs, err = e.target.Logs(e.ctx, "api")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range observedQueueIDs(logs, "queue_dispatched", "cancel-queue-") {
		if id == "cancel-queue-waiting" {
			t.Fatal("cancelled queued work entered native dispatch")
		}
	}
	state, err := e.target.Observe(e.ctx)
	if err != nil || releasedAPIResources(state) != nil {
		t.Fatal("FIFO/cancellation retained API request resources")
	}
}
