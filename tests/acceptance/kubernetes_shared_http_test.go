package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This bracket establishes continuity at its boundaries, not continuously.
func runP08BoundHTTPSlice(ctx context.Context, images [2]string, observe func(context.Context) (p08HeapOwner, error), run func() bool) error {
	if images[0] == "" || images[1] == "" || observe == nil || run == nil {
		return errors.New("explicit image identities and workload are required")
	}
	before, err := observe(ctx)
	if err != nil || before.Containers[0].Image != images[0] || before.Containers[1].Image != images[1] {
		return errors.New("selected workload identity unavailable or mismatched")
	}
	passed := run()
	after, err := observe(ctx)
	if err != nil || after != before {
		return errors.New("selected workload identity changed or became unavailable")
	}
	if !passed {
		return errors.New("shared public workload failed")
	}
	return nil
}

type p08SharedHTTPSelection struct {
	ResourceTarget string `json:"resource_target"`
	APIImageID     string `json:"api_image_id"`
	BackendImageID string `json:"backend_image_id"`
}

func readP08SharedHTTPSelection(path string) (p08SharedHTTPSelection, error) {
	var selection p08SharedHTTPSelection
	if !filepath.IsAbs(path) {
		return selection, errors.New("absolute private HTTP selection required")
	}
	f, err := os.Open(path)
	if err != nil {
		return selection, errors.New("private HTTP selection unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return selection, errors.New("private HTTP selection must be a regular mode 0600 file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return selection, errors.New("private HTTP selection unavailable or too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&selection) != nil || d.Decode(new(any)) != io.EOF || !filepath.IsAbs(selection.ResourceTarget) || selection.APIImageID == "" || selection.BackendImageID == "" {
		return p08SharedHTTPSelection{}, errors.New("complete private HTTP selection required")
	}
	return selection, nil
}

// Only parent-owned Kubernetes HTTP/client workloads; this does not qualify P08.
func TestP08SharedHTTPPartial(t *testing.T) {
	if os.Getenv("P08_SHARED_HTTP") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes pair and loopback forward")
	}
	address := os.Getenv("P08_DEVELOPMENT_API_URL")
	fixtures := os.Getenv("P08_DEVELOPMENT_FIXTURES")
	evidence := os.Getenv("P08_DEVELOPMENT_EVIDENCE")
	model := os.Getenv("P08_DEVELOPMENT_MODEL")
	port := os.Getenv("P08_SHARED_BACKEND_PORT")
	portNumber, err := strconv.Atoi(port)
	if !isLoopbackAPIRoot(address) || !filepath.IsAbs(fixtures) || !filepath.IsAbs(evidence) || model == "" || err != nil || portNumber < 1 || portNumber > 65535 {
		t.Fatal("explicit loopback API, absolute public fixtures and private evidence, model and backend port required")
	}
	info, err := os.Stat(evidence)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("existing private evidence directory with mode 0700 required")
	}
	selection, err := readP08SharedHTTPSelection(os.Getenv("P08_SHARED_HTTP_SELECTION"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := readP08ResourceTarget(selection.ResourceTarget)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	images := [2]string{selection.APIImageID, selection.BackendImageID}
	client, err := NewPublicClient(address, 120*time.Second, 1<<20)
	if err != nil {
		t.Fatal("cannot construct bounded public client")
	}
	defer client.Close()
	observe := func(ctx context.Context) (p08HeapOwner, error) {
		before, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil {
			return p08HeapOwner{}, err
		}
		err = p08VerifyHTTPForward(ctx, client, func(ctx context.Context) ([]byte, error) {
			return target.command(ctx, "logs", target.APIPod, "-c", target.APIContainer, "--since=30s", "--limit-bytes=1048576")
		})
		if err != nil {
			return p08HeapOwner{}, err
		}
		after, err := p08ReadHeapOwner(ctx, target, model, port)
		if err != nil || after != before {
			return p08HeapOwner{}, errors.New("selected API identity changed during forward correlation")
		}
		return before, nil
	}
	for _, slice := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"public_contract", func(t *testing.T) { runPublicCases(t, ctx, client, fixtures, model) }},
		{"client_compatibility", func(t *testing.T) { runClientCases(t, ctx, address, filepath.Join(fixtures, "jfk.wav"), evidence) }},
	} {
		err := runP08BoundHTTPSlice(ctx, images, observe, func() bool { return t.Run(slice.name, slice.run) })
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Log("shared public/SDK slices only; endpoint correlation is sampled at boundaries, GPU activity, lifecycle, memory and full P08 qualification are not established")
}
