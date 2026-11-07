package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// runClientCases must only be called by the parent-owned ready local target.
// Both upstream probes currently select base and the pinned WAV. This is genuine
// WAV transport evidence, not a client-by-codec matrix or complete Electron UI.
func runClientCases(t *testing.T, ctx context.Context, apiURL, fixturePath, privateEvidenceDir string) {
	t.Helper()
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("actual client slice requires an explicit credential-free loopback API root")
	}
	file, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal("public client fixture unavailable")
	}
	audio, err := io.ReadAll(io.LimitReader(file, 352079))
	_ = file.Close()
	digest := sha256.Sum256(audio)
	if err != nil || len(audio) != 352078 || hex.EncodeToString(digest[:]) != publicJFKIdentity {
		t.Fatal("actual client slice requires the bounded pinned public JFK WAV")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal("client project root unavailable")
	}
	absoluteFixture, err := filepath.Abs(fixturePath)
	if err != nil {
		t.Fatal("client fixture path unavailable")
	}
	base := strings.TrimRight(apiURL, "/") + "/v1"
	uv := filepath.Join(root, ".local", "uv-migration", "bin", "uv")
	var results []clientCaseResult
	for _, format := range []string{"json", "text"} {
		name := "official-openai-" + format
		t.Run(name, func(t *testing.T) {
			stdout, commandErr := runClientCaseCommand(ctx, root, uv, "run", "--locked", "python", "tests/acceptance/openai_client.py", "--base-url", base, "--audio", absoluteFixture, "--response-format", format, "--timeout-seconds", "120")
			result, validationErr := readClientCaseResult(stdout, "openai", format)
			result.Name, result.ExpectedVersion = name, "2.11.0"
			result.Succeeded = commandErr == nil && validationErr == nil
			results = append(results, result)
			if !result.Succeeded {
				t.Error("genuine OpenAI SDK transport failed; raw diagnostics and endpoint withheld")
			}
		})
	}
	t.Run("openwhispr-electron-upload", func(t *testing.T) {
		stdout, commandErr := runClientCaseCommand(ctx, root, "bash", filepath.Join(root, ".local", "p07", "clients", "run-electron-probe.sh"), "--base-url="+base, "--audio="+absoluteFixture)
		result, validationErr := readClientCaseResult(stdout, "openwhispr", "json")
		result.Name, result.ExpectedVersion = "openwhispr-electron-upload", "1.10.4"
		result.Succeeded = commandErr == nil && validationErr == nil
		results = append(results, result)
		if !result.Succeeded {
			t.Error("genuine OpenWhispr Electron upload failed; raw diagnostics and endpoint withheld")
		}
	})
	if err := os.MkdirAll(privateEvidenceDir, 0700); err != nil {
		t.Fatal("cannot create private client evidence directory")
	}
	summary, err := json.MarshalIndent(struct {
		Scope         string             `json:"scope"`
		Qualification string             `json:"qualification"`
		Results       []clientCaseResult `json:"results"`
	}{"actual-client-WAV-transport-only", "not_established", results}, "", "  ")
	if err != nil {
		t.Fatal("cannot serialize sanitized client results")
	}
	out, err := os.CreateTemp(privateEvidenceDir, "client-transport-*.json")
	if err != nil {
		t.Fatal("cannot create private client evidence")
	}
	_, writeErr := out.Write(summary)
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("cannot preserve sanitized client results")
	}
}

type clientCaseResult struct {
	Name                 string `json:"name"`
	ExpectedVersion      string `json:"expected_version"`
	ObservedVersion      string `json:"observed_version,omitempty"`
	ElectronVersion      string `json:"electron_version,omitempty"`
	Succeeded            bool   `json:"succeeded"`
	TranscriptCharacters int    `json:"transcript_characters,omitempty"`
	FullUI               string `json:"full_ui"`
}

