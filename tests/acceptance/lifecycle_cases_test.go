package acceptance

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/httpapi"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func lifecycleDeadlineResult(result lifecycleCallResult) bool {
	return result.err == nil && result.reply.Status == http.StatusRequestTimeout && result.reply.ErrorCode == "operation_timeout"
}

func TestLifecycleDeadlineUsesCanonicalPublicFailure(t *testing.T) {
	mapped := httpapi.MapFailure(&domain.Failure{Kind: domain.DeadlineExceeded})
	result := lifecycleCallResult{reply: PublicReply{Status: mapped.Status, ErrorCode: mapped.Code}}
	if !lifecycleDeadlineResult(result) {
		t.Fatal("documented public deadline response was rejected")
	}
	for _, wrong := range []lifecycleCallResult{
		{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "backend_unavailable"}},
		{reply: PublicReply{Status: http.StatusRequestTimeout, ErrorCode: "deadline_exceeded"}},
		{err: context.DeadlineExceeded},
	} {
		if lifecycleDeadlineResult(wrong) {
			t.Fatal("non-delivered or noncanonical deadline response was accepted")
		}
	}
}

type lifecycleDeadlineEvidence struct {
	HTTPStatus                                                                                               int    `json:"http_status"`
	ErrorCode                                                                                                string `json:"error_code"`
	RequestError, TransportError, ContextDeadline, ContextCanceled, EOF, UnexpectedEOF, HTTPResponseReceived bool
	PublicDurationSeconds                                                                                    float64
	BeforeGeneration, BusyGeneration                                                                         string
	AfterGeneration                                                                                          *string
	OperationSeconds, CleanupSeconds, DeclaredClientBudgetSeconds                                            int
}

func lifecycleDeadlineFacts(result lifecycleCallResult, before, busy DockerSnapshot) lifecycleDeadlineEvidence {
	fact := lifecycleDeadlineEvidence{HTTPStatus: result.reply.Status, HTTPResponseReceived: result.reply.Status != 0, RequestError: result.err != nil, ContextDeadline: errors.Is(result.err, context.DeadlineExceeded), ContextCanceled: errors.Is(result.err, context.Canceled), EOF: errors.Is(result.err, io.EOF), UnexpectedEOF: errors.Is(result.err, io.ErrUnexpectedEOF), PublicDurationSeconds: result.reply.PublicDuration.Seconds()}
	switch result.reply.ErrorCode {
	case "operation_timeout", "backend_unavailable", "not_ready", "internal_error", "shutting_down", "gpu_unavailable":
		fact.ErrorCode = result.reply.ErrorCode
	case "":
	default:
		fact.ErrorCode = "unrecognized"
	}
	var network net.Error
	fact.TransportError = errors.As(result.err, &network)
	if before.PrivateHealth != nil {
		fact.BeforeGeneration = before.PrivateHealth.ProcessGeneration
	}
	if busy.PrivateHealth != nil {
		fact.BusyGeneration = busy.PrivateHealth.ProcessGeneration
	}
	return fact
}

func recordLifecycleDeadlineFacts(t *testing.T, e *acceptanceEnvironment, result lifecycleCallResult, before, busy DockerSnapshot) {
	t.Helper()
	fact := lifecycleDeadlineFacts(result, before, busy)
	fact.OperationSeconds = e.settings.Timeouts.OperationSeconds
	fact.CleanupSeconds = e.settings.Timeouts.CleanupSeconds
	fact.DeclaredClientBudgetSeconds = fact.OperationSeconds + fact.CleanupSeconds + 5
	data, err := json.MarshalIndent(fact, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "lifecycle-operation-deadline-outcome.json"), data, 0600) != nil {
		t.Error("cannot retain safe deadline outcome facts")
	}
	t.Logf("deadline outcome: response_received=%t status=%d code=%q request_error=%t transport_error=%t context_deadline=%t context_canceled=%t eof=%t unexpected_eof=%t duration=%s busy_generation_unchanged=%t", fact.HTTPResponseReceived, fact.HTTPStatus, fact.ErrorCode, fact.RequestError, fact.TransportError, fact.ContextDeadline, fact.ContextCanceled, fact.EOF, fact.UnexpectedEOF, result.reply.PublicDuration, fact.BeforeGeneration != "" && fact.BeforeGeneration == fact.BusyGeneration)
}

func TestLifecycleDeadlineFactsPreserveCausesWithoutContents(t *testing.T) {
	result := lifecycleCallResult{reply: PublicReply{ErrorCode: "PRIVATE-CONTENT", Text: "PRIVATE-CONTENT"}, err: errors.Join(context.DeadlineExceeded, errors.New("PRIVATE-CONTENT"))}
	fact := lifecycleDeadlineFacts(result, DockerSnapshot{}, DockerSnapshot{})
	data, err := json.Marshal(fact)
	if err != nil || !fact.ContextDeadline || fact.ContextCanceled || fact.ErrorCode != "unrecognized" || fact.AfterGeneration != nil || strings.Contains(string(data), "PRIVATE-CONTENT") {
		t.Fatal("deadline diagnostics lost its cause or retained raw private contents")
	}
}

