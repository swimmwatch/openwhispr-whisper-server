package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DockerOptions selects one explicitly owned Compose project. The constructor
// distinguishes published candidate references from named local build references;
// the caller owns the matching inventory or local receipt. This adapter does not
// build, publish or qualify artifacts. HostPort zero requests an ephemeral port.
type DockerOptions struct {
	Root, Project, ConfigFile, PreparedDir              string
	APIImage, BackendImage, GPUDevice, DockerExecutable string
	HostPort, APIPort, BackendPort, StopTimeoutSeconds  int
	CommandTimeout                                      time.Duration
}

// DockerTarget serializes lifecycle operations for one project. Down preserves
// all named volumes and caller-owned configuration/prepared directories.
type DockerTarget struct {
	opts  DockerOptions
	mu    sync.Mutex
	owned bool
	// Only fixed P07 TLS and numeric Go-process GC observation fixtures may add
	// runtime environment. No arbitrary Compose/image/mount override exists.
	modelSourceTrust  bool
	supervisorGCTrace bool
	apiGCTrace        bool
}
type DockerContainers struct{ APIID, BackendID string }
type DockerMount struct {
	Type, Name, Source, Destination string
	RW                              bool
}
type DockerDeviceRequest struct {
	Driver       string
	Count        int
	DeviceIDs    []string
	Capabilities [][]string
}
type DockerPortBinding struct{ HostIP, HostPort string }

// DockerInspection.Image is the engine-reported image identity; depending on its
// image store it need not be an OCI config digest. Reference is Config.Image.
type DockerInspection struct {
	ID, Image, Reference, User string
	PID                        int
	Running, ReadonlyRootFS    bool
	StartedAt                  time.Time
	Mounts                     []DockerMount
	DeviceRequests             []DockerDeviceRequest
	PortBindings               map[string][]DockerPortBinding
}

// Command is the executable name (comm), never argv or a shell command line.
type DockerProcess struct {
	PID, PPID int
	Command   string
}

// NewLocalDockerTarget selects named local RepoDigests for pre-release testing
// only. The parent must verify their actual engine identities/build receipts;
// this constructor never qualifies them as published candidate artifacts.
func NewLocalDockerTarget(opts DockerOptions) (*DockerTarget, error) {
	return newDockerTarget(opts, true)
}

func NewDockerTarget(opts DockerOptions) (*DockerTarget, error) {
	return newDockerTarget(opts, false)
}