// Select validated scalar facts only. Original JSON may contain a private route;
// neither that route nor its response bytes are copied to the summary or errors.
func readClientCaseResult(content []byte, client, format string) (clientCaseResult, error) {
	var decoded struct {
		Success            bool   `json:"success"`
		SDK                string `json:"sdk"`
		SDKVersion         string `json:"sdk_version"`
		Format             string `json:"response_format"`
		Fixture            string `json:"fixture_sha256"`
		Characters         int    `json:"transcript_characters"`
		Transcript         string `json:"transcript_sha256"`
		Qualification      string `json:"qualification"`
		Upstream           string `json:"upstream"`
		Version            string `json:"version"`
		Commit             string `json:"commit"`
		Electron           string `json:"electron"`
		Observation        string `json:"observation"`
		Stage              string `json:"stage"`
		FullUI             string `json:"fullUI"`
		ElectronFixture    string `json:"fixtureSHA256"`
		ElectronCharacters int    `json:"transcriptCharacters"`
		ElectronTranscript string `json:"transcriptSHA256"`
	}
	fail := func() (clientCaseResult, error) {
		return clientCaseResult{FullUI: "not_executed"}, errors.New("client result lacks verified transport/version/fixture evidence")
	}
	if json.Unmarshal(content, &decoded) != nil || !decoded.Success || decoded.Qualification != "not_established" {
		return fail()
	}
	result := clientCaseResult{FullUI: "not_executed"}
	hash, fixture, characters := decoded.Transcript, decoded.Fixture, decoded.Characters
	if client == "openai" {
		if decoded.SDK != "openai-python" || decoded.SDKVersion != "2.11.0" || decoded.Format != format {
			return fail()
		}
		result.ObservedVersion = decoded.SDKVersion
	} else if client == "openwhispr" {
		if decoded.Upstream != "OpenWhispr/openwhispr" || decoded.Version != "1.10.4" || decoded.Commit != "7a6ae826de9ec1caf65f3a6e769475dab2ae829f" || decoded.Electron != "41.10.7" || decoded.Stage != "transport-call" || decoded.Observation != "actual-electron-net-fetch-with-verbatim-upstream-upload-transport" || decoded.FullUI != "not_executed" {
			return fail()
		}
		result.ObservedVersion, result.ElectronVersion = decoded.Version, decoded.Electron
		hash, fixture, characters = decoded.ElectronTranscript, decoded.ElectronFixture, decoded.ElectronCharacters
	} else {
		return fail()
	}
	bytes, err := hex.DecodeString(hash)
	if err != nil || len(bytes) != sha256.Size || fixture != publicJFKIdentity || characters <= 0 {
		return fail()
	}
	result.TranscriptCharacters = characters
	return result, nil
}

const clientCaseOutputLimit = 1 << 20

type clientCaseOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (w *clientCaseOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > clientCaseOutputLimit-w.data.Len() {
		w.overflow = true
		w.cancel()
		return len(p), nil
	}
	_, _ = w.data.Write(p)
	return len(p), nil
}

func runClientCaseCommand(parent context.Context, root, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 130*time.Second)
	defer cancel()
	stdout, stderr := &clientCaseOutput{cancel: cancel}, &clientCaseOutput{cancel: cancel}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = root
	command.Env = append(os.Environ(), "UV_CACHE_DIR="+filepath.Join(root, ".local", "uv-cache"))
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 200 * time.Millisecond
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	err := command.Run()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("client diagnostics exceeded bounded observation")
	}
	if err != nil {
		return nil, errors.Join(errors.New("client process failed; raw diagnostics withheld"), ctx.Err())
	}
	return append([]byte(nil), stdout.data.Bytes()...), nil
}

func TestClientCaseResultRequiresActualEvidence(t *testing.T) {
	valid := map[string]any{"success": true, "sdk": "openai-python", "sdk_version": "2.11.0", "response_format": "json", "fixture_sha256": publicJFKIdentity, "transcript_characters": 25, "transcript_sha256": strings.Repeat("a", 64), "qualification": "not_established", "route": "http://PRIVATE_ENDPOINT_MARKER", "text": "PRIVATE_TRANSCRIPT_MARKER"}
	content, _ := json.Marshal(valid)
	result, err := readClientCaseResult(content, "openai", "json")
	if err != nil || result.ObservedVersion != "2.11.0" {
		t.Fatal("actual client metadata was not retained")
	}
	safe, _ := json.Marshal(result)
	if strings.Contains(string(safe), "PRIVATE_") || strings.Contains(string(safe), "route") {
		t.Fatal("private upstream fields escaped sanitized client result")
	}
	for _, field := range []string{"sdk_version", "fixture_sha256", "transcript_sha256", "response_format", "qualification"} {
		t.Run(field, func(t *testing.T) {
			modified := make(map[string]any)
			for key, value := range valid {
				modified[key] = value
			}
			modified[field] = "wrong"
			content, _ := json.Marshal(modified)
			if _, err := readClientCaseResult(content, "openai", "json"); err == nil {
				t.Fatal("incomplete/incorrect client evidence accepted")
			}
		})
	}
	if _, err := readClientCaseResult([]byte(`{"success":true,"networkRequests":0}`), "openwhispr", "json"); err == nil {
		t.Fatal("prepared-only client descriptor accepted as actual transport")
	}
}