// writeAndRecreate changes the existing bind inode, validates through the
// real loader, and refreshes only this environment's pair/address/client. It does
// not require readiness: callers may deliberately test runtime startup failure.
func writeAndRecreate(t *testing.T, e *acceptanceEnvironment, settings config.Settings) {
	t.Helper()
	if err := reconfigureLifecycle(e.ctx, e, settings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.client.Close)
}

func reconfigureLifecycle(ctx context.Context, e *acceptanceEnvironment, settings config.Settings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return errors.New("cannot encode local lifecycle settings")
	}
	candidate := filepath.Join(e.workdir, "lifecycle-next.json")
	if os.WriteFile(candidate, data, 0600) != nil {
		return errors.New("cannot stage local lifecycle settings")
	}
	if _, err := config.Load([]string{"--config", candidate}, []string{}); err != nil {
		return errors.New("actual configuration loader rejected lifecycle settings")
	}
	file, err := os.OpenFile(e.config, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return errors.New("cannot open the existing bound configuration inode")
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write the existing bound configuration inode")
	}
	e.settings = settings
	if err := e.target.Recreate(ctx); err != nil {
		return err
	}
	if err := refreshLifecycleClient(ctx, e); err != nil {
		return err
	}
	if e.selection.Scope == publishedAcceptanceScope {
		return recordPublishedBinding(ctx, e)
	}
	return nil
}

func refreshLifecycleClient(ctx context.Context, e *acceptanceEnvironment) error {
	address, err := e.target.APIAddress(ctx)
	if err != nil {
		return err
	}
	client, err := NewPublicClient(address, time.Duration(e.settings.Timeouts.OperationSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second, 1<<20)
	if err != nil {
		return err
	}
	if e.client != nil {
		e.client.Close()
	}
	e.address, e.client = address, client
	// Keep the final restored/reused client's cleanup tied to its environment,
	// including when a creating subtest's own cleanup already ran.
	context.AfterFunc(e.ctx, client.Close)
	return nil
}

func lifecycleSnapshotFacts(s DockerSnapshot, state string) error {
	if s.PrivateHealth == nil || s.PrivateHealth.HTTPStatus != http.StatusOK || s.PrivateHealth.Status != state || !s.PrivateHealth.CUDAReady || s.PrivateHealth.Model == "" || s.PrivateHealth.ProcessGeneration == "" || s.PrivateHealth.ProtocolVersion == "" || s.PrivateHealth.ConfigurationFingerprint == "" || s.NativeHostPID == nil || *s.NativeHostPID <= 0 || s.NativeNamespacePID == nil || *s.NativeNamespacePID <= 1 {
		return errors.New("required lifecycle health/process observation is unavailable")
	}
	for _, r := range []DockerResourceSnapshot{s.API, s.Backend} {
		if r.RSSBytes == nil || r.FDCount == nil || r.ScratchFileCount == nil || r.ScratchBytes == nil {
			return errors.New("required lifecycle resource observation is unavailable")
		}
	}
	return nil
}

func lifecycleScratchStable(before, after DockerSnapshot) error {
	for _, pair := range [][2]DockerResourceSnapshot{{before.API, after.API}, {before.Backend, after.Backend}} {
		if pair[0].ScratchFileCount == nil || pair[1].ScratchFileCount == nil || pair[0].ScratchBytes == nil || pair[1].ScratchBytes == nil {
			return errors.New("lifecycle scratch cleanup observation is unavailable")
		}
		if *pair[1].ScratchFileCount > *pair[0].ScratchFileCount || *pair[1].ScratchBytes > *pair[0].ScratchBytes {
			return errors.New("lifecycle terminal path accumulated scratch files or bytes")
		}
	}
	return nil
}

func lifecycleOwnedGPUAllocations(reader io.Reader, owned map[int]struct{}) (map[int]uint64, error) {
	result := make(map[int]uint64)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 64<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		pidText, memoryText, ok := strings.Cut(line, ",")
		pid, err := strconv.Atoi(strings.TrimSpace(pidText))
		if !ok || err != nil || pid <= 0 {
			return nil, errors.New("GPU process observation is malformed")
		}
		if _, wanted := owned[pid]; !wanted {
			continue
		} // Never retain unrelated allocation rows.
		mib, err := strconv.ParseUint(strings.TrimSpace(memoryText), 10, 64)
		if err != nil || mib > ^uint64(0)/(1<<20) {
			return nil, errors.New("owned GPU allocation is unavailable")
		}
		if _, duplicate := result[pid]; duplicate {
			return nil, errors.New("owned GPU process observation is ambiguous")
		}
		result[pid] = mib * (1 << 20)
	}
	if scanner.Err() != nil {
		return nil, errors.New("GPU process observation exceeded its read bound")
	}
	return result, nil
}

