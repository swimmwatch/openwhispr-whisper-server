package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

const localAcceptanceScope = "local_validation_not_release"
const publishedAcceptanceScope = "published_candidate_partial_acceptance"

// The shared environment consumes an explicit selection, never a local receipt
// relabeled as a published release. Only openLocalEnvironment reads that schema.
type acceptanceSelection struct {
	Scope, RuntimeInputIdentity              string
	APIImage, BackendImage, APIID, BackendID string
	BaseFile, TinyFile, FixtureDirectory     string
	Published                                *publishedInventory
	InventorySHA256, SelectionSHA256         string
}

type publishedInventory struct {
	CandidateVersion     string            `json:"candidate_version"`
	CandidateSourceSHA   string            `json:"candidate_source_sha"`
	BuildInputIdentity   string            `json:"build_input_identity"`
	RuntimeInputIdentity string            `json:"runtime_input_identity"`
	Images               map[string]string `json:"image_manifest_digests"`
}

// This private selection references the R01 inventory and already prepared public
// files. It deliberately has no scope, local image aliases, build receipt or CA
// derivative override. Unknown fields are rejected instead of silently ignored.
type publishedInputs struct {
	ReleaseInventory string `json:"release_inventory"`
	Registry         string `json:"registry"`
	BaseFile         string `json:"base_file"`
	TinyFile         string `json:"tiny_file,omitempty"`
	FixtureDirectory string `json:"fixture_directory"`
}

func readBoundedAcceptanceJSON(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("selected acceptance input is unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(data) == 1<<20 {
		return nil, errors.New("selected acceptance input exceeds its read bound")
	}
	return data, nil
}

func acceptanceSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validAcceptanceIdentity(value string, size int, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	text := strings.TrimPrefix(value, prefix)
	decoded, err := hex.DecodeString(text)
	return err == nil && len(decoded) == size && text == strings.ToLower(text)
}

func loadPublishedSelection(path string) (acceptanceSelection, error) {
	var selected acceptanceSelection
	data, err := readBoundedAcceptanceJSON(path)
	if err != nil {
		return selected, err
	}
	var inputs publishedInputs
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&inputs) != nil || decoder.Decode(new(any)) != io.EOF {
		return selected, errors.New("published acceptance requires its explicit selection schema, not a local receipt")
	}
	if !filepath.IsAbs(inputs.ReleaseInventory) || !filepath.IsAbs(inputs.BaseFile) || !filepath.IsAbs(inputs.FixtureDirectory) || inputs.TinyFile != "" && !filepath.IsAbs(inputs.TinyFile) {
		return selected, errors.New("published inventory and public fixture paths must be explicit absolute paths")
	}
	inventoryData, err := readBoundedAcceptanceJSON(inputs.ReleaseInventory)
	if err != nil {
		return selected, err
	}
	var inventory publishedInventory
	if json.Unmarshal(inventoryData, &inventory) != nil || inventory.CandidateVersion == "" || !validAcceptanceIdentity(inventory.CandidateSourceSHA, 20, "") || !validAcceptanceIdentity(inventory.BuildInputIdentity, 32, "sha256:") || !validAcceptanceIdentity(inventory.RuntimeInputIdentity, 32, "sha256:") || !validAcceptanceIdentity(inventory.Images["api"], 32, "sha256:") || !validAcceptanceIdentity(inventory.Images["backend"], 32, "sha256:") {
		return selected, errors.New("R01 candidate source/build/runtime/image identities are incomplete")
	}
	selected = acceptanceSelection{Scope: publishedAcceptanceScope, RuntimeInputIdentity: inventory.RuntimeInputIdentity, APIImage: inputs.Registry + "/swimmwatch/openwhispr-whisper-server-api@" + inventory.Images["api"], BackendImage: inputs.Registry + "/swimmwatch/openwhispr-whisper-server-backend@" + inventory.Images["backend"], BaseFile: inputs.BaseFile, TinyFile: inputs.TinyFile, FixtureDirectory: inputs.FixtureDirectory, Published: &inventory, InventorySHA256: acceptanceSHA256(inventoryData), SelectionSHA256: acceptanceSHA256(data)}
	// Reuse the owning target constructor's namespace/role/digest validation.
	if _, err := newSelectedDockerTarget(selected, DockerOptions{Root: filepath.Dir(path), Project: "p07-selection", ConfigFile: filepath.Join(filepath.Dir(path), "config.json"), PreparedDir: inputs.FixtureDirectory, APIImage: selected.APIImage, BackendImage: selected.BackendImage, GPUDevice: "0"}); err != nil {
		return acceptanceSelection{}, err
	}
	return selected, nil
}