func TestClientCaseElectronRequiresTransportNotDescriptor(t *testing.T) {
	valid := map[string]any{"success": true, "upstream": "OpenWhispr/openwhispr", "version": "1.10.4", "commit": "7a6ae826de9ec1caf65f3a6e769475dab2ae829f", "electron": "41.10.7", "stage": "transport-call", "observation": "actual-electron-net-fetch-with-verbatim-upstream-upload-transport", "fullUI": "not_executed", "qualification": "not_established", "fixtureSHA256": publicJFKIdentity, "transcriptCharacters": 25, "transcriptSHA256": strings.Repeat("b", 64)}
	content, _ := json.Marshal(valid)
	if result, err := readClientCaseResult(content, "openwhispr", "json"); err != nil || result.ObservedVersion != "1.10.4" || result.ElectronVersion != "41.10.7" {
		t.Fatal("actual Electron transport facts were not recognized")
	}
	for _, field := range []string{"version", "commit", "electron", "stage", "observation", "fullUI", "fixtureSHA256"} {
		t.Run(field, func(t *testing.T) {
			modified := make(map[string]any)
			for key, value := range valid {
				modified[key] = value
			}
			modified[field] = "not-the-required-evidence"
			content, _ := json.Marshal(modified)
			if _, err := readClientCaseResult(content, "openwhispr", "json"); err == nil {
				t.Fatal("prepared-only or wrong Electron evidence accepted")
			}
		})
	}
}

func TestClientCaseCommandBoundsAndPrivacy(t *testing.T) {
	if _, err := runClientCaseCommand(t.Context(), t.TempDir(), "sh", "-c", "printf PRIVATE_CLIENT_DIAGNOSTIC >&2; exit 17"); err == nil || strings.Contains(err.Error(), "PRIVATE_CLIENT_DIAGNOSTIC") {
		t.Fatal("raw subprocess diagnostic escaped")
	}
	if _, err := runClientCaseCommand(t.Context(), t.TempDir(), "sh", "-c", "head -c 1048577 /dev/zero"); err == nil {
		t.Fatal("unbounded client stdout accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := runClientCaseCommand(ctx, t.TempDir(), "sh", "-c", "exec sleep 30"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("client deadline lost")
	}
}

// A proc entry can disappear before lookup (ENOENT) or its task can exit
// between opening and reading the entry (ESRCH). Other read failures do not
// establish termination and must remain failed observations.
func clientCaseProcessGone(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

func TestClientCaseProcObservationHandlesExitRace(t *testing.T) {
	command := exec.CommandContext(t.Context(), "sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal("cannot start owned process observation fixture")
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	stat, err := os.Open(filepath.Join("/proc", strconv.Itoa(command.Process.Pid), "stat"))
	if err != nil {
		t.Fatal("cannot open owned live process observation")
	}
	defer stat.Close()
	if err := command.Process.Kill(); err != nil {
		t.Fatal("cannot stop owned process observation fixture")
	}
	_ = command.Wait()
	waited = true
	_, err = io.ReadAll(io.LimitReader(stat, 4096))
	if err == nil {
		t.Fatal("stale process handle did not expose terminated task")
	}
	var errno syscall.Errno
	_ = errors.As(err, &errno)
	t.Logf("owned task reaped before proc read: errno=%d", errno)
	if !clientCaseProcessGone(err) {
		t.Fatal("terminated task observation was incorrectly classified as unknown")
	}
	for _, cause := range []error{syscall.EACCES, syscall.EPERM, syscall.EIO} {
		if clientCaseProcessGone(&os.PathError{Op: "read", Path: "PRIVATE_TEST_PATH", Err: cause}) {
			t.Fatal("permission or IO failure incorrectly proves process termination")
		}
	}
}

func TestClientCaseCommandStopsChildren(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "child-pid")
	t.Setenv("P07_CLIENT_CHILD_PID", path)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runClientCaseCommand(ctx, dir, "sh", "-c", `sleep 30 & child=$!; printf '%s\n' "$child" > "$P07_CLIENT_CHILD_PID"; wait "$child"`)
		done <- err
	}()
	var pid int
	deadline := time.After(3 * time.Second)
	for pid == 0 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("test child failed to start")
		case <-time.After(5 * time.Millisecond):
			data, err := os.ReadFile(path)
			if err == nil && strings.HasSuffix(string(data), "\n") {
				pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			}
		}
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("client cancellation cause lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client process group did not stop")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil && !clientCaseProcessGone(err) {
		var errno syscall.Errno
		_ = errors.As(err, &errno)
		t.Fatalf("cannot establish whether owned client descendant stopped: errno=%d", errno)
	}
	if err == nil && !strings.Contains(string(data), ") Z ") {
		t.Fatal("client descendant survived cancellation")
	}
}