func lifecycleGPUQuery(parent context.Context, owned map[int]struct{}) (map[int]uint64, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-i", "0", "--query-compute-apps=pid,used_gpu_memory", "--format=csv,noheader,nounits")
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 200 * time.Millisecond
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("cannot open owned GPU observation")
	}
	if cmd.Start() != nil {
		return nil, errors.New("cannot start owned GPU observation")
	}
	limited := &io.LimitedReader{R: pipe, N: (1 << 20) + 1}
	allocations, readErr := lifecycleOwnedGPUAllocations(limited, owned)
	if readErr != nil || limited.N == 0 {
		cancel()
	}
	waitErr := cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil || limited.N == 0 {
		return nil, errors.New("owned GPU observation failed or exceeded its bound")
	}
	return allocations, nil
}

func lifecyclePIDStart(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", err
	}
	split := strings.LastIndex(string(data), ") ")
	if split < 0 {
		return "", errors.New("owned process identity unavailable")
	}
	fields := strings.Fields(string(data[split+2:]))
	if len(fields) < 20 {
		return "", errors.New("owned process identity unavailable")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New("owned process identity unavailable")
	}
	return fields[19], nil
}

type lifecycleTrace struct {
	Samples, UnavailableSamples, PeakNativeProcesses, PeakOwnedGPUProcesses, ReplacementSamples int
	ScheduledCadenceMillis                                                                      int
	OldGoneBeforeReplacement                                                                    bool
	Err                                                                                         error `json:"-"`
}

func watchLifecycleReplacement(t *testing.T, e *acceptanceEnvironment, before DockerSnapshot) func() lifecycleTrace {
	t.Helper()
	old := *before.NativeHostPID
	identity, err := lifecyclePIDStart(old)
	if err != nil {
		t.Fatal("cannot observe the selected old native process identity")
	}
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan lifecycleTrace, 1)
	armed := make(chan error, 1)
	go func() {
		trace := lifecycleTrace{ScheduledCadenceMillis: 100, OldGoneBeforeReplacement: true}
		known := map[int]struct{}{old: {}}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		first := true
		defer func() { done <- trace }()
		for {
			// This unlocked read helper is used only while target ownership is stable:
			// no Down/Up runs until this observer has stopped and joined.
			processes, processErr := e.target.processes(ctx, "backend")
			nativeCount := 0
			if processErr == nil {
				for _, p := range processes {
					if p.Command != "whisper-server" {
						continue
					}
					nativeCount++
					known[p.PID] = struct{}{}
					if p.PID != old {
						trace.ReplacementSamples++
						start, err := lifecyclePIDStart(old)
						if err == nil && start == identity {
							trace.OldGoneBeforeReplacement = false
							trace.Err = errors.New("replacement native observed before old process was reaped")
						} else if err != nil && !errors.Is(err, os.ErrNotExist) {
							trace.Err = errors.New("old native retirement observation unavailable")
						}
					}
				}
			}
			// Drop an old numeric PID if the host has reused it for another process.
			if start, err := lifecyclePIDStart(old); err == nil && start != identity {
				delete(known, old)
			}
			allocations, gpuErr := lifecycleGPUQuery(ctx, known)
			if ctx.Err() != nil {
				return
			}
			if processErr != nil || gpuErr != nil {
				trace.UnavailableSamples++
			} else {
				trace.Samples++
				trace.PeakNativeProcesses = max(trace.PeakNativeProcesses, nativeCount)
				count := 0
				for _, bytes := range allocations {
					if bytes > 0 {
						count++
					}
				}
				trace.PeakOwnedGPUProcesses = max(trace.PeakOwnedGPUProcesses, count)
				if nativeCount > 1 || count > 1 {
					trace.Err = errors.New("overlapping owned native/GPU processes observed")
				}
			}
			if first {
				first = false
				if processErr != nil || gpuErr != nil || allocations[old] == 0 || trace.Err != nil {
					armed <- errors.New("initial owned native GPU allocation could not be observed")
				} else {
					armed <- nil
				}
			}
			if trace.Err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	var result lifecycleTrace
	finish := func() lifecycleTrace {
		once.Do(func() {
			cancel()
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				result.Err = errors.New("lifecycle observation did not stop within its probe budget")
			}
		})
		return result
	}
	t.Cleanup(func() {
		if report := finish(); report.Err != nil {
			t.Error(report.Err)
		}
	})
	select {
	case err := <-armed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle GPU observation did not arm within its probe budget")
	}
	return finish
}

type lifecycleCall struct {
	cancel   context.CancelFunc
	done     chan lifecycleCallResult
	consumed bool
}
type lifecycleCallResult struct {
	reply PublicReply
	err   error
}

func startLifecycleCall(t *testing.T, e *acceptanceEnvironment, audio []byte, filename string) *lifecycleCall {
	t.Helper()
	ctx, cancel := context.WithCancel(e.ctx)
	call := &lifecycleCall{cancel: cancel, done: make(chan lifecycleCallResult, 1)}
	client, model := e.client, e.settings.Models.Served
	go func() {
		reply, err := client.Transcribe(ctx, audio, filename, []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}})
		call.done <- lifecycleCallResult{reply: reply, err: err}
	}()
	t.Cleanup(func() {
		cancel()
		if !call.consumed {
			select {
			case <-call.done:
				call.consumed = true
			case <-time.After(5 * time.Second):
				t.Error("owned lifecycle request did not stop after cancellation")
			}
		}
	})
	return call
}
func awaitLifecycleCall(t *testing.T, call *lifecycleCall, budget time.Duration) lifecycleCallResult {
	t.Helper()
	select {
	case result := <-call.done:
		call.consumed = true
		return result
	case <-time.After(budget):
		t.Fatal("lifecycle request did not finish within its declared bound")
		return lifecycleCallResult{}
	}
}

