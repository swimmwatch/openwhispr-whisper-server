package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const dockerAPIID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const dockerBackendID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

const dockerFixtureCommandTimeout = 3 * time.Second

func dockerFixture(t *testing.T) (*DockerTarget, string) {
	t.Helper()
	return dockerFixtureWithBudget(t, dockerFixtureCommandTimeout)
}

func dockerFixtureWithBudget(t *testing.T, budget time.Duration) (*DockerTarget, string) {
	t.Helper()
	return dockerFixtureWithImages(t, budget, false)
}

func dockerFixtureWithImages(t *testing.T, budget time.Duration, local bool) (*DockerTarget, string) {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "docker")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec \"$P07_DOCKER_HELPER_EXEC\" -test.run=^TestDockerCLIHelper$ -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P07_DOCKER_HELPER_EXEC", executable)
	t.Setenv("P07_DOCKER_HELPER_DIR", dir)
	t.Setenv("P07_DOCKER_HELPER_ROOT", dir)
	t.Setenv("WHISPER_API_IMAGE", "untrusted:latest")
	t.Setenv("COMPOSE_FILE", "unrelated.yaml")
	t.Setenv("COMPOSE_PROFILES", "unrelated")
	config := filepath.Join(dir, "config.json")
	prepared := filepath.Join(dir, "prepared")
	if err := os.Mkdir(prepared, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared, "model.bin"), []byte("operator-model"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache-model.bin"), []byte("cached-model"), 0600); err != nil {
		t.Fatal(err)
	}
	apiImage := "ghcr.io/swimmwatch/openwhispr-whisper-server-api@sha256:" + strings.Repeat("1", 64)
	backendImage := "ghcr.io/swimmwatch/openwhispr-whisper-server-backend@sha256:" + strings.Repeat("2", 64)
	constructor := NewDockerTarget
	if local {
		apiImage = "whisper-p07-local-api@sha256:" + strings.Repeat("1", 64)
		backendImage = "whisper-p07-local-backend@sha256:" + strings.Repeat("2", 64)
		constructor = NewLocalDockerTarget
	}
	t.Setenv("P07_DOCKER_HELPER_EXPECT_API", apiImage)
	t.Setenv("P07_DOCKER_HELPER_EXPECT_BACKEND", backendImage)
	target, err := constructor(DockerOptions{Root: dir, Project: "p07-owned", ConfigFile: config, PreparedDir: prepared, APIImage: apiImage, BackendImage: backendImage, GPUDevice: "0", DockerExecutable: wrapper, CommandTimeout: budget})
	if err != nil {
		t.Fatal(err)
	}
	return target, dir
}