func newSelectedDockerTarget(selection acceptanceSelection, opts DockerOptions) (*DockerTarget, error) {
	switch selection.Scope {
	case localAcceptanceScope:
		if selection.Published != nil {
			return nil, errors.New("published selection cannot use the local target constructor")
		}
		return NewLocalDockerTarget(opts)
	case publishedAcceptanceScope:
		if selection.Published == nil {
			return nil, errors.New("published target requires an R01 inventory binding")
		}
		if opts.APIImage != selection.APIImage || opts.BackendImage != selection.BackendImage {
			return nil, errPublishedBinding
		}
		return NewDockerTarget(opts)
	default:
		return nil, errors.New("acceptance target mode must be explicit")
	}
}

var errPublishedBinding = errors.New("published target does not match the selected immutable artifacts or execution boundaries")

func verifyPublishedContainers(states map[string]DockerInspection, references, imageIDs map[string]string) error {
	for _, role := range []string{"api", "backend"} {
		state := states[role]
		if !state.Running || !state.ReadonlyRootFS || state.User != "10001:10001" || state.Reference != references[role] || state.Image != imageIDs[role] || !validAcceptanceIdentity(state.Image, 32, "sha256:") {
			return errPublishedBinding
		}
		if role == "api" && len(state.DeviceRequests) != 0 || role == "backend" && (len(state.DeviceRequests) != 1 || len(state.PortBindings) != 0) {
			return errPublishedBinding
		}
	}
	return nil
}

func selectedPublishedImageIDs(ctx context.Context, target *DockerTarget) (map[string]string, error) {
	ids := map[string]string{}
	for role, ref := range map[string]string{"api": target.opts.APIImage, "backend": target.opts.BackendImage} {
		data, err := target.run(ctx, "selected published image identity", false, "image", "inspect", "--format", "{{.Id}}", ref)
		id := strings.TrimSpace(string(data))
		if err != nil || !validAcceptanceIdentity(id, 32, "sha256:") {
			return nil, errors.New("selected already-pulled published image identity is unavailable")
		}
		ids[role] = id
	}
	return ids, nil
}

// Docker's engine image ID is observed separately from the selected registry
// index. Depending on the image store it is not necessarily an OCI config digest.
type publishedContainerBinding struct {
	ContainerID   string `json:"container_id"`
	EngineImageID string `json:"engine_image_id"`
	Reference     string `json:"reference"`
}

type publishedBinding struct {
	ObservedAt          time.Time                            `json:"observed_at"`
	TestName            string                               `json:"test_name"`
	Scope               string                               `json:"scope"`
	Qualification       string                               `json:"qualification"`
	Candidate           publishedInventory                   `json:"candidate"`
	InventorySHA256     string                               `json:"inventory_sha256"`
	SelectionSHA256     string                               `json:"selection_sha256"`
	ConfigurationSHA256 string                               `json:"configuration_sha256"`
	AudioSHA256         string                               `json:"audio_sha256"`
	Settings            config.Settings                      `json:"effective_settings"`
	Containers          map[string]publishedContainerBinding `json:"containers"`
}

func observePublishedBinding(ctx context.Context, e *acceptanceEnvironment) (publishedBinding, error) {
	var binding publishedBinding
	if e.selection.Scope != publishedAcceptanceScope || e.selection.Published == nil {
		return binding, errPublishedBinding
	}
	references := map[string]string{"api": e.selection.APIImage, "backend": e.selection.BackendImage}
	ids := map[string]string{"api": e.selection.APIID, "backend": e.selection.BackendID}
	states := map[string]DockerInspection{}
	for _, role := range []string{"api", "backend"} {
		state, err := e.target.Inspect(ctx, role)
		if err != nil {
			return binding, err
		}
		states[role] = state
	}
	if err := verifyPublishedContainers(states, references, ids); err != nil {
		return binding, err
	}
	data, err := readBoundedAcceptanceJSON(e.config)
	if err != nil {
		return binding, err
	}
	// Bind bytes actually mounted, not merely the intended settings in memory.
	expected, err := json.Marshal(e.settings)
	if err != nil || !bytes.Equal(data, expected) {
		return binding, errors.New("bound application configuration differs from the selected scenario")
	}
	binding = publishedBinding{ObservedAt: time.Now().UTC(), TestName: e.testName, Scope: publishedAcceptanceScope, Qualification: "not_established", Candidate: *e.selection.Published, InventorySHA256: e.selection.InventorySHA256, SelectionSHA256: e.selection.SelectionSHA256, ConfigurationSHA256: acceptanceSHA256(data), AudioSHA256: strings.TrimPrefix(acceptanceSHA256(e.audio), "sha256:"), Settings: e.settings, Containers: map[string]publishedContainerBinding{}}
	for role, state := range states {
		binding.Containers[role] = publishedContainerBinding{ContainerID: state.ID, EngineImageID: state.Image, Reference: state.Reference}
	}
	return binding, nil
}