func readyLifecycleSnapshot(t *testing.T, e *acceptanceEnvironment) DockerSnapshot {
	t.Helper()
	snapshot, err := e.target.Observe(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycleSnapshotFacts(snapshot, "ready"); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := config.CanonicalFingerprint(e.settings)
	if err != nil || snapshot.PrivateHealth.Model != e.settings.Models.Served || snapshot.PrivateHealth.ConfigurationFingerprint != fingerprint {
		t.Fatal("lifecycle target model/configuration identity differs from selected settings")
	}
	return snapshot
}

func waitLifecycleBusy(t *testing.T, e *acceptanceEnvironment, call *lifecycleCall, generation string) DockerSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.OperationSeconds)*time.Second)
	defer cancel()
	for {
		select {
		case <-call.done:
			call.consumed = true
			t.Fatal("long fixture completed or rejected before an actual Busy cancellation barrier")
		default:
		}
		snapshot, err := e.target.Observe(ctx)
		if err != nil {
			t.Fatal("actual Busy observation is unavailable", err)
		}
		if snapshot.PrivateHealth != nil && snapshot.PrivateHealth.Status == "busy" {
			if err := lifecycleSnapshotFacts(snapshot, "busy"); err != nil {
				t.Fatal(err)
			}
			if snapshot.PrivateHealth.ProcessGeneration != generation {
				t.Fatal("native generation changed before lifecycle fault injection")
			}
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatal("valid long fixture did not reach active Busy within its operation budget")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func waitLifecycleUnready(parent context.Context, address string, unreachable bool, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/health/ready", nil)
		if err != nil {
			return errors.New("invalid lifecycle readiness endpoint")
		}
		response, err := client.Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
		}
		if ctx.Err() != nil {
			return errors.New("truthful unready state was not observed within its declared bound")
		}
		if err != nil {
			if unreachable {
				return nil
			}
			return errors.New("running API readiness unexpectedly became unreachable")
		}
		if response.StatusCode == http.StatusServiceUnavailable {
			return nil
		}
		if response.StatusCode != http.StatusOK {
			return errors.New("readiness returned an unsupported lifecycle status")
		}
		select {
		case <-ctx.Done():
			return errors.New("truthful unready state was not observed within its declared bound")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func waitLifecycleHealthy(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	if err := waitLocalReady(e.ctx, e.target, e.address, time.Duration(e.settings.Timeouts.ProvisioningSeconds)*time.Second); err != nil {
		t.Fatal(err)
	}
}
func nextLifecycleValid(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}, {"language", "en"}, {"temperature", "0"}})
	if err != nil || reply.Status != http.StatusOK || strings.TrimSpace(reply.Text) == "" || !identifiedPublicSpeech(reply.Text) {
		t.Fatalf("fresh valid transcription failed after lifecycle recovery: status=%d transport_error=%t speech=%+v", reply.Status, err != nil, observePublicSpeech(reply.Text))
	}
}

func restoreLifecycleCase(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	data, err := json.Marshal(e.settings)
	if err != nil {
		t.Fatal(err)
	}
	var saved config.Settings
	if json.Unmarshal(data, &saved) != nil {
		t.Fatal("cannot retain original lifecycle settings")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, role := range []string{"api", "backend"} {
			probe, stop := context.WithTimeout(ctx, 3*time.Second)
			logs, err := e.target.Logs(probe, role)
			stop()
			if err == nil {
				name := "lifecycle-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + role + ".log"
				if os.WriteFile(filepath.Join(e.workdir, name), logs, 0600) != nil {
					t.Log("private lifecycle diagnostics could not be retained")
				}
			} else {
				t.Log("bounded private lifecycle diagnostics unavailable", role)
			}
		}
		if err := reconfigureLifecycle(ctx, e, saved); err != nil {
			t.Error("cannot restore original lifecycle configuration and pair", err)
			return
		}
		if err := waitLocalReady(ctx, e.target, e.address, time.Duration(saved.Timeouts.ProvisioningSeconds)*time.Second); err != nil {
			t.Error("restored lifecycle pair did not become ready", err)
		}
	})
}