// The helper is a fake external Docker executable, not a Compose parser or GPU substitute.
func TestDockerCLIHelper(t *testing.T) {
	dir := os.Getenv("P07_DOCKER_HELPER_DIR")
	if dir == "" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	fail := func() { fmt.Fprintln(os.Stderr, "PRIVATE_RUNTIME_DIAGNOSTIC"); os.Exit(17) }
	if len(args) == 0 {
		fail()
	}
	if os.Getenv("WHISPER_API_IMAGE") != os.Getenv("P07_DOCKER_HELPER_EXPECT_API") || os.Getenv("WHISPER_BACKEND_IMAGE") != os.Getenv("P07_DOCKER_HELPER_EXPECT_BACKEND") {
		fail()
	}
	state := filepath.Join(dir, "running")
	if args[0] == "compose" {
		expected := map[string]string{"--project-name": "p07-owned", "--project-directory": os.Getenv("P07_DOCKER_HELPER_ROOT"), "--file": filepath.Join(os.Getenv("P07_DOCKER_HELPER_ROOT"), "deploy", "compose.yaml"), "--env-file": "/dev/null"}
		args = args[1:]
		for len(args) >= 2 && strings.HasPrefix(args[0], "--") {
			value, ok := expected[args[0]]
			if !ok || value != args[1] {
				fail()
			}
			delete(expected, args[0])
			args = args[2:]
		}
		if len(expected) != 0 || len(args) == 0 || os.Getenv("COMPOSE_FILE") != "" || os.Getenv("COMPOSE_PROFILES") != "" || !strings.Contains(os.Getenv("WHISPER_API_IMAGE"), "@sha256:") || os.Getenv("WHISPER_CONFIG_FILE") != filepath.Join(dir, "config.json") || os.Getenv("WHISPER_PREPARED_DIR") != filepath.Join(dir, "prepared") {
			fail()
		}
		action := args[0]
		f, err := os.OpenFile(filepath.Join(dir, "actions"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fail()
		}
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
		switch action {
		case "ps":
			if os.Getenv("P07_DOCKER_HELPER_MODE") == "deadline-ps" {
				time.Sleep(30 * time.Second)
			}
			if _, err := os.Stat(state); err != nil {
				os.Exit(0)
			}
			service := args[len(args)-1]
			if service == "api" {
				fmt.Println(dockerAPIID)
			} else if service == "backend" {
				fmt.Println(dockerBackendID)
			} else {
				fmt.Println(dockerAPIID)
				fmt.Println(dockerBackendID)
			}
		case "up":
			if err := os.WriteFile(state, []byte("owned"), 0600); err != nil {
				fail()
			}
			switch os.Getenv("P07_DOCKER_HELPER_MODE") {
			case "partial":
				fail()
			case "blocked":
				child := exec.Command("sleep", "30")
				child.Stdout = os.Stdout
				child.Stderr = os.Stderr
				if child.Start() != nil {
					fail()
				}
				if address := os.Getenv("P07_DOCKER_HELPER_READY"); address != "" {
					ready, err := net.DialTimeout("unix", address, time.Second)
					if err != nil {
						fail()
					}
					if _, err := fmt.Fprintln(ready, child.Process.Pid); err != nil {
						ready.Close()
						fail()
					}
					ready.Close()
				}
				child.Wait()
			case "overflow":
				fmt.Print(strings.Repeat("PRIVATE_RUNTIME_DIAGNOSTIC", 100000))
			}
		case "down":
			if os.Getenv("P07_DOCKER_HELPER_DOWN_FAIL") == "1" {
				fail()
			}
			for _, arg := range args {
				if arg == "--volumes" || arg == "-v" {
					os.Remove(filepath.Join(dir, "cache-model.bin"))
				}
			}
			os.Remove(state)
		case "restart":
			if _, err := os.Stat(state); err != nil {
				fail()
			}
		case "port":
			if os.Getenv("P07_DOCKER_HELPER_PORT") != "" {
				fmt.Println(os.Getenv("P07_DOCKER_HELPER_PORT"))
			} else {
				fmt.Println("127.0.0.1:49152")
			}
		default:
			fail()
		}
	} else {
		switch args[0] {
		case "image":
			var ids map[string]string
			if json.Unmarshal([]byte(os.Getenv("P07_DOCKER_HELPER_IMAGE_IDS")), &ids) != nil || ids[args[len(args)-1]] == "" {
				fail()
			}
			fmt.Println(ids[args[len(args)-1]])
		case "inspect":
			if input := os.Getenv("P07_DOCKER_HELPER_INSPECTIONS"); input != "" {
				var states map[string]DockerInspection
				if json.Unmarshal([]byte(input), &states) != nil {
					fail()
				}
				for _, state := range states {
					if state.ID == args[len(args)-1] {
						json.NewEncoder(os.Stdout).Encode(state)
						os.Exit(0)
					}
				}
				fail()
			}
			json.NewEncoder(os.Stdout).Encode(DockerInspection{ID: dockerBackendID, Image: "sha256:" + strings.Repeat("3", 64), Reference: "backend@sha256:" + strings.Repeat("2", 64), PID: 1234, Running: true, StartedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), User: "10001:10001", ReadonlyRootFS: true, Mounts: []DockerMount{{Type: "volume", Name: "p07-owned_model-cache", Destination: "/models", RW: true}}, DeviceRequests: []DockerDeviceRequest{{Driver: "nvidia", DeviceIDs: []string{"0"}, Capabilities: [][]string{{"gpu"}}}}})
		case "logs":
			fmt.Fprintln(os.Stderr, "private-log-marker")
		case "top":
			if os.Getenv("P07_DOCKER_HELPER_OBSERVE") == "missing-native" {
				fmt.Println("PID PPID COMMAND\n111 1 whisper-backend")
			} else {
				fmt.Println("PID PPID COMMAND\n1234 1 whisper-server")
			}
		case "exec":
			dockerObservationCLI(args, fail)
		default:
			fail()
		}
	}
	os.Exit(0)
}

