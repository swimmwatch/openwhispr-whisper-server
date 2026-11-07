package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

var p08InputCachePath = regexp.MustCompile(`^/[A-Za-z0-9_-]+(?:/[A-Za-z0-9_.-]+)*$`)

// The config path must be the selected backend container's unambiguous cache
// mount, not a subpath or an unrelated directory. No PVC writes are performed.
func p08InputCacheMount(ctx context.Context, target p08ResourceTarget, cache string) error {
	if len(cache) > 256 || !p08InputCachePath.MatchString(cache) || filepath.Clean(cache) != cache {
		return errors.New("unsafe cache path")
	}
	for _, reserved := range []string{"/proc", "/sys", "/dev", "/tmp", "/etc"} {
		if cache == reserved || strings.HasPrefix(cache, reserved+"/") || strings.HasPrefix(reserved, cache+"/") {
			return errors.New("unsafe cache path")
		}
	}
	data, err := target.command(ctx, "get", "pod", target.BackendPod, "-o", "json")
	var pod struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Name   string                                                   `json:"name"`
				Mounts []struct{ Name, MountPath, SubPath, SubPathExpr string } `json:"volumeMounts"`
			} `json:"containers"`
		} `json:"spec"`
	}
	if err != nil || json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != target.BackendUID {
		return errors.New("cache mount identity unavailable")
	}
	containers, matches := 0, 0
	for _, container := range pod.Spec.Containers {
		if container.Name != target.BackendContainer {
			continue
		}
		containers++
		for _, mount := range container.Mounts {
			if mount.Name == "cache" {
				if mount.MountPath != cache || mount.SubPath != "" || mount.SubPathExpr != "" {
					return errors.New("cache mount mismatch")
				}
				matches++
			} else if mount.MountPath == cache || strings.HasPrefix(mount.MountPath, cache+"/") {
				return errors.New("nested cache mount unavailable")
			}
		}
	}
	if containers != 1 || matches != 1 {
		return errors.New("cache mount unavailable")
	}
	return nil
}

// This observes a bounded sample, not a complete CRI log stream or all terminal paths.
func p08InputPrivacy(ctx context.Context, target p08ResourceTarget, expected [2]p08ContainerIdentity, cache, marker string) error {
	before, err := target.identity(ctx)
	if err != nil || before != expected || marker == "" {
		return errors.New("privacy identity or inputs unavailable")
	}
	if p08InputCacheMount(ctx, target, cache) != nil {
		return errors.New("privacy cache mount unavailable")
	}
	for i, role := range []struct{ pod, container string }{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		logs, err := target.command(ctx, "logs", role.pod, "-c", role.container, "--limit-bytes=1048577")
		if err != nil || len(logs) == 0 || len(logs) >= 1<<20 || logs[len(logs)-1] != '\n' || checkResidentLogPrivacy(logs, marker) != nil {
			return errors.New("privacy log sample unavailable or violated")
		}
		const probe = `pattern=$1; shift; for root do [ -d "$root" ] && [ ! -L "$root" ] && [ -r /proc/self/mountinfo ] || exit 1; awk -v r="$root/" 'index($5,r)==1 {found=1} END {exit found ? 1 : 0}' /proc/self/mountinfo || exit 1; done; if grep -rFq -- "$pattern" "$@"; then printf leak; else code=$?; [ "$code" -eq 1 ] || exit "$code"; printf clean; fi`
		args := []string{"exec", role.pod, "-c", role.container, "--", "sh", "-ec", probe, "p08-privacy", marker, "/tmp"}
		if i == 1 {
			args = append(args, cache)
		}
		result, err := target.command(ctx, args...)
		if err != nil || string(result) != "clean" {
			return errors.New("privacy file sample unavailable or violated")
		}
	}
	after, err := target.identity(ctx)
	if err != nil || after != before {
		return errors.New("privacy identity changed")
	}
	return nil
}

func p08InputCandidateWork(ctx context.Context, refs, images [2]string, pods func(context.Context) ([2]string, error), owner func(context.Context) (p08HeapOwner, error), run func() bool) error {
	if owner == nil || run == nil {
		return errors.New("candidate owner and workload required")
	}
	return p08PublicCandidateWork(ctx, refs, pods, func() error {
		return runP08BoundHTTPSlice(ctx, images, func(ctx context.Context) (p08HeapOwner, error) {
			current, err := owner(ctx)
			if err != nil || p08PublicRuntimeMatchesCandidate(refs, current) != nil {
				return p08HeapOwner{}, errors.New("candidate runtime identity unavailable")
			}
			return current, nil
		}, run)
	})
}

