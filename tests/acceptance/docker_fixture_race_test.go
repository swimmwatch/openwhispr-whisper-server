//go:build race

package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// The deliberate race is confined to an owned child. All writers join before
// exit, so removing post-exit sleep must not hide this fixture's race failure.
func TestDockerFixtureRaceCanary(t *testing.T) {
	if os.Getenv("P07_DOCKER_FIXTURE_RACE_CANARY") != "1" {
		return
	}
	var value int
	var writers sync.WaitGroup
	writers.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer writers.Done()
			value = i
		}()
	}
	writers.Wait()
	fmt.Fprintln(os.Stdout, value)
	os.Exit(0)
}

func TestDockerFixtureDispatchPreservesRaceDetection(t *testing.T) {
	target, _ := dockerFixture(t)
	if err := os.WriteFile(target.opts.DockerExecutable, dockerFixtureScript("TestDockerFixtureRaceCanary"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, target.opts.DockerExecutable)
	// A child-local nondefault exit code proves the wrapper preserves other
	// supported race options rather than replacing the complete GORACE value.
	cmd.Env = append(target.environment(), "P07_DOCKER_FIXTURE_RACE_CANARY=1", "GORACE=exitcode=57 atexit_sleep_ms=1500")
	cmd.WaitDelay = 200 * time.Millisecond
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	err := cmd.Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 57 || !bytes.Contains(diagnostics.Bytes(), []byte("WARNING: DATA RACE")) {
		t.Fatal("owned fixture lost race instrumentation, reporting, or the configured failure exit code")
	}
}