func lifecyclePreparedIdentity(e *acceptanceEnvironment) (string, error) {
	file, err := os.Open(filepath.Join(e.prepared, "base.bin"))
	if err != nil {
		return "", errors.New("owned prepared model is unavailable")
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil || written == 0 {
		return "", errors.New("owned prepared model identity is unavailable")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func lifecycleReport(t *testing.T, e *acceptanceEnvironment, name string, overrides map[string]any, trace *lifecycleTrace) {
	t.Helper()
	data, err := json.MarshalIndent(struct {
		Scope, Case, RuntimeIdentity string
		Overrides                    map[string]any
		SampledObservation           *lifecycleTrace
		ObservationLimit             string
	}{e.selection.Scope, name, e.selection.RuntimeInputIdentity, overrides, trace, "100ms scheduled sampling plus CLI latency; gaps are counted; not continuous GPU overlap proof"}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(e.workdir, "lifecycle-"+name+".json"), data, 0600) != nil {
		t.Fatal("cannot retain sanitized local lifecycle observations")
	}
}

func lifecycleBackendInferenceCount(t *testing.T, e *acceptanceEnvironment) int {
	t.Helper()
	logs, err := e.target.Logs(e.ctx, "backend")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(logs)))
	scanner.Buffer(make([]byte, 1024), 64<<10)
	known, count := false, 0
	for scanner.Scan() {
		line := scanner.Text()
		index := strings.IndexByte(line, '{')
		if index < 0 {
			continue
		}
		var event struct{ Event, Route, Method string }
		if json.Unmarshal([]byte(line[index:]), &event) != nil {
			continue
		}
		if event.Event == "request_completed" {
			known = true
			if event.Route == "/inference" && event.Method == http.MethodPost {
				count++
			}
		}
	}
	if scanner.Err() != nil || !known {
		t.Fatal("backend request observations unavailable; cannot infer zero submitted work")
	}
	return count
}

func lifecycleTraceFailure(trace lifecycleTrace, replacementRequired bool) error {
	if trace.Err != nil || trace.Samples == 0 || trace.PeakNativeProcesses != 1 || trace.PeakOwnedGPUProcesses != 1 || !trace.OldGoneBeforeReplacement || replacementRequired && trace.ReplacementSamples == 0 {
		return errors.New("sampled native/GPU replacement ownership check failed")
	}
	return nil
}

func lifecycleTracePassed(t *testing.T, trace lifecycleTrace, replacementRequired bool) {
	t.Helper()
	if err := lifecycleTraceFailure(trace, replacementRequired); err != nil {
		t.Fatal(err, trace.Err)
	}
}

func lifecycleOldProcessGone(t *testing.T, e *acceptanceEnvironment, pid int, start string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.CleanupSeconds)*time.Second)
	defer cancel()
	for {
		current, err := lifecyclePIDStart(pid)
		if errors.Is(err, os.ErrNotExist) || err == nil && current != start {
			return
		}
		if err != nil {
			t.Fatal("old native retirement could not be observed")
		}
		select {
		case <-ctx.Done():
			t.Fatal("old native process was not reaped within cleanup bounds")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func lifecycleOldGPUAllocationGone(t *testing.T, e *acceptanceEnvironment, pid int, start string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(e.settings.Timeouts.CleanupSeconds)*time.Second)
	defer cancel()
	samples := 0
	for {
		if current, err := lifecyclePIDStart(pid); err == nil && current != start {
			t.Fatal("retired native PID was reused before GPU release could be attributed")
		}
		allocations, err := lifecycleGPUQuery(ctx, map[int]struct{}{pid: {}})
		if err != nil {
			t.Fatal("retired owned native GPU allocation is unavailable", err)
		}
		samples++
		if allocations[pid] == 0 {
			return samples
		}
		select {
		case <-ctx.Done():
			t.Fatal("retired owned native GPU allocation remained beyond cleanup bounds")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func runLifecycleCases(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	t.Cleanup(func() {
		if e.client != nil {
			e.client.Close()
		}
	})
	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{"enabled_limiter", func(t *testing.T) {
			settings := e.settings
			settings.API.RateLimit.Enabled = true
			settings.API.RateLimit.RequestsPerSecond = 0.01
			settings.API.RateLimit.Burst = 1
			settings.API.RateLimit.IdleSeconds = max(100, settings.API.RateLimit.IdleSeconds)
			writeAndRecreate(t, e, settings)
			waitLifecycleHealthy(t, e)
			before := readyLifecycleSnapshot(t, e)
			count := lifecycleBackendInferenceCount(t, e)
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			reply, err := e.client.Transcribe(e.ctx, e.audio, "jfk.wav", []FormField{{"model", e.settings.Models.Served}})
			if err != nil || reply.Status != http.StatusTooManyRequests || reply.ErrorCode != "rate_limit_exceeded" {
				t.Fatal("enabled dedicated limiter did not reject the second request with its declared code")
			}
			after := readyLifecycleSnapshot(t, e)
			if lifecycleBackendInferenceCount(t, e) != count+1 || before.PrivateHealth.ProcessGeneration != after.PrivateHealth.ProcessGeneration {
				t.Fatal("limiter rejection submitted work or replaced residency")
			}
			lifecycleReport(t, e, "enabled-limiter", map[string]any{"enabled": true, "requests_per_second": 0.01, "burst": 1, "idle_seconds": settings.API.RateLimit.IdleSeconds}, nil)
		}},
		{"active_client_cancel", func(t *testing.T) {
			before := readyLifecycleSnapshot(t, e)
			finish := watchLifecycleReplacement(t, e, before)
			long := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
			call := startLifecycleCall(t, e, long, "jfk-900.flac")
			waitLifecycleBusy(t, e, call, before.PrivateHealth.ProcessGeneration)
			call.cancel()
			result := awaitLifecycleCall(t, call, time.Duration(e.settings.Timeouts.CleanupSeconds+5)*time.Second)
			if !errors.Is(result.err, context.Canceled) || result.reply.Status == http.StatusOK {
				t.Fatal("active cancellation was reported as successful transcription")
			}
			if err := waitLifecycleUnready(e.ctx, e.address, false, time.Duration(e.settings.Timeouts.CleanupSeconds)*time.Second); err != nil {
				t.Fatal(err)
			}
			waitLifecycleHealthy(t, e)
			after := readyLifecycleSnapshot(t, e)
			if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
				t.Fatal("active cancellation reused the uncertain native generation")
			}
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			trace := finish()
			lifecycleTracePassed(t, trace, true)
			lifecycleReport(t, e, "active-cancel", nil, &trace)
		}},
		{"active_operation_deadline", func(t *testing.T) {
			originalOperationSeconds := e.settings.Timeouts.OperationSeconds
			settings := e.settings
			settings.Timeouts.OperationSeconds = 3
			writeAndRecreate(t, e, settings)
			waitLifecycleHealthy(t, e)
			before := readyLifecycleSnapshot(t, e)
			finish := watchLifecycleReplacement(t, e, before)
			long := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
			call := startLifecycleCall(t, e, long, "jfk-900.flac")
			busy := waitLifecycleBusy(t, e, call, before.PrivateHealth.ProcessGeneration)
			result := awaitLifecycleCall(t, call, time.Duration(e.settings.Timeouts.OperationSeconds+e.settings.Timeouts.CleanupSeconds+5)*time.Second)
			recordLifecycleDeadlineFacts(t, e, result, before, busy)
			if !lifecycleDeadlineResult(result) {
				t.Fatal("active operation deadline did not yield its documented public failure")
			}
			waitLifecycleHealthy(t, e)
			after := readyLifecycleSnapshot(t, e)
			if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
				t.Fatal("active deadline reused the uncertain native generation")
			}
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			trace := finish()
			lifecycleTracePassed(t, trace, true)
			lifecycleReport(t, e, "operation-deadline", map[string]any{"operation_seconds": 3, "original_operation_seconds": originalOperationSeconds, "fixture_decoded_seconds": 900}, &trace)
		}},
		{"api_death", func(t *testing.T) { runLifecycleContainerDeath(t, e, "api") }},
		{"backend_death", func(t *testing.T) { runLifecycleContainerDeath(t, e, "backend") }},
		{"native_crash", func(t *testing.T) {
			before := readyLifecycleSnapshot(t, e)
			finish := watchLifecycleReplacement(t, e, before)
			long := readLocalAudio(t, filepath.Join(e.selection.FixtureDirectory, "jfk-900.flac"), e.settings.Limits.AudioBytes)
			call := startLifecycleCall(t, e, long, "jfk-900.flac")
			busy := waitLifecycleBusy(t, e, call, before.PrivateHealth.ProcessGeneration)
			ids, err := e.target.Containers(e.ctx)
			if err != nil {
				t.Fatal(err)
			}
			const killNative = `case "$1" in ''|*[!0-9]*) exit 1;; esac; [ "$1" -gt 1 ]; IFS= read -r name < "/proc/$1/comm"; [ "$name" = whisper-server ]; kill -KILL "$1"`
			if _, err := e.target.run(e.ctx, "owned native crash", false, "exec", ids.BackendID, "sh", "-eu", "-c", killNative, "p07-owned-native", strconv.Itoa(*busy.NativeNamespacePID)); err != nil {
				t.Fatal(err)
			}
			if err := waitLifecycleUnready(e.ctx, e.address, false, time.Duration(e.settings.Timeouts.CleanupSeconds)*time.Second); err != nil {
				t.Fatal(err)
			}
			result := awaitLifecycleCall(t, call, time.Duration(e.settings.Timeouts.CleanupSeconds+5)*time.Second)
			if result.err == nil && result.reply.Status == http.StatusOK {
				t.Fatal("crashed active native inference fabricated success")
			}
			waitLifecycleHealthy(t, e)
			after := readyLifecycleSnapshot(t, e)
			if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
				t.Fatal("supervised native crash did not create a new generation")
			}
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			trace := finish()
			lifecycleTracePassed(t, trace, true)
			lifecycleReport(t, e, "native-crash", nil, &trace)
		}},
		{"ordinary_restart", func(t *testing.T) {
			before := readyLifecycleSnapshot(t, e)
			identity, err := lifecyclePreparedIdentity(e)
			if err != nil {
				t.Fatal(err)
			}
			finish := watchLifecycleReplacement(t, e, before)
			if err := e.target.Restart(e.ctx, "backend"); err != nil {
				t.Fatal(err)
			}
			if err := refreshLifecycleClient(e.ctx, e); err != nil {
				t.Fatal(err)
			}
			waitLifecycleHealthy(t, e)
			after := readyLifecycleSnapshot(t, e)
			if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
				t.Fatal("ordinary backend restart reused process-local residency")
			}
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			retained, err := lifecyclePreparedIdentity(e)
			if err != nil || retained != identity {
				t.Fatal("restart changed verified prepared model bytes")
			}
			trace := finish()
			lifecycleTracePassed(t, trace, true)
			lifecycleReport(t, e, "restart", nil, &trace)
		}},
		{"down_up_retains_prepared", func(t *testing.T) {
			before := readyLifecycleSnapshot(t, e)
			identity, err := lifecyclePreparedIdentity(e)
			if err != nil {
				t.Fatal(err)
			}
			start, err := lifecyclePIDStart(*before.NativeHostPID)
			if err != nil {
				t.Fatal("old native process identity unavailable")
			}
			down := false
			t.Cleanup(func() {
				if down {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					if err := e.target.Up(ctx); err != nil {
						t.Error(err)
					}
				}
			})
			if err := e.target.Down(e.ctx); err != nil {
				t.Fatal(err)
			}
			down = true
			lifecycleOldProcessGone(t, e, *before.NativeHostPID, start)
			gpuReleaseSamples := lifecycleOldGPUAllocationGone(t, e, *before.NativeHostPID, start)
			retained, err := lifecyclePreparedIdentity(e)
			if err != nil || retained != identity {
				t.Fatal("Down changed verified prepared model bytes")
			}
			if err := e.target.Up(e.ctx); err != nil {
				t.Fatal(err)
			}
			down = false
			if err := refreshLifecycleClient(e.ctx, e); err != nil {
				t.Fatal(err)
			}
			waitLifecycleHealthy(t, e)
			after := readyLifecycleSnapshot(t, e)
			if after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
				t.Fatal("Down/Up reused the old resident generation")
			}
			nextLifecycleValid(t, e)
			verifyLifecycleScratch(t, e, before)
			retained, err = lifecyclePreparedIdentity(e)
			if err != nil || retained != identity {
				t.Fatal("Down/Up changed verified prepared model bytes")
			}
			lifecycleReport(t, e, "down-up", map[string]any{"old_native_reaped_before_up": true, "prepared_sha256_unchanged": true, "old_owned_gpu_allocation_absent_before_up": true, "gpu_release_samples": gpuReleaseSamples}, nil)
		}},
	}
	for _, test := range cases {
		if !t.Run(test.name, func(t *testing.T) { restoreLifecycleCase(t, e); test.run(t) }) {
			return
		}
	}
}

