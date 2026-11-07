package acceptance

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// This CPU-only subprocess lets the real Go runtime emit its own trace. It
// performs ordinary allocations; no GC tuning or explicit GC is used.
func TestDockerGCProcessHelper(t *testing.T) {
	if os.Getenv("P07_GC_PROCESS_HELPER") != "1" {
		return
	}
	fmt.Println(os.Getenv("GODEBUG"))
	live := make([][]byte, 64)
	for i := range live {
		live[i] = make([]byte, 1<<20)
		live[i][0] = byte(i)
	}
	runtime.KeepAlive(live)
	os.Exit(0)
}

func dockerSupervisorGCProcess(dir string, environment map[string]string, fail func()) {
	executable, err := os.Executable()
	if err != nil {
		fail()
		return
	}
	cmd := exec.Command(executable, "-test.run=^TestDockerGCProcessHelper$")
	// Fake the container environment at the external CLI boundary, not the host.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GODEBUG" || key == "P07_GC_PROCESS_HELPER" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Env = append(cmd.Env, "P07_GC_PROCESS_HELPER=1")
	var output, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	if cmd.Run() != nil {
		fail()
		return
	}
	if os.WriteFile(filepath.Join(dir, "supervisor-effective-godebug"), output.Bytes(), 0600) != nil || os.WriteFile(filepath.Join(dir, "supervisor-gc-log"), diagnostic.Bytes(), 0600) != nil {
		fail()
	}
}

func TestDockerTargetAPIGCTraceIsDiagnosticOnly(t *testing.T) {
	target, dir := dockerFixture(t)
	t.Setenv("P07_DOCKER_HELPER_MODE", "api-gc")
	for _, enabled := range []bool{false, true, false} {
		if err := target.setAPIGCTrace(enabled); err != nil {
			t.Fatal(err)
		}
		// An unrelated fixture toggle must not lose the API observation setting.
		if err := target.setSupervisorGCTrace(true); err != nil {
			t.Fatal(err)
		}
		if err := target.Up(t.Context()); err != nil {
			t.Fatal(err)
		}
		effective, err := os.ReadFile(filepath.Join(dir, "supervisor-effective-godebug"))
		expected := ""
		if enabled {
			expected = "gctrace=1,scavtrace=1"
		}
		if err != nil || strings.TrimSpace(string(effective)) != expected {
			t.Fatal("API numeric diagnostic selection did not reach the real Go process")
		}
		if enabled {
			diagnostic, err := os.ReadFile(filepath.Join(dir, "supervisor-gc-log"))
			if err != nil || !regexp.MustCompile(`(?m)^gc [0-9]+ @[^\n]+ [0-9]+->[0-9]+->[0-9]+ MB,`).Match(diagnostic) {
				t.Fatal("selected API observation did not produce actual natural GC statistics")
			}
		}
		if err := target.Down(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDockerTargetSupervisorGCTraceIsDiagnosticOnly(t *testing.T) {
	target, dir := dockerFixture(t)
	t.Setenv("P07_DOCKER_HELPER_MODE", "supervisor-gc")
	if err := target.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	effective, err := os.ReadFile(filepath.Join(dir, "supervisor-effective-godebug"))
	if err != nil || strings.TrimSpace(string(effective)) != "" {
		t.Fatal("ordinary target unexpectedly enabled supervisor diagnostics")
	}
	if err := target.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := target.setSupervisorGCTrace(true); err != nil {
		t.Fatal(err)
	}
	if err := target.setModelSourceTrust(true); err != nil {
		t.Fatal(err)
	}
	if err := target.setModelSourceTrust(false); err != nil {
		t.Fatal(err)
	}
	if err := target.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	effective, err = os.ReadFile(filepath.Join(dir, "supervisor-effective-godebug"))
	if err != nil || strings.TrimSpace(string(effective)) != "gctrace=1,scavtrace=1" {
		t.Fatal("explicit supervisor diagnostic profile did not reach the runtime process")
	}
	diagnostic, err := os.ReadFile(filepath.Join(dir, "supervisor-gc-log"))
	if err != nil || !regexp.MustCompile(`(?m)^gc [0-9]+ @[^\n]+ [0-9]+->[0-9]+->[0-9]+ MB,`).Match(diagnostic) {
		t.Fatal("actual runtime did not emit numeric GC statistics")
	}
	if err := target.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := target.setSupervisorGCTrace(false); err != nil {
		t.Fatal(err)
	}
	if err := target.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	effective, err = os.ReadFile(filepath.Join(dir, "supervisor-effective-godebug"))
	if err != nil || strings.TrimSpace(string(effective)) != "" {
		t.Fatal("disabled supervisor trace remained active")
	}
	if err := target.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
}