func newDockerTarget(opts DockerOptions, local bool) (*DockerTarget, error) {
	if !filepath.IsAbs(opts.Root) || !filepath.IsAbs(opts.ConfigFile) || !filepath.IsAbs(opts.PreparedDir) || !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`).MatchString(opts.Project) || opts.GPUDevice == "" || strings.ContainsAny(opts.GPUDevice, "\r\n\x00") {
		return nil, errors.New("explicit absolute Docker target paths, project and GPU selection are required")
	}
	for role, ref := range map[string]string{"api": opts.APIImage, "backend": opts.BackendImage} {
		pattern := `^(ghcr\.io|docker\.io)/swimmwatch/openwhispr-whisper-server-` + role + `@sha256:[a-f0-9]{64}$`
		if local {
			pattern = `^whisper-p07-local-` + role + `@sha256:[a-f0-9]{64}$`
		}
		if !regexp.MustCompile(pattern).MatchString(ref) {
			return nil, errors.New("explicit immutable role-matched image references are required")
		}
	}
	if opts.APIPort == 0 {
		opts.APIPort = 8080
	}
	if opts.BackendPort == 0 {
		opts.BackendPort = 8081
	}
	if opts.HostPort < 0 || opts.HostPort > 65535 || opts.APIPort < 1 || opts.APIPort > 65535 || opts.BackendPort < 1 || opts.BackendPort > 65535 {
		return nil, errors.New("invalid Docker target ports")
	}
	if opts.CommandTimeout == 0 {
		opts.CommandTimeout = 2 * time.Minute
	}
	if opts.StopTimeoutSeconds == 0 {
		opts.StopTimeoutSeconds = 45
	}
	if opts.CommandTimeout < 0 || opts.StopTimeoutSeconds < 1 {
		return nil, errors.New("positive Docker operation budgets are required")
	}
	if opts.DockerExecutable == "" {
		opts.DockerExecutable = "docker"
	}
	opts.Root = filepath.Clean(opts.Root)
	return &DockerTarget{opts: opts}, nil
}

func (t *DockerTarget) environment() []string {
	env := make([]string, 0, len(os.Environ())+12)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "WHISPER_") || strings.HasPrefix(key, "COMPOSE_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"WHISPER_API_IMAGE="+t.opts.APIImage, "WHISPER_BACKEND_IMAGE="+t.opts.BackendImage,
		"WHISPER_CONFIG_FILE="+t.opts.ConfigFile, "WHISPER_PREPARED_DIR="+t.opts.PreparedDir,
		"WHISPER_GPU_DEVICE_ID="+t.opts.GPUDevice, "WHISPER_BIND_ADDRESS=127.0.0.1",
		"WHISPER_API_PORT="+strconv.Itoa(t.opts.HostPort), "WHISPER_API_CONTAINER_PORT="+strconv.Itoa(t.opts.APIPort),
		"WHISPER_BACKEND_CONTAINER_PORT="+strconv.Itoa(t.opts.BackendPort), "WHISPER_STOP_GRACE="+strconv.Itoa(t.opts.StopTimeoutSeconds)+"s",
		"COMPOSE_DISABLE_ENV_FILE=true")
}

const dockerOutputLimit = 1 << 20

type dockerOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	size     int
	overflow bool
	cancel   context.CancelFunc
	retain   bool
}

func (w *dockerOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size += len(p)
	if w.size > dockerOutputLimit {
		w.overflow = true
		w.cancel()
		return len(p), nil
	}
	if w.retain {
		_, _ = w.data.Write(p)
	}
	return len(p), nil
}

func (t *DockerTarget) run(parent context.Context, operation string, combined bool, args ...string) ([]byte, error) {
	return t.runWithTimeout(parent, t.opts.CommandTimeout, operation, combined, args...)
}

// runWithTimeout lets an attached model observer own its declared phase budget.
// Ordinary CLI calls retain CommandTimeout; an earlier parent deadline still wins.
func (t *DockerTarget) runWithTimeout(parent context.Context, budget time.Duration, operation string, combined bool, args ...string) ([]byte, error) {
	if budget <= 0 {
		return nil, errors.New("positive Docker observation budget is required")
	}
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.opts.DockerExecutable, args...)
	cmd.Dir = t.opts.Root
	cmd.Env = t.environment()
	output := &dockerOutput{cancel: cancel, retain: true}
	diagnostic := &dockerOutput{cancel: cancel}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	if combined {
		cmd.Stderr = output
	}
	err := runOwnedCommand(cmd)
	if output.overflow || diagnostic.overflow {
		return nil, errors.New("Docker " + operation + " output exceeded its observation limit")
	}
	if err != nil {
		exitCode := -1
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			exitCode = exited.ExitCode()
		}
		return nil, errors.Join(errors.New("Docker "+operation+" failed (exit "+strconv.Itoa(exitCode)+"); raw diagnostics suppressed"), ctx.Err())
	}
	return output.data.Bytes(), nil
}

// setModelSourceTrust uses only the existing read-only prepared mount. The
// generated private runtime input changes neither image nor application settings.
func (t *DockerTarget) setModelSourceTrust(enabled bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.prepareRuntimeFixture(enabled, t.supervisorGCTrace, t.apiGCTrace); err != nil {
		return err
	}
	t.modelSourceTrust = enabled
	return nil
}

// setSupervisorGCTrace enables numeric diagnostics, never GC tuning. The flag
// alone establishes no qualification; physical RSS/resource ownership checks apply.
func (t *DockerTarget) setSupervisorGCTrace(enabled bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.prepareRuntimeFixture(t.modelSourceTrust, enabled, t.apiGCTrace); err != nil {
		return err
	}
	t.supervisorGCTrace = enabled
	return nil
}

// setAPIGCTrace is supplemental evidence only; it cannot relax API RSS bounds.
func (t *DockerTarget) setAPIGCTrace(enabled bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.prepareRuntimeFixture(t.modelSourceTrust, t.supervisorGCTrace, enabled); err != nil {
		return err
	}
	t.apiGCTrace = enabled
	return nil
}

// Fixed fixtures share one environment owner, preserving each other's settings.
// No caller-supplied key, value, path or override is accepted.
func (t *DockerTarget) prepareRuntimeFixture(sourceTrust, gcTrace, apiTrace bool) error {
	if !sourceTrust && !gcTrace && !apiTrace {
		return nil
	}
	environment := map[string]string{}
	if sourceTrust {
		environment["SSL_CERT_FILE"] = "/prepared/p07-source-ca.pem"
	}
	if gcTrace {
		environment["GODEBUG"] = "gctrace=1,scavtrace=1"
	}
	services := map[string]any{"backend": map[string]any{"environment": environment}}
	if apiTrace {
		services["api"] = map[string]any{"environment": map[string]string{"GODEBUG": "gctrace=1,scavtrace=1"}}
	}
	fixture, err := json.Marshal(map[string]any{"services": services})
	if err != nil {
		return errors.New("cannot encode the private backend runtime fixture")
	}
	file := filepath.Join(filepath.Dir(t.opts.ConfigFile), "p07-runtime-fixtures.json")
	if os.WriteFile(file, fixture, 0600) != nil || os.Chmod(file, 0600) != nil {
		return errors.New("cannot protect the private backend runtime fixture")
	}
	return nil
}

func (t *DockerTarget) compose(ctx context.Context, operation string, args ...string) ([]byte, error) {
	base := []string{"compose", "--project-name", t.opts.Project, "--project-directory", t.opts.Root, "--file", filepath.Join(t.opts.Root, "deploy", "compose.yaml"), "--env-file", "/dev/null"}
	if t.modelSourceTrust || t.supervisorGCTrace || t.apiGCTrace {
		base = append(base, "--file", filepath.Join(filepath.Dir(t.opts.ConfigFile), "p07-runtime-fixtures.json"))
	}
	return t.run(ctx, operation, false, append(base, args...)...)
}

func (t *DockerTarget) Up(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.owned {
		return errors.New("Docker target is already owned; use recreate")
	}
	existing, err := t.compose(ctx, "project ownership", "ps", "--all", "--quiet")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(existing)) != 0 {
		return errors.New("Docker project already contains containers; refusing adoption")
	}
	// Claim cleanup responsibility before Up: Docker may leave a partial project.
	t.owned = true
	if _, err := t.compose(ctx, "up", "up", "--detach", "--no-build", "--pull", "never"); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), t.opts.CommandTimeout)
		defer cancel()
		return errors.Join(err, t.down(cleanup))
	}
	return nil
}
func (t *DockerTarget) down(ctx context.Context) error {
	if !t.owned {
		return nil
	}
	if _, err := t.compose(ctx, "down", "down", "--timeout", strconv.Itoa(t.opts.StopTimeoutSeconds)); err != nil {
		return err
	}
	t.owned = false
	return nil
}
func (t *DockerTarget) Down(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.down(ctx)
}
func (t *DockerTarget) Recreate(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.owned {
		return errors.New("Docker target is not owned")
	}
	_, err := t.compose(ctx, "recreate", "up", "--detach", "--no-build", "--pull", "never", "--force-recreate")
	return err
}
func dockerService(service string) error {
	if service != "api" && service != "backend" {
		return errors.New("unknown Docker target service")
	}
	return nil
}
func (t *DockerTarget) Restart(ctx context.Context, service string) error {
	if err := dockerService(service); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.owned {
		return errors.New("Docker target is not owned")
	}
	_, err := t.compose(ctx, "restart", "restart", "--timeout", strconv.Itoa(t.opts.StopTimeoutSeconds), service)
	return err
}
func (t *DockerTarget) container(ctx context.Context, service string) (string, error) {
	if err := dockerService(service); err != nil {
		return "", err
	}
	if !t.owned {
		return "", errors.New("Docker target is not owned")
	}
	data, err := t.compose(ctx, "container identity", "ps", "--all", "--quiet", service)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
		return "", errors.New("Docker service must resolve to one exact container")
	}
	return id, nil
}
func (t *DockerTarget) Containers(ctx context.Context) (DockerContainers, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	api, err := t.container(ctx, "api")
	if err != nil {
		return DockerContainers{}, err
	}
	backend, err := t.container(ctx, "backend")
	return DockerContainers{APIID: api, BackendID: backend}, err
}
func (t *DockerTarget) APIAddress(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.owned {
		return "", errors.New("Docker target is not owned")
	}
	data, err := t.compose(ctx, "published API address", "port", "api", strconv.Itoa(t.opts.APIPort))
	if err != nil {
		return "", err
	}
	address := strings.TrimSpace(string(data))
	host, port, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return "", errors.New("Docker API publication must be one loopback address")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("invalid Docker published API port")
	}
	return "http://" + address, nil
}
func (t *DockerTarget) Inspect(ctx context.Context, service string) (DockerInspection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, err := t.container(ctx, service)
	if err != nil {
		return DockerInspection{}, err
	}
	const format = `{"ID":{{json .Id}},"Image":{{json .Image}},"Reference":{{json .Config.Image}},"PID":{{.State.Pid}},"Running":{{.State.Running}},"StartedAt":{{json .State.StartedAt}},"User":{{json .Config.User}},"ReadonlyRootFS":{{.HostConfig.ReadonlyRootfs}},"Mounts":{{json .Mounts}},"DeviceRequests":{{json .HostConfig.DeviceRequests}},"PortBindings":{{json .HostConfig.PortBindings}}}`
	data, err := t.run(ctx, "inspection", false, "inspect", "--format", format, id)
	if err != nil {
		return DockerInspection{}, err
	}
	var state DockerInspection
	if json.Unmarshal(data, &state) != nil || state.ID != id || state.PID < 0 {
		return DockerInspection{}, errors.New("invalid Docker container observation")
	}
	return state, nil
}

// Logs returns bounded untrusted content for in-memory privacy assertions. The
// caller must not serialize these bytes into public reports or diagnostics.
func (t *DockerTarget) Logs(ctx context.Context, service string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, err := t.container(ctx, service)
	if err != nil {
		return nil, err
	}
	return t.run(ctx, "log observation", true, "logs", "--timestamps", "--tail", "10000", id)
}
func (t *DockerTarget) Processes(ctx context.Context, service string) ([]DockerProcess, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.processes(ctx, service)
}

func (t *DockerTarget) processes(ctx context.Context, service string) ([]DockerProcess, error) {
	id, err := t.container(ctx, service)
	if err != nil {
		return nil, err
	}
	data, err := t.run(ctx, "process observation", false, "top", id, "-eo", "pid,ppid,comm")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	header := strings.Fields(lines[0])
	if len(header) != 3 || header[0] != "PID" || header[1] != "PPID" || header[2] != "COMMAND" {
		return nil, errors.New("invalid Docker process observation")
	}
	result := make([]DockerProcess, 0, len(lines)-1)
	for _, line := range lines[1:] {
		row := strings.Fields(line)
		if len(row) != 3 {
			return nil, errors.New("invalid Docker process columns")
		}
		pid, e1 := strconv.Atoi(row[0])
		ppid, e2 := strconv.Atoi(row[1])
		if e1 != nil || e2 != nil || pid < 1 || ppid < 0 {
			return nil, errors.New("invalid Docker process identity")
		}
		result = append(result, DockerProcess{PID: pid, PPID: ppid, Command: row[2]})
	}
	return result, nil
}
