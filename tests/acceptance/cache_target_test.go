package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// The normal lifecycle recorder requires Running=true and therefore cannot own
// expected provisioning/native-init exits. Keep that shared contract unchanged.
func cacheReconfigure(ctx context.Context, e *acceptanceEnvironment, settings config.Settings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	candidate := filepath.Join(e.workdir, "cache-next.json")
	if err := os.WriteFile(candidate, data, 0600); err != nil {
		return err
	}
	if _, err := config.Load([]string{"--config", candidate}, []string{}); err != nil {
		return errors.New("actual configuration loader rejected cache scenario")
	}
	file, err := os.OpenFile(e.config, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	e.settings = settings
	if err := e.target.Recreate(ctx); err != nil {
		return err
	}
	if err := refreshLifecycleClient(ctx, e); err != nil {
		return err
	}
	if e.selection.Scope == publishedAcceptanceScope {
		return recordPublishedStartupBinding(ctx, e)
	}
	return nil
}

func TestCacheReconfigureRejectsInvalidInputBeforeReplacingBoundConfiguration(t *testing.T) {
	target, dir := dockerFixture(t)
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := os.ReadFile(target.opts.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	settings.Models.Served = "not-in-the-catalog"
	e := &acceptanceEnvironment{target: target, workdir: dir, config: target.opts.ConfigFile}
	if err := cacheReconfigure(t.Context(), e, settings); err == nil {
		t.Fatal("invalid cache scenario was accepted")
	}
	after, err := os.ReadFile(target.opts.ConfigFile)
	if err != nil || string(after) != string(bound) {
		t.Fatal("invalid scenario replaced the live bound configuration before validation")
	}
	if _, err := os.Stat(filepath.Join(dir, "actions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid scenario reached the Docker process boundary")
	}
}

func cacheNoPartialArtifacts(t *testing.T, e *acceptanceEnvironment) {
	t.Helper()
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	const script = `for entry in "$1"/*.partial-*; do [ ! -e "$entry" ] || exit 1; done`
	if _, err := e.target.run(e.ctx, "failed download partial cleanup", false, "exec", containers.BackendID, "sh", "-eu", "-c", script, "p07-cache-partials", filepath.Join(e.settings.Storage.CacheDir, "artifacts")); err != nil {
		t.Fatal("failed transfer left partial model bytes after corrected retry")
	}
}

func cacheRuntimeSourceTrust(t *testing.T, e *acceptanceEnvironment) string {
	t.Helper()
	state, err := e.target.Inspect(e.ctx, "backend")
	if err != nil {
		t.Fatal("cannot inspect source fixture prepared mount")
	}
	mounted := false
	for _, mount := range state.Mounts {
		if mount.Destination == "/prepared" && mount.Type == "bind" && mount.Source == e.prepared && !mount.RW {
			mounted = true
		}
	}
	if !mounted {
		t.Fatal("source fixture CA is not in the existing read-only prepared bind")
	}
	data, err := e.target.run(e.ctx, "effective scoped model-source trust", false, "exec", state.ID, "printenv", "SSL_CERT_FILE")
	if err != nil || strings.TrimSpace(string(data)) != "/prepared/p07-source-ca.pem" {
		t.Fatal("effective model-source trust differs from the explicit runtime fixture")
	}
	ca, err := os.ReadFile(filepath.Join(e.prepared, "p07-source-ca.pem"))
	if err != nil {
		t.Fatal("source fixture public CA is unavailable")
	}
	sum := strings.TrimPrefix(acceptanceSHA256(ca), "sha256:")
	data, err = e.target.run(e.ctx, "mounted public source CA checksum", false, "exec", state.ID, "sha256sum", "/prepared/p07-source-ca.pem")
	fields := strings.Fields(string(data))
	if err != nil || len(fields) != 2 || fields[0] != sum {
		t.Fatal("mounted source fixture CA bytes differ")
	}
	return sum
}

func cacheChangeAndRetire(t *testing.T, e *acceptanceEnvironment, settings config.Settings) {
	t.Helper()
	var before *DockerSnapshot
	var start string
	var finish func() lifecycleTrace
	// Failed startup has no resident generation to retire. A running ready native
	// generation must always be observed, reaped and released before proceeding.
	containers, err := e.target.Containers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	health, healthErr := e.target.privateHealth(e.ctx, containers.APIID)
	state, err := e.target.Inspect(e.ctx, "backend")
	if err != nil {
		t.Fatal("cannot observe old cache-model backend ownership")
	}
	if state.Running && (healthErr != nil || health == nil || health.Status != "ready") {
		t.Fatal("running cache-model generation is not independently ready for controlled replacement")
	}
	if state.Running {
		snapshot := readyLifecycleSnapshot(t, e)
		before = &snapshot
		start, err = lifecyclePIDStart(*snapshot.NativeHostPID)
		if err != nil {
			t.Fatal("cannot identify the old cache-model native generation")
		}
		finish = watchLifecycleReplacement(t, e, snapshot)
	}
	began := time.Now()
	if err := cacheReconfigure(e.ctx, e, settings); err != nil {
		t.Fatal(err)
	}
	if before != nil {
		lifecycleOldProcessGone(t, e, *before.NativeHostPID, start)
		samples := lifecycleOldGPUAllocationGone(t, e, *before.NativeHostPID, start)
		trace := finish()
		lifecycleTracePassed(t, trace, false)
		lifecycleReport(t, e, "cache-model-retirement-"+strconv.FormatInt(time.Now().UnixNano(), 10), map[string]any{"elapsed_seconds": time.Since(began).Seconds(), "gpu_release_samples": samples}, &trace)
	}
}