func runLifecycleContainerDeath(t *testing.T, e *acceptanceEnvironment, role string) {
	t.Helper()
	before := readyLifecycleSnapshot(t, e)
	finish := watchLifecycleReplacement(t, e, before)
	ids, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := ids.APIID
	if role == "backend" {
		id = ids.BackendID
	}
	// Target selection is an observed ID from this owned Compose project only.
	if _, err := e.target.run(e.ctx, "owned component death", false, "kill", "--signal", "KILL", id); err != nil {
		t.Fatal(err)
	}
	if err := waitLifecycleUnready(e.ctx, e.address, role == "api", time.Duration(e.settings.Timeouts.CleanupSeconds)*time.Second); err != nil {
		t.Fatal(err)
	}
	state, err := e.target.Inspect(e.ctx, role)
	if err != nil || state.Running {
		t.Fatal("killed Compose component unexpectedly auto-restarted or could not be observed")
	}
	if err := e.target.Restart(e.ctx, role); err != nil {
		t.Fatal(err)
	}
	if err := refreshLifecycleClient(e.ctx, e); err != nil {
		t.Fatal(err)
	}
	waitLifecycleHealthy(t, e)
	after := readyLifecycleSnapshot(t, e)
	if role == "backend" && after.PrivateHealth.ProcessGeneration == before.PrivateHealth.ProcessGeneration {
		t.Fatal("explicit backend restart reused dead residency")
	}
	nextLifecycleValid(t, e)
	verifyLifecycleScratch(t, e, before)
	trace := finish()
	lifecycleTracePassed(t, trace, role == "backend")
	lifecycleReport(t, e, role+"-death", map[string]any{"explicit_component_restart": true, "fault_injected_while_idle": true}, &trace)
}