func TestDockerTargetLifecycleRetainsPreparedData(t *testing.T) {
	target, dir := dockerFixture(t)
	ctx := context.Background()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	ids, err := target.Containers(ctx)
	if err != nil || ids.APIID != dockerAPIID || ids.BackendID != dockerBackendID {
		t.Fatalf("container identities unavailable: %+v %v", ids, err)
	}
	address, err := target.APIAddress(ctx)
	if err != nil || address != "http://127.0.0.1:49152" {
		t.Fatalf("published API address: %q %v", address, err)
	}
	if err := target.Restart(ctx, "api"); err != nil {
		t.Fatal(err)
	}
	if err := target.Recreate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := target.Down(ctx); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "prepared", "model.bin"))
	if err != nil || string(contents) != "operator-model" {
		t.Fatal("normal teardown modified prepared model")
	}
	cache, err := os.ReadFile(filepath.Join(dir, "cache-model.bin"))
	if err != nil || string(cache) != "cached-model" {
		t.Fatal("normal teardown deleted cached model volume")
	}
	actions, err := os.ReadFile(filepath.Join(dir, "actions"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(actions), "--force-recreate") || !strings.Contains(string(actions), "restart") || !strings.Contains(string(actions), "down") {
		t.Fatal("requested lifecycle transitions were not executed")
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned containers not removed")
	}
}

func TestDockerTargetFailedUpCleansPartialResources(t *testing.T) {
	target, dir := dockerFixture(t)
	t.Setenv("P07_DOCKER_HELPER_MODE", "partial")
	err := target.Up(context.Background())
	if err == nil || strings.Contains(err.Error(), "PRIVATE_RUNTIME_DIAGNOSTIC") || strings.Contains(err.Error(), dir) {
		t.Fatalf("failure missing or leaked private diagnostic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial Up left containers running")
	}
	if err := target.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "prepared", "model.bin"))
	if err != nil || string(data) != "operator-model" {
		t.Fatal("failed Up cleanup removed prepared data")
	}
}

func TestDockerTargetRefusesExistingProjectAndUnownedDown(t *testing.T) {
	target, dir := dockerFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "running"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := target.Up(context.Background()); err == nil {
		t.Fatal("existing project was adopted")
	}
	if err := target.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); err != nil {
		t.Fatal("unowned project was removed")
	}
}

func TestDockerTargetRetainsCleanupOwnershipOnDownFailure(t *testing.T) {
	target, dir := dockerFixture(t)
	t.Setenv("P07_DOCKER_HELPER_MODE", "partial")
	t.Setenv("P07_DOCKER_HELPER_DOWN_FAIL", "1")
	if err := target.Up(context.Background()); err == nil {
		t.Fatal("partial failure accepted")
	}
	t.Setenv("P07_DOCKER_HELPER_DOWN_FAIL", "")
	if err := target.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retry lost partial-resource cleanup ownership")
	}
}

