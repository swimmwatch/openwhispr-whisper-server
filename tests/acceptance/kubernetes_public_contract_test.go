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
)

// Pod spec references are checked separately from runtime image IDs: an OCI
// index digest is not necessarily the runtime's platform image identity.
func p08PublicPodImages(ctx context.Context, target p08ResourceTarget) ([2]string, error) {
	var images [2]string
	for i, selected := range []struct{ pod, uid, container string }{{target.APIPod, target.APIUID, target.APIContainer}, {target.BackendPod, target.BackendUID, target.BackendContainer}} {
		data, err := target.command(ctx, "get", "pod", selected.pod, "-o", "json")
		var pod struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name  string `json:"name"`
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		}
		if err != nil || json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != selected.uid {
			return [2]string{}, errors.New("candidate Pod image observation unavailable")
		}
		count := 0
		for _, container := range pod.Spec.Containers {
			if container.Name == selected.container {
				count++
				images[i] = container.Image
			}
		}
		if count != 1 || images[i] == "" {
			return [2]string{}, errors.New("candidate Pod image observation unavailable")
		}
	}
	return images, nil
}

func p08PublicCandidateWork(ctx context.Context, expected [2]string, observe func(context.Context) ([2]string, error), run func() error) error {
	if expected[0] == "" || expected[1] == "" || observe == nil || run == nil {
		return errors.New("candidate references and observations required")
	}
	before, err := observe(ctx)
	if err != nil || before != expected {
		return errors.New("candidate Pod references unavailable or mismatched")
	}
	workErr := run()
	after, err := observe(ctx)
	if err != nil || after != before {
		return errors.New("candidate Pod references changed or became unavailable")
	}
	return workErr
}

// Unknown platform digests cannot establish a relationship to the selected index.
func p08PublicRuntimeMatchesCandidate(refs [2]string, owner p08HeapOwner) error {
	for i, ref := range refs {
		name, digest, ok := strings.Cut(ref, "@")
		if !ok || name == "" || !validAcceptanceIdentity(digest, 32, "sha256:") {
			return errors.New("published candidate image identity unavailable")
		}
		actual := owner.Containers[i].Image
		for _, prefix := range []string{"docker-pullable://", "containerd://"} {
			if strings.HasPrefix(actual, prefix) {
				actual = strings.TrimPrefix(actual, prefix)
				break
			}
		}
		if actual != ref && actual != digest {
			return errors.New("runtime image identity does not establish selected candidate index")
		}
	}
	return nil
}

// This is a partial workload on an already authorized, selected candidate pair.
// No Kubernetes write, port-forward setup, or inference is performed without opt-in.
func TestP08PublicCandidateContract(t *testing.T) {
	if os.Getenv("P08_PUBLIC_CANDIDATE") != "1" {
		t.Skip("requires an authorized candidate Kubernetes pair")
	}
	selectionPath := os.Getenv("P08_PUBLISHED_SELECTION")
	if !filepath.IsAbs(selectionPath) {
		t.Fatal("absolute published candidate selection required")
	}
	selected, err := loadPublishedSelection(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := readP08SharedHTTPSelection(os.Getenv("P08_SHARED_HTTP_SELECTION"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := readP08ResourceTarget(selection.ResourceTarget)
	if err != nil {
		t.Fatal(err)
	}
	address, model := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL")
	evidence, port := os.Getenv("P08_DEVELOPMENT_EVIDENCE"), os.Getenv("P08_SHARED_BACKEND_PORT")
	n, err := strconv.Atoi(port)
	info, statErr := os.Stat(evidence)
	if !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(evidence) || statErr != nil || !info.IsDir() || info.Mode().Perm() != 0700 || err != nil || n < 1 || n > 65535 {
		t.Fatal("explicit loopback, model, private evidence and backend port required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	client, err := NewPublicClient(address, 120*time.Second, 1<<20)
	if err != nil {
		t.Fatal("bounded public client unavailable")
	}
	defer client.Close()
	refs := [2]string{selected.APIImage, selected.BackendImage}
	observe := func(ctx context.Context) (p08HeapOwner, error) {
		before, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil {
			return p08HeapOwner{}, err
		}
		if err = p08PublicRuntimeMatchesCandidate(refs, before); err != nil {
			return p08HeapOwner{}, err
		}
		if err = p08VerifyHTTPForward(ctx, client, func(ctx context.Context) ([]byte, error) {
			return target.command(ctx, "logs", target.APIPod, "-c", target.APIContainer, "--since=30s", "--limit-bytes=1048576")
		}); err != nil {
			return p08HeapOwner{}, err
		}
		after, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil || after != before {
			return p08HeapOwner{}, errors.New("selected workload identity changed")
		}
		return before, nil
	}
	images := [2]string{selection.APIImageID, selection.BackendImageID}
	for _, slice := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"public_contract", func(t *testing.T) { runPublicCases(t, ctx, client, selected.FixtureDirectory, model) }},
		{"client_compatibility", func(t *testing.T) {
			runClientCases(t, ctx, address, filepath.Join(selected.FixtureDirectory, "jfk.wav"), evidence)
		}},
	} {
		err := p08PublicCandidateWork(ctx, refs, func(ctx context.Context) ([2]string, error) { return p08PublicPodImages(ctx, target) }, func() error {
			return runP08BoundHTTPSlice(ctx, images, observe, func() bool { return t.Run(slice.name, slice.run) })
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Log("partial public/SDK evidence only; identity is sampled at boundaries; CUDA activity and P08 qualification remain unestablished")
}