// The explicit Kubernetes selector never substitutes default cluster settings.
// This runs a shared input slice only, not the full P08/privacy acceptance.
func TestP08InputBoundsPartial(t *testing.T) {
	if os.Getenv("WHISPER_TEST_TARGET") != "kubernetes" {
		t.Skip("requires explicit Kubernetes target selection")
	}
	selectionPath := os.Getenv("P08_PUBLISHED_SELECTION")
	if !filepath.IsAbs(selectionPath) {
		t.Fatal("absolute published candidate selection required")
	}
	published, err := loadPublishedSelection(selectionPath)
	if err != nil {
		t.Fatal("published candidate selection unavailable")
	}
	address, fixtures := os.Getenv("P08_DEVELOPMENT_API_URL"), published.FixtureDirectory
	model, port := os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_SHARED_BACKEND_PORT")
	portNumber, err := strconv.Atoi(port)
	if !isLoopbackAPIRoot(address) || !filepath.IsAbs(fixtures) || model == "" || err != nil || portNumber < 1 || portNumber > 65535 {
		t.Fatal("explicit loopback API, fixtures, model and backend port required")
	}
	selection, err := readP08SharedHTTPSelection(os.Getenv("P08_SHARED_HTTP_SELECTION"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := readP08ResourceTarget(selection.ResourceTarget)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	client, err := NewPublicClient(address, 10*time.Minute, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	refs := [2]string{published.APIImage, published.BackendImage}
	observe := func(ctx context.Context) (p08HeapOwner, error) {
		before, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil || p08PublicRuntimeMatchesCandidate(refs, before) != nil {
			return p08HeapOwner{}, errors.New("target candidate owner unavailable")
		}
		if p08VerifyHTTPForward(ctx, client, func(ctx context.Context) ([]byte, error) {
			return target.command(ctx, "logs", target.APIPod, "-c", target.APIContainer, "--since=30s", "--limit-bytes=1048577")
		}) != nil {
			return p08HeapOwner{}, errors.New("API forward correlation unavailable")
		}
		after, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil || after != before {
			return p08HeapOwner{}, errors.New("target owner changed")
		}
		return before, nil
	}
	owner, err := observe(ctx)
	if err != nil || owner.Containers[0].Image != selection.APIImageID || owner.Containers[1].Image != selection.BackendImageID {
		t.Fatal("selected candidate owner unavailable")
	}
	data, err := target.command(ctx, "exec", target.APIPod, "-c", target.APIContainer, "--", "cat", "/etc/whisper/config.json")
	if err != nil || len(data) == 0 || len(data) >= 1<<20 {
		t.Fatal("runtime configuration unavailable")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if os.WriteFile(path, data, 0600) != nil {
		t.Fatal("cannot stage private runtime configuration")
	}
	settings, err := config.Load([]string{"--config", path}, []string{})
	if err != nil {
		t.Fatal("runtime configuration invalid")
	}
	fingerprint, err := config.CanonicalFingerprint(settings)
	if err != nil || fingerprint != owner.Process.fingerprint || settings.Models.Served != model || settings.Backend.Port != portNumber || settings.Limits.DecodedSeconds != 900 || settings.Limits.AudioBytes < 16 || settings.Limits.AudioBytes > 128<<20 || settings.Limits.PromptBytes < 1 || settings.Limits.PromptBytes > 1<<20 {
		t.Fatal("runtime settings do not match the selected resident or workload bounds")
	}
	audio, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal("public WAV fixture unavailable")
	}
	identity, err := target.identity(ctx)
	if err != nil || identity != owner.Process.containers {
		t.Fatal("selected container identity unavailable")
	}
	baseline, err := p08PairScratch(ctx, target, identity)
	if err != nil || !p08APIScratchClean(baseline[0]) || p08CheckAPIResource(ctx, target, identity) != nil {
		t.Fatal("initial scratch observation unavailable")
	}
	work := inputBoundsWork{ctx: ctx, client: client, model: model, audio: audio, fixtures: fixtures, audioBytes: settings.Limits.AudioBytes, promptBytes: settings.Limits.PromptBytes, privacy: func(ctx context.Context, marker string) error {
		return p08InputPrivacy(ctx, target, identity, settings.Storage.CacheDir, marker)
	}}
	images := [2]string{selection.APIImageID, selection.BackendImageID}
	work.bracket = func(run func() bool) error {
		return p08InputCandidateWork(ctx, refs, images, func(ctx context.Context) ([2]string, error) {
			return p08PublicPodImages(ctx, target)
		}, observe, run)
	}
	if !t.Run("shared_input_bounds_and_isolation", func(t *testing.T) { runInputBoundsWork(t, work) }) {
		t.Fatal("shared input workload failed")
	}
	cleanup, stopCleanup := context.WithTimeout(ctx, time.Duration(settings.Timeouts.CleanupSeconds)*time.Second)
	defer stopCleanup()
	if p08WaitPairScratch(cleanup, target, identity, baseline) != nil || p08CheckAPIResource(cleanup, target, identity) != nil {
		t.Fatal("scratch cleanup observation unavailable or violated")
	}
	after, err := observe(ctx)
	if err != nil || after != owner {
		t.Fatal("selected resident changed during cleanup")
	}
	t.Log("partial shared input slice only: sampled logs/files and scratch do not establish complete privacy or P08 qualification")
}
