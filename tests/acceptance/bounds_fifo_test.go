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
	return identifiedPublicClient(t, e.address, time.Duration(e.settings.Timeouts.OperationSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second, id)
}

func identifiedPublicClient(t *testing.T, address string, timeout time.Duration, id string) *PublicClient {
	t.Helper()
	client, err := NewPublicClient(address, timeout, 1<<20)
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
	waitQueueEvent(t, e.ctx, func(ctx context.Context) ([]byte, error) { return e.target.Logs(ctx, "api") }, event, id)
}

func waitQueueEvent(t *testing.T, ctx context.Context, observe func(context.Context) ([]byte, error), event, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		logs, err := observe(ctx)
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
		case <-ctx.Done():
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
	return submitPublicRequest(ctx, identifiedLocalClient(t, e, id), e.settings.Models.Served, audio, filename)
}

func submitPublicRequest(ctx context.Context, client *PublicClient, model string, audio []byte, filename string) <-chan asyncPublicReply {
	done := make(chan asyncPublicReply, 1)
	go func() {
		reply, err := client.Transcribe(ctx, audio, filename, []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
		done <- asyncPublicReply{reply, err}
	}()
	return done
}

func TestLocalInputBoundsAndIsolation(t *testing.T) {
	runInputBoundsAndIsolation(t, openLocalEnvironment(t, nil))
}

type inputBoundsWork struct {
	ctx         context.Context
	client      *PublicClient
	model       string
	audio       []byte
	fixtures    string
	audioBytes  int64
	promptBytes int
	privacy     func(context.Context, string) error
	bracket     func(func() bool) error
}

func runInputBoundsAndIsolation(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	work := inputBoundsWork{ctx: e.ctx, client: e.client, model: e.settings.Models.Served, audio: e.audio, fixtures: e.selection.FixtureDirectory, audioBytes: e.settings.Limits.AudioBytes, promptBytes: e.settings.Limits.PromptBytes}
	work.privacy = func(ctx context.Context, marker string) error {
		for _, role := range []string{"api", "backend"} {
			logs, err := e.target.Logs(ctx, role)
			if err != nil || checkResidentLogPrivacy(logs, marker) != nil {
				return errors.New("privacy log observation unavailable or violated")
			}
			id, err := e.target.container(ctx, role)
			if err != nil {
				return errors.New("privacy container unavailable")
			}
			const fileProbe = `pattern=$1; shift; if grep -rFq -- "$pattern" "$@"; then printf leak; else code=$?; [ "$code" -eq 1 ] || exit "$code"; printf clean; fi`
			args := []string{"exec", id, "sh", "-ec", fileProbe, "p07-privacy", marker, "/tmp"}
			if role == "backend" {
				args = append(args, e.settings.Storage.CacheDir)
			}
			result, err := e.target.run(ctx, "owned runtime file marker observation", false, args...)
			if err != nil || string(result) != "clean" {
				return errors.New("privacy file observation unavailable or violated")
			}
		}
		return nil
	}
	runInputBoundsWork(t, work)
}

func runInputBoundsWork(t *testing.T, work inputBoundsWork) {
	t.Helper()
	runCase := func(name string, run func(*testing.T)) {
		if work.bracket == nil {
			t.Run(name, run)
			return
		}
		if err := work.bracket(func() bool { return t.Run(name, run) }); err != nil {
			t.Fatal("input workload candidate bracket failed")
		}
	}
	runCase("exact_file_cap_and_one_byte_excess", func(t *testing.T) {
		cap := work.audioBytes
		if len(work.audio) < 8 || int64(len(work.audio))+8 > cap {
			t.Fatal("public WAV fixture cannot be padded to the selected cap")
		}
		padded := make([]byte, int(cap))
		copy(padded, work.audio)
		binary.LittleEndian.PutUint32(padded[4:8], uint32(cap-8))
		copy(padded[len(work.audio):], "JUNK")
		binary.LittleEndian.PutUint32(padded[len(work.audio)+4:len(work.audio)+8], uint32(cap-int64(len(work.audio))-8))
		reply, err := work.client.Transcribe(work.ctx, padded, "exact.wav", []FormField{{"model", work.model}, {"language", "en"}})
		if err != nil || reply.Status != http.StatusOK {
			t.Fatal("valid WAV at the exact selected file cap was rejected")
		}
		excess := append(padded, 0)
		reply, err = work.client.Transcribe(work.ctx, excess, "excess.wav", []FormField{{"model", work.model}})
		if err != nil || reply.Status != http.StatusRequestEntityTooLarge || reply.ErrorCode != "payload_too_large" {
			t.Fatal("one-byte excess did not fail the actual upload boundary")
		}
	})
	runCase("decoded_duration_and_prompt_limits", func(t *testing.T) {
		overlong := readLocalAudio(t, filepath.Join(work.fixtures, "jfk-901.flac"), work.audioBytes)
		reply, err := work.client.Transcribe(work.ctx, overlong, "overlong.flac", []FormField{{"model", work.model}})
		if err != nil || reply.Status != http.StatusBadRequest || reply.ErrorCode != "audio_duration_limit" {
			t.Fatal("verified 901-second lossless input did not reject without truncation")
		}
		reply, err = work.client.Transcribe(work.ctx, work.audio, "jfk.wav", []FormField{{"model", work.model}, {"prompt", strings.Repeat("p", work.promptBytes+1)}})
		if err != nil || reply.Status != http.StatusBadRequest || reply.ErrorCode != "invalid_request" {
			t.Fatal("oversized prompt did not fail its actual byte limit")
		}
	})
	runCase("distinct_recordings_and_prompt_reset", func(t *testing.T) {
		for _, name := range []string{"jfk-first.wav", "jfk-last.wav", "jfk-first.wav"} {
			audio := readLocalAudio(t, filepath.Join(work.fixtures, name), work.audioBytes)
			reply, err := work.client.Transcribe(work.ctx, audio, name, []FormField{{"model", work.model}, {"language", "en"}, {"temperature", "0"}})
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
			reply, err := work.client.Transcribe(work.ctx, work.audio, marker+".wav", []FormField{{"model", work.model}, {"prompt", prompt}, {"language", "en"}, {"temperature", "0"}})
			if err != nil || reply.Status != http.StatusOK {
				t.Fatal("prompt/reset control failed")
			}
		}
		if work.privacy == nil || work.privacy(work.ctx, marker) != nil {
			t.Fatal("privacy observation unavailable or violated")
		}
	})
}

func TestLocalFIFOAndQueuedCancellation(t *testing.T) {
	runFIFOAndQueuedCancellation(t, openLocalEnvironment(t, func(settings *config.Settings) { settings.Queue.WaitingCapacity = 2 }))
}

func runFIFOAndQueuedCancellation(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	runQueueWorkload(t, queueWorkload{
		ctx: e.ctx, model: e.settings.Models.Served, short: e.audio,
		cleanupTimeout: time.Duration(e.settings.Timeouts.CleanupSeconds) * time.Second,
		long:           readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes),
		client:         func(t *testing.T, id string) *PublicClient { return identifiedLocalClient(t, e, id) },
		logs:           func(ctx context.Context) ([]byte, error) { return e.target.Logs(ctx, "api") },
		released: func(ctx context.Context) error {
			state, err := e.target.Observe(ctx)
			if err != nil {
				return err
			}
			return releasedAPIResources(state)
		},
	})
}