func verifyLifecycleScratch(t *testing.T, e *acceptanceEnvironment, before DockerSnapshot) {
	t.Helper()
	if err := lifecycleScratchStable(before, readyLifecycleSnapshot(t, e)); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleReconfigurePreservesBindAndDoesNotRequireReady(t *testing.T) {
	target, dir := dockerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := target.Down(context.Background()); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"not_ready"}}`))
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"text":"country"}`))
	}))
	defer server.Close()
	t.Setenv("P07_DOCKER_HELPER_PORT", strings.TrimPrefix(server.URL, "http://"))
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	settings.Timeouts.OperationSeconds = 3
	settings.API.RateLimit.Enabled = false
	path := filepath.Join(dir, "config.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	e := &acceptanceEnvironment{ctx: ctx, target: target, settings: settings, workdir: dir, config: path}
	if err := reconfigureLifecycle(ctx, e, settings); err != nil {
		t.Fatal("runtime-unready reconfiguration was rejected", err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("reconfiguration replaced the mounted config inode")
	}
	loaded, err := config.Load([]string{"--config", path}, []string{})
	if err != nil || loaded.Timeouts.OperationSeconds != 3 || loaded.API.RateLimit.Enabled {
		t.Fatal("actual loader did not observe new lifecycle settings")
	}
	reply, err := e.client.Transcribe(ctx, []byte("synthetic"), "sample.wav", nil)
	if err != nil || reply.Status != http.StatusOK || reply.Text != "country" || e.address != server.URL {
		t.Fatal("reconfiguration did not refresh the owned API client/address")
	}
}

func TestLifecycleScratchGrowthIsNotHiddenByControlFiles(t *testing.T) {
	one, two, zero := uint64(1), uint64(2), uint64(0)
	baseline := DockerSnapshot{API: DockerResourceSnapshot{ScratchFileCount: &one, ScratchBytes: &zero}, Backend: DockerResourceSnapshot{ScratchFileCount: &zero, ScratchBytes: &zero}}
	if lifecycleScratchStable(baseline, baseline) != nil {
		t.Fatal("stable known empty ownership control file was rejected")
	}
	leaked := baseline
	leaked.API.ScratchFileCount = &two
	if lifecycleScratchStable(baseline, leaked) == nil {
		t.Fatal("lifecycle scratch file accumulation was accepted")
	}
	unknown := baseline
	unknown.API.ScratchBytes = nil
	if lifecycleScratchStable(baseline, unknown) == nil {
		t.Fatal("unknown scratch bytes became measured zero")
	}
}

func TestLifecycleFactsRejectUnknownNotMeasuredZero(t *testing.T) {
	zero, rss, fds := uint64(0), uint64(4096), uint64(3)
	host, namespace := 1234, 42
	good := DockerSnapshot{PrivateHealth: &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: "busy", Model: "base", ProcessGeneration: "generation", ProtocolVersion: "1", ConfigurationFingerprint: "fingerprint", CUDAReady: true}, NativeHostPID: &host, NativeNamespacePID: &namespace, API: DockerResourceSnapshot{RSSBytes: &rss, FDCount: &fds, ScratchFileCount: &zero, ScratchBytes: &zero}, Backend: DockerResourceSnapshot{RSSBytes: &rss, FDCount: &fds, ScratchFileCount: &zero, ScratchBytes: &zero}}
	if err := lifecycleSnapshotFacts(good, "busy"); err != nil {
		t.Fatal("known empty scratch was treated as unavailable")
	}
	for _, mode := range []string{"health", "host", "namespace", "resource", "cpu", "generation", "wrong-state"} {
		t.Run(mode, func(t *testing.T) {
			bad := good
			health := *good.PrivateHealth
			bad.PrivateHealth = &health
			switch mode {
			case "health":
				bad.PrivateHealth = nil
			case "host":
				bad.NativeHostPID = nil
			case "namespace":
				bad.NativeNamespacePID = nil
			case "resource":
				bad.API.FDCount = nil
			case "cpu":
				bad.PrivateHealth.CUDAReady = false
			case "generation":
				bad.PrivateHealth.ProcessGeneration = ""
			case "wrong-state":
				bad.PrivateHealth.Status = "ready"
			}
			if lifecycleSnapshotFacts(bad, "busy") == nil {
				t.Fatal("unavailable runtime fact accepted")
			}
		})
	}
}