func recordPublishedBinding(ctx context.Context, e *acceptanceEnvironment) error {
	binding, err := observePublishedBinding(ctx, e)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return errors.New("cannot encode published candidate binding")
	}
	file, err := os.CreateTemp(e.workdir, "candidate-binding-*.json")
	if err != nil {
		return errors.New("cannot create private candidate binding")
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot retain private candidate binding")
	}
	return nil
}

func requirePublishedOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv("P07_GPU_TESTS") != "1" {
		t.Skip("published GPU slices require explicit P07_GPU_TESTS=1")
	}
	if os.Getenv("P07_PUBLISHED_SELECTION") == "" {
		t.Fatal("published GPU slices require P07_PUBLISHED_SELECTION; a local receipt is not a candidate selection")
	}
}

func openPublishedEnvironment(t *testing.T, configure func(*config.Settings)) *acceptanceEnvironment {
	t.Helper()
	requirePublishedOptIn(t)
	selection, err := loadPublishedSelection(os.Getenv("P07_PUBLISHED_SELECTION"))
	if err != nil {
		t.Fatal(err)
	}
	return openAcceptanceEnvironment(t, selection, configure)
}

// This runs implemented slices, not all P07 gates. An unfiltered opt-in run fails
// the explicit required-gap subtest rather than silently skipping full coverage.
func TestResidentSuite(t *testing.T) {
	requirePublishedOptIn(t)
	t.Run("baseline", func(t *testing.T) { runResidentBaseline(t, openPublishedEnvironment(t, nil)) })
	t.Run("public_contract", func(t *testing.T) {
		e := openPublishedEnvironment(t, nil)
		runPublicCases(t, e.ctx, e.client, e.selection.FixtureDirectory, e.settings.Models.Served)
	})
	t.Run("input_bounds_and_isolation", func(t *testing.T) { runInputBoundsAndIsolation(t, openPublishedEnvironment(t, nil)) })
	t.Run("fifo_and_queued_cancellation", func(t *testing.T) {
		runFIFOAndQueuedCancellation(t, openPublishedEnvironment(t, func(s *config.Settings) { s.Queue.WaitingCapacity = 2 }))
	})
	t.Run("queued_deadline", func(t *testing.T) {
		runQueuedOperationDeadline(t, openPublishedEnvironment(t, func(s *config.Settings) { s.Timeouts.OperationSeconds = 1; s.Queue.WaitingCapacity = 2 }))
	})
	t.Run("lifecycle", func(t *testing.T) { e := openPublishedEnvironment(t, nil); runLifecycleCases(t, e) })
	for _, mode := range []string{"graceful_term", "forced_kill"} {
		t.Run("active_api_shutdown/"+mode, func(t *testing.T) { runActiveAPIShutdown(t, openPublishedEnvironment(t, nil), mode) })
	}
	t.Run("model_observer", func(t *testing.T) { runModelObserver(t, openPublishedEnvironment(t, nil)) })
	t.Run("gpu_unavailable", func(t *testing.T) { runGPUUnavailable(t, openPublishedEnvironment(t, nil)) })
	t.Run("required_unimplemented_gates", func(t *testing.T) {
		t.Fatal("UNIMPLEMENTED full P07 gates: exact-candidate counted HTTPS/cache/model recipes without a CA derivative; configured idle and retention/failure memory envelope; remaining reload/raw-output/overlap PROVE mutations; concurrent measurements and feasible direct-native comparison; second-registry runtime recipe")
	})
}

func TestClientCompatibility(t *testing.T) {
	e := openPublishedEnvironment(t, nil)
	runClientCases(t, e.ctx, e.address, filepath.Join(e.selection.FixtureDirectory, "jfk.wav"), e.workdir)
}

func TestResidentMeasurements(t *testing.T) {
	runResidentMeasurements(t, openPublishedEnvironment(t, nil))
}
