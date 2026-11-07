package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// This observer never deploys, scales or selects a global Kubernetes context.
// The parent owns the ready queue-capacity-two target and its GPU test window.
type developmentQueueTarget struct {
	Kubeconfig   string `json:"kubeconfig"`
	Context      string `json:"context"`
	Namespace    string `json:"namespace"`
	NamespaceUID string `json:"namespace_uid"`
	Release      string `json:"release"`
	APIPod       string `json:"api_pod"`
	BackendPod   string `json:"backend_pod"`
}

func (target developmentQueueTarget) command(ctx context.Context, args ...string) ([]byte, error) {
	selected := []string{"--kubeconfig", target.Kubeconfig, "--context", target.Context, "--namespace", target.Namespace, "--request-timeout=4s"}
	return runKubernetesCommand(ctx, "kubectl", 5*time.Second, 1<<20, nil, append(selected, args...)...)
}

func (target developmentQueueTarget) verify(ctx context.Context) error {
	data, err := target.command(ctx, "get", "namespace", target.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(data) != target.NamespaceUID {
		return errors.New("development namespace identity does not match the authorized target")
	}
	for role, name := range map[string]string{"api": target.APIPod, "backend": target.BackendPod} {
		data, err = target.command(ctx, "get", "pod", name, "-o", "json")
		var pod struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		}
		if err != nil || json.Unmarshal(data, &pod) != nil ||
			pod.Metadata.Labels["app.kubernetes.io/instance"] != target.Release ||
			pod.Metadata.Labels["app.kubernetes.io/component"] != role {
			return errors.New("development Pod does not belong to the selected release and role")
		}
	}
	return nil
}

func (target developmentQueueTarget) health(ctx context.Context, port string) (*DockerPrivateHealth, error) {
	data, err := target.command(ctx, "exec", target.BackendPod, "--", "curl", "--silent", "--show-error", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "2", "--write-out", "\n%{http_code}", "http://127.0.0.1:"+port+"/health")
	if err != nil {
		return nil, err
	}
	return parsePrivateHealth(data)
}

func TestDevelopmentQueue(t *testing.T) {
	queue := os.Getenv("P08_DEVELOPMENT_QUEUE") == "1"
	deadline := os.Getenv("P08_DEVELOPMENT_DEADLINE") == "1"
	if !queue && !deadline {
		t.Skip("requires a parent-owned Kubernetes queue test window")
	}
	if queue && deadline {
		t.Fatal("select exactly one parent-owned queue configuration")
	}
	address := os.Getenv("P08_DEVELOPMENT_API_URL")
	fixtures := os.Getenv("P08_DEVELOPMENT_FIXTURES")
	model := os.Getenv("P08_DEVELOPMENT_MODEL")
	if !isLoopbackAPIRoot(address) || fixtures == "" || model == "" {
		t.Fatal("explicit credential-free loopback API, public fixtures and model are required")
	}
	file, err := os.Open(os.Getenv("P08_DEVELOPMENT_TARGET"))
	if err != nil {
		t.Fatal("explicit private Kubernetes target input is unavailable")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 64<<10+1))
	if err != nil || len(content) > 64<<10 {
		t.Fatal("private Kubernetes target input exceeded its bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var target developmentQueueTarget
	if decoder.Decode(&target) != nil || decoder.Decode(new(any)) != io.EOF ||
		target.Kubeconfig == "" || target.Context == "" || target.Namespace == "" ||
		target.NamespaceUID == "" || target.Release == "" || target.APIPod == "" || target.BackendPod == "" {
		t.Fatal("explicit complete Kubernetes target selection is required")
	}
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("canonical workload bounds unavailable")
	}
	if deadline {
		// The parent must separately configure the selected target with this deadline.
		// One second expired in audio staging on this target before native dispatch.
		settings.Timeouts.OperationSeconds = 5
	}
	timeout := time.Duration(settings.Timeouts.OperationSeconds+settings.Timeouts.CleanupSeconds+5) * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), 2*timeout+time.Duration(settings.Timeouts.ProvisioningSeconds)*time.Second+time.Minute)
	defer cancel()
	if target.verify(ctx) != nil {
		t.Fatal("development target ownership verification failed")
	}
	short, err := readPublicCaseFixture(fixtures, "jfk.wav")
	if err != nil {
		t.Fatal(err)
	}
	long, err := readPublicCaseFixture(fixtures, "jfk-900.flac")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(settings.Backend.Port)
	before, err := target.health(ctx, port)
	if err != nil || before.HTTPStatus != http.StatusOK || before.Status != "ready" || !before.CUDAReady || before.Model != model {
		t.Fatal("queue target is not a ready CUDA resident of the selected model")
	}
	work := queueWorkload{
		ctx: ctx, model: model, short: short, long: long,
		cleanupTimeout: time.Duration(settings.Timeouts.CleanupSeconds) * time.Second,
		client:         func(t *testing.T, id string) *PublicClient { return identifiedPublicClient(t, address, timeout, id) },
		logs: func(ctx context.Context) ([]byte, error) {
			return target.command(ctx, "logs", target.APIPod, "--limit-bytes=1048576")
		},
		released: func(ctx context.Context) error {
			if err := target.verify(ctx); err != nil {
				return err
			}
			data, err := target.command(ctx, "exec", target.APIPod, "--", "sh", "-eu", "-c", dockerResourceProbe, "development-observe", "api")
			if err != nil {
				return err
			}
			resource, _, err := parseResourceSnapshot(data, "api")
			if err != nil {
				return err
			}
			if err := releasedScratchResources(resource); err != nil {
				return err
			}
			after, err := target.health(ctx, port)
			if err != nil || after.HTTPStatus != http.StatusOK || after.Status != "ready" || !after.CUDAReady || after.Model != model ||
				after.ConfigurationFingerprint != before.ConfigurationFingerprint || (!deadline && after.ProcessGeneration != before.ProcessGeneration) {
				return errors.New("queue workload did not recover the expected ready resident")
			}
			return nil
		},
	}
	if !deadline {
		runQueueWorkload(t, work)
		return
	}
	runQueuedDeadlineWorkload(t, work, timeout, func() {
		if err := waitDevelopmentReady(ctx, address, time.Duration(settings.Timeouts.ProvisioningSeconds)*time.Second); err != nil {
			t.Fatal(err)
		}
	})
}

func waitDevelopmentReady(parent context.Context, address string, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/health/ready", nil)
		if err != nil {
			return errors.New("invalid development readiness endpoint")
		}
		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil && response.StatusCode == http.StatusOK {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("development target did not recover readiness within its provisioning budget")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