func TestLifecycleReplacementRequiresKnownNonOverlappingSamples(t *testing.T) {
	good := lifecycleTrace{Samples: 2, PeakNativeProcesses: 1, PeakOwnedGPUProcesses: 1, ReplacementSamples: 1, OldGoneBeforeReplacement: true}
	if lifecycleTraceFailure(good, true) != nil {
		t.Fatal("known sampled replacement was rejected")
	}
	for _, mode := range []string{"unknown", "gpu-unknown", "overlap", "replacement-unobserved", "old-not-reaped"} {
		t.Run(mode, func(t *testing.T) {
			bad := good
			switch mode {
			case "unknown":
				bad.Samples = 0
			case "gpu-unknown":
				bad.PeakOwnedGPUProcesses = 0
			case "overlap":
				bad.PeakOwnedGPUProcesses = 2
			case "replacement-unobserved":
				bad.ReplacementSamples = 0
			case "old-not-reaped":
				bad.OldGoneBeforeReplacement = false
			}
			if lifecycleTraceFailure(bad, true) == nil {
				t.Fatal("unavailable or overlapping replacement was accepted")
			}
		})
	}
}

func TestLifecycleGPUAllocationFilteringAndUnknown(t *testing.T) {
	owned := map[int]struct{}{1234: {}}
	got, err := lifecycleOwnedGPUAllocations(strings.NewReader("9999, PRIVATE_FOREIGN_ROW\n1234, 12\n"), owned)
	if err != nil || len(got) != 1 || got[1234] != 12*(1<<20) {
		t.Fatal("owned GPU allocation missing or unrelated row retained")
	}
	if _, err := lifecycleOwnedGPUAllocations(strings.NewReader("1234, N/A\n"), owned); err == nil {
		t.Fatal("unknown owned GPU allocation became measured zero")
	}
}
