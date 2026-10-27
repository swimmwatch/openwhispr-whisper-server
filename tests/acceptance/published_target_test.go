package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"strings"
	"testing"
)

func TestPublishedTargetRejectsUnselectedRunningImages(t *testing.T) {
	target, _ := dockerFixture(t)
	references := map[string]string{"api": target.opts.APIImage, "backend": target.opts.BackendImage}
	imageIDs := map[string]string{"api": "sha256:" + strings.Repeat("3", 64), "backend": "sha256:" + strings.Repeat("4", 64)}
	good := map[string]DockerInspection{}
	for role, id := range map[string]string{"api": dockerAPIID, "backend": dockerBackendID} {
		state := DockerInspection{ID: id, Image: imageIDs[role], Reference: references[role], Running: true, PID: 1234, ReadonlyRootFS: true, User: "10001:10001"}
		if role == "backend" {
			state.DeviceRequests = []DockerDeviceRequest{{Driver: "nvidia", DeviceIDs: []string{"0"}, Capabilities: [][]string{{"gpu"}}}}
		}
		good[role] = state
	}
	if err := target.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := target.Down(context.Background()); err != nil {
			t.Error(err)
		}
	})
	for _, mode := range []string{"selected", "local-reference", "wrong-registry", "wrong-engine-image"} {
		t.Run(mode, func(t *testing.T) {
			states := map[string]DockerInspection{"api": good["api"], "backend": good["backend"]}
			api := states["api"]
			switch mode {
			case "local-reference":
				api.Reference = "whisper-p07-local-api@sha256:" + strings.Repeat("1", 64)
			case "wrong-registry":
				api.Reference = strings.Replace(api.Reference, "ghcr.io/", "docker.io/", 1)
			case "wrong-engine-image":
				api.Image = "sha256:" + strings.Repeat("9", 64)
			}
			states["api"] = api
			data, _ := json.Marshal(states)
			t.Setenv("P07_DOCKER_HELPER_INSPECTIONS", string(data))
			observed := map[string]DockerInspection{}
			for _, role := range []string{"api", "backend"} {
				state, err := target.Inspect(context.Background(), role)
				if err != nil {
					t.Fatal(err)
				}
				observed[role] = state
			}
			err := verifyPublishedContainers(observed, references, imageIDs)
			if mode == "selected" && err != nil {
				t.Fatal("selected immutable pair rejected", err)
			}
			if mode != "selected" && err == nil {
				t.Fatal("unselected running artifact accepted as candidate evidence")
			}
		})
	}
}

func publishedSelectionFixture(t *testing.T, registry string) (string, publishedInventory) {
	t.Helper()
	dir := t.TempDir()
	inventory := publishedInventory{CandidateVersion: "0.1.0-rc.3", CandidateSourceSHA: strings.Repeat("a", 40), BuildInputIdentity: "sha256:" + strings.Repeat("b", 64), RuntimeInputIdentity: "sha256:" + strings.Repeat("c", 64), Images: map[string]string{"api": "sha256:" + strings.Repeat("1", 64), "backend": "sha256:" + strings.Repeat("2", 64)}}
	inventoryPath := filepath.Join(dir, "channel-state.json")
	writeAcceptanceJSON(t, inventoryPath, inventory)
	selectionPath := filepath.Join(dir, "selection.json")
	writeAcceptanceJSON(t, selectionPath, publishedInputs{ReleaseInventory: inventoryPath, Registry: registry, BaseFile: filepath.Join(dir, "base.bin"), FixtureDirectory: filepath.Join(dir, "fixtures")})
	return selectionPath, inventory
}

func writeAcceptanceJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedSelectionConsumesR01InventoryNotLocalReceipt(t *testing.T) {
	for _, registry := range []string{"ghcr.io", "docker.io"} {
		t.Run(registry, func(t *testing.T) {
			path, inventory := publishedSelectionFixture(t, registry)
			selected, err := loadPublishedSelection(path)
			if err != nil || selected.Scope != publishedAcceptanceScope || selected.Published == nil || selected.Published.CandidateSourceSHA != inventory.CandidateSourceSHA || selected.RuntimeInputIdentity != inventory.RuntimeInputIdentity || !strings.HasPrefix(selected.APIImage, registry+"/") || selected.InventorySHA256 == "" || selected.SelectionSHA256 == "" {
				t.Fatal("selected inventory identity was not bound", err)
			}
			opts := DockerOptions{Root: filepath.Dir(path), Project: "p07-published", ConfigFile: filepath.Join(filepath.Dir(path), "config.json"), PreparedDir: filepath.Dir(path), APIImage: selected.APIImage, BackendImage: selected.BackendImage, GPUDevice: "0"}
			if _, err := newSelectedDockerTarget(selected, opts); err != nil {
				t.Fatal(err)
			}
			opts.APIImage = "whisper-p07-local-api@" + inventory.Images["api"]
			if _, err := newSelectedDockerTarget(selected, opts); err == nil {
				t.Fatal("local fixture image entered published mode")
			}
			selected.Scope = localAcceptanceScope
			if _, err := newSelectedDockerTarget(selected, opts); err == nil {
				t.Fatal("published inventory entered the local receipt mode")
			}
		})
	}
	t.Run("local-receipt", func(t *testing.T) {
		path, _ := publishedSelectionFixture(t, "ghcr.io")
		writeAcceptanceJSON(t, path, localBuildReceipt{Scope: localAcceptanceScope, RuntimeInputIdentity: "sha256:" + strings.Repeat("c", 64), APIImage: "whisper-p07-local-api@sha256:" + strings.Repeat("1", 64)})
		if _, err := loadPublishedSelection(path); err == nil {
			t.Fatal("local build receipt became a candidate selection")
		}
	})
	for _, mode := range []string{"wrong-namespace", "missing-source", "missing-build", "missing-runtime", "mutable-image", "wrong-inventory"} {
		t.Run(mode, func(t *testing.T) {
			path, inventory := publishedSelectionFixture(t, "ghcr.io")
			inventoryPath := filepath.Join(filepath.Dir(path), "channel-state.json")
			switch mode {
			case "wrong-namespace":
				writeAcceptanceJSON(t, path, publishedInputs{ReleaseInventory: inventoryPath, Registry: "ghcr.io/foreign", BaseFile: filepath.Join(filepath.Dir(path), "base.bin"), FixtureDirectory: filepath.Dir(path)})
			case "missing-source":
				inventory.CandidateSourceSHA = ""
			case "missing-build":
				inventory.BuildInputIdentity = ""
			case "missing-runtime":
				inventory.RuntimeInputIdentity = ""
			case "mutable-image":
				inventory.Images["api"] = "latest"
			case "wrong-inventory":
				writeAcceptanceJSON(t, inventoryPath, localBuildReceipt{Scope: localAcceptanceScope})
				if _, err := loadPublishedSelection(path); err == nil {
					t.Fatal("local receipt accepted as R01 inventory")
				}
				return
			}
			writeAcceptanceJSON(t, inventoryPath, inventory)
			if _, err := loadPublishedSelection(path); err == nil {
				t.Fatal("unbound or foreign candidate input accepted")
			}
		})
	}
}

func TestPublishedBindingObservesEngineAndMountedConfiguration(t *testing.T) {
	target, dir := dockerFixture(t)
	path, inventory := publishedSelectionFixture(t, "ghcr.io")
	selected, err := loadPublishedSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	expectedIDs := map[string]string{target.opts.APIImage: "sha256:" + strings.Repeat("3", 64), target.opts.BackendImage: "sha256:" + strings.Repeat("4", 64)}
	imageData, _ := json.Marshal(expectedIDs)
	t.Setenv("P07_DOCKER_HELPER_IMAGE_IDS", string(imageData))
	ids, err := selectedPublishedImageIDs(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	selected.APIID, selected.BackendID = ids["api"], ids["backend"]
	states := map[string]DockerInspection{}
	for role, id := range map[string]string{"api": dockerAPIID, "backend": dockerBackendID} {
		ref := selected.APIImage
		if role == "backend" {
			ref = selected.BackendImage
		}
		state := DockerInspection{ID: id, Image: ids[role], Reference: ref, PID: 1234, Running: true, ReadonlyRootFS: true, User: "10001:10001"}
		if role == "backend" {
			state.DeviceRequests = []DockerDeviceRequest{{Driver: "nvidia", DeviceIDs: []string{"0"}, Capabilities: [][]string{{"gpu"}}}}
		}
		states[role] = state
	}
	stateData, _ := json.Marshal(states)
	t.Setenv("P07_DOCKER_HELPER_INSPECTIONS", string(stateData))
	if err := target.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := target.Down(context.Background()); err != nil {
			t.Error(err)
		}
	})
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(target.opts.ConfigFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	e := &acceptanceEnvironment{target: target, selection: selected, settings: settings, config: target.opts.ConfigFile, workdir: dir, audio: []byte("synthetic public fixture")}
	binding, err := observePublishedBinding(t.Context(), e)
	if err != nil || binding.Candidate.BuildInputIdentity != inventory.BuildInputIdentity || binding.ConfigurationSHA256 != acceptanceSHA256(data) || binding.Containers["api"].EngineImageID != ids["api"] || binding.Qualification != "not_established" {
		t.Fatal("actual binding unavailable or mislabeled", err)
	}
	if err := recordPublishedBinding(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	// A valid but different mounted configuration is drift, not candidate evidence.
	changed := settings
	changed.Queue.WaitingCapacity++
	writeAcceptanceJSON(t, target.opts.ConfigFile, changed)
	if _, err := observePublishedBinding(t.Context(), e); err == nil {
		t.Fatal("changed bound configuration was represented by stale selected settings")
	}
	t.Setenv("P07_DOCKER_HELPER_IMAGE_IDS", `{}`)
	if _, err := selectedPublishedImageIDs(t.Context(), target); err == nil {
		t.Fatal("unavailable image inspection became a known identity")
	}
}