func TestDockerTargetCancellationStopsCLIChildrenAndCleans(t *testing.T) {
	target, dir := dockerFixture(t)
	t.Setenv("P07_DOCKER_HELPER_MODE", "blocked")
	address := filepath.Join(dir, "r")
	ready, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	if err := ready.SetDeadline(time.Now().Add(2 * dockerFixtureCommandTimeout)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("P07_DOCKER_HELPER_READY", address)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	returned := false
	go func() { done <- target.Up(ctx) }()
	t.Cleanup(func() {
		cancel()
		if !returned {
			select {
			case <-done:
			case <-time.After(2 * dockerFixtureCommandTimeout):
				t.Error("Docker Up did not stop during test cleanup")
			}
		}
	})
	connection, err := ready.AcceptUnix()
	if err != nil {
		t.Fatal("blocked CLI child did not announce readiness", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(dockerFixtureCommandTimeout)); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	var pid int
	_, err = fmt.Fscan(io.LimitReader(connection, 64), &pid)
	connection.Close()
	if err != nil || pid < 1 {
		t.Fatal("blocked CLI child announced an invalid PID", err)
	}
	// Cancellation begins only after the fake CLI has actually started its child.
	// Initial Compose discovery and race-runtime exit delay are not cancellation time.
	cancel()
	select {
	case err = <-done:
		returned = true
	case <-time.After(2 * dockerFixtureCommandTimeout):
		t.Fatal("unbounded cancellation or independent cleanup")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
	// A terminated orphan may briefly remain a zombie until the host reaps it.
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err == nil && !strings.Contains(string(data), ") Z ") {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("CLI descendant survived cancellation")
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled Up did not execute independent cleanup")
	}
}

func TestDockerTargetCommandDeadlineBoundsDiscovery(t *testing.T) {
	target, dir := dockerFixtureWithBudget(t, 150*time.Millisecond)
	t.Setenv("P07_DOCKER_HELPER_MODE", "deadline-ps")
	started := time.Now()
	err := target.Up(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("command timeout did not bound stalled discovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "running")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("timed-out discovery reached container creation")
	}
}

func TestDockerTargetObservationAndOutputBounds(t *testing.T) {
	target, _ := dockerFixture(t)
	ctx := context.Background()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	defer target.Down(ctx)
	inspect, err := target.Inspect(ctx, "backend")
	if err != nil || inspect.PID != 1234 || !inspect.Running || len(inspect.DeviceRequests) != 1 || len(inspect.Mounts) != 1 {
		t.Fatalf("typed inspection missing: %+v %v", inspect, err)
	}
	logs, err := target.Logs(ctx, "backend")
	if err != nil || !strings.Contains(string(logs), "private-log-marker") {
		t.Fatal("container stderr was not observed in memory")
	}
	processes, err := target.Processes(ctx, "backend")
	if err != nil || len(processes) != 1 || processes[0].PID != 1234 || processes[0].Command != "whisper-server" {
		t.Fatalf("safe process observation: %+v %v", processes, err)
	}
	t.Setenv("P07_DOCKER_HELPER_PORT", "0.0.0.0:49152")
	if _, err := target.APIAddress(ctx); err == nil {
		t.Fatal("non-loopback publication accepted")
	}
	t.Setenv("P07_DOCKER_HELPER_MODE", "overflow")
	if err := target.Recreate(ctx); err == nil || strings.Contains(err.Error(), "PRIVATE_RUNTIME_DIAGNOSTIC") {
		t.Fatal("output overflow was accepted or leaked")
	}
}

func TestDockerTargetRequiresExplicitImmutableInputs(t *testing.T) {
	if _, err := NewDockerTarget(DockerOptions{}); err == nil {
		t.Fatal("implicit target accepted")
	}
	root := t.TempDir()
	base := DockerOptions{Root: root, Project: "p07-inputs", ConfigFile: filepath.Join(root, "config.json"), PreparedDir: filepath.Join(root, "prepared"), APIImage: "ghcr.io/swimmwatch/openwhispr-whisper-server-api@sha256:" + strings.Repeat("1", 64), BackendImage: "docker.io/swimmwatch/openwhispr-whisper-server-backend@sha256:" + strings.Repeat("2", 64), GPUDevice: "0"}
	for name, change := range map[string]func(*DockerOptions){
		"mutable-image":     func(o *DockerOptions) { o.APIImage = "ghcr.io/swimmwatch/openwhispr-whisper-server-api:latest" },
		"wrong-role":        func(o *DockerOptions) { o.BackendImage = o.APIImage },
		"relative-config":   func(o *DockerOptions) { o.ConfigFile = "config.json" },
		"relative-prepared": func(o *DockerOptions) { o.PreparedDir = "prepared" },
		"implicit-project":  func(o *DockerOptions) { o.Project = "" },
		"invalid-port":      func(o *DockerOptions) { o.HostPort = -1 },
		"invalid-budget":    func(o *DockerOptions) { o.CommandTimeout = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			opts := base
			change(&opts)
			if _, err := NewDockerTarget(opts); err == nil {
				t.Fatal("invalid target input accepted")
			}
		})
	}
	target, _ := dockerFixture(t)
	if err := target.Restart(context.Background(), "backend --volumes"); err == nil {
		t.Fatal("unknown service accepted")
	}
}
