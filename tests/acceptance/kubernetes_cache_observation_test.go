package acceptance

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// kubernetesCacheSelection is supplied privately by the caller for each stage.
// A replacement backend has a new Pod UID; the namespace and claim UIDs must
// remain the same. This helper does not perform lifecycle actions or inference.
type kubernetesCacheSelection struct {
	Kubeconfig, Context, Namespace, NamespaceUID, Release string
	BackendPod, BackendUID, Container, Claim, ClaimUID    string
	ArtifactPath, SHA256                                  string
	Size                                                  int64
	run                                                   func(context.Context, ...string) ([]byte, error)
}

type kubernetesCachePod struct {
	Metadata struct {
		UID               string            `json:"uid"`
		DeletionTimestamp *string           `json:"deletionTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Volumes []struct {
			Name string `json:"name"`
			PVC  *struct {
				ClaimName string `json:"claimName"`
			} `json:"persistentVolumeClaim"`
		} `json:"volumes"`
		Containers []struct {
			Name   string `json:"name"`
			Mounts []struct {
				Name, MountPath, SubPath, SubPathExpr string
			} `json:"volumeMounts"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase      string                          `json:"phase"`
		Conditions []struct{ Type, Status string } `json:"conditions"`
	} `json:"status"`
}

type kubernetesCacheClaim struct {
	Metadata struct {
		UID               string  `json:"uid"`
		DeletionTimestamp *string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func (s kubernetesCacheSelection) validate() error {
	hash, err := hex.DecodeString(s.SHA256)
	if !filepath.IsAbs(s.Kubeconfig) || s.Context == "" || s.Namespace == "" || s.NamespaceUID == "" || s.Release == "" || s.BackendPod == "" || s.BackendUID == "" || s.Container == "" || s.Claim == "" || s.ClaimUID == "" || !path.IsAbs(s.ArtifactPath) || path.Clean(s.ArtifactPath) != s.ArtifactPath || s.ArtifactPath == "/" || err != nil || len(hash) != 32 || strings.ToLower(s.SHA256) != s.SHA256 || s.Size <= 0 {
		return errors.New("complete explicit cache observation selection is required")
	}
	return nil
}

func (s kubernetesCacheSelection) command(ctx context.Context, args ...string) ([]byte, error) {
	run := s.run
	if run == nil {
		run = func(ctx context.Context, args ...string) ([]byte, error) {
			return runKubernetesCommand(ctx, "kubectl", 2*time.Minute, 1<<20, nil, args...)
		}
	}
	prefix := []string{"--kubeconfig", s.Kubeconfig, "--context", s.Context, "--namespace", s.Namespace}
	if len(args) > 0 && args[0] == "exec" {
		prefix = append(prefix, "--request-timeout=120s")
	} else {
		prefix = append(prefix, "--request-timeout=4s")
	}
	return run(ctx, append(prefix, args...)...)
}

// observeKubernetesRetainedClaim can be used after uninstall, when no Pod exists.
// Bound is required; absence, deletion and a recreated claim all fail closed.
func observeKubernetesRetainedClaim(ctx context.Context, s kubernetesCacheSelection) error {
	if !filepath.IsAbs(s.Kubeconfig) || s.Context == "" || s.Namespace == "" || s.NamespaceUID == "" || s.Release == "" || s.Claim == "" || s.ClaimUID == "" {
		return errors.New("complete explicit claim observation selection is required")
	}
	uid, err := s.command(ctx, "get", "namespace", s.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(uid) != s.NamespaceUID {
		return errors.New("cache namespace identity unavailable")
	}
	data, err := s.command(ctx, "get", "pvc", s.Claim, "-o", "json")
	var claim kubernetesCacheClaim
	if err != nil || json.Unmarshal(data, &claim) != nil || claim.Metadata.UID != s.ClaimUID || claim.Metadata.DeletionTimestamp != nil || claim.Status.Phase != "Bound" {
		return errors.New("bound cache claim identity unavailable")
	}
	return nil
}

func (s kubernetesCacheSelection) identities(ctx context.Context) (string, error) {
	if err := observeKubernetesRetainedClaim(ctx, s); err != nil {
		return "", err
	}
	data, err := s.command(ctx, "get", "pod", s.BackendPod, "-o", "json")
	var pod kubernetesCachePod
	if err != nil || json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != s.BackendUID || pod.Metadata.DeletionTimestamp != nil || pod.Metadata.Labels["app.kubernetes.io/instance"] != s.Release || pod.Metadata.Labels["app.kubernetes.io/component"] != "backend" || pod.Status.Phase != "Running" {
		return "", errors.New("cache backend identity unavailable")
	}
	ready := false
	for _, c := range pod.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			ready = true
		}
	}
	if !ready {
		return "", errors.New("cache backend is not ready")
	}
	volume := ""
	for _, v := range pod.Spec.Volumes {
		if v.PVC != nil && v.PVC.ClaimName == s.Claim {
			if volume != "" {
				return "", errors.New("ambiguous cache claim mount")
			}
			volume = v.Name
		}
	}
	mount := ""
	containers := 0
	for _, c := range pod.Spec.Containers {
		if c.Name != s.Container {
			continue
		}
		containers++
		for _, m := range c.Mounts {
			if !path.IsAbs(m.MountPath) || path.Clean(m.MountPath) != m.MountPath {
				return "", errors.New("invalid cache mount")
			}
			if s.ArtifactPath == m.MountPath || strings.HasPrefix(s.ArtifactPath, strings.TrimSuffix(m.MountPath, "/")+"/") {
				if mount != "" {
					return "", errors.New("ambiguous artifact mount")
				}
				if m.Name != volume || volume == "" || m.SubPath != "" || m.SubPathExpr != "" {
					return "", errors.New("artifact does not use selected cache claim")
				}
				mount = m.MountPath
			}
		}
	}
	if containers != 1 || mount == "" {
		return "", errors.New("selected cache container or mount unavailable")
	}
	return mount, nil
}

// observeKubernetesCache hashes the real file in the selected existing backend.
// The caller supplies the same expected claim UID, hash and size at each stage
// and owns uninstall, reinstall and offline evidence. Reads are not atomic.
func observeKubernetesCache(ctx context.Context, s kubernetesCacheSelection) error {
	if err := s.validate(); err != nil {
		return err
	}
	mount, err := s.identities(ctx)
	if err != nil {
		return err
	}
	// Reject symlink resolution outside or within the mount, and print only hash
	// and byte count. No raw model contents, path, identifiers or stderr are logged.
	const script = `p=$(realpath -e -- "$1") || exit 1; [ "$p" = "$1" ] || exit 1; [ -f "$p" ] || exit 1; h=$(sha256sum < "$p") || exit 1; n=$(wc -c < "$p") || exit 1; printf '%s\n%s\n' "${h%% *}" "$n"`
	data, err := s.command(ctx, "exec", s.BackendPod, "-c", s.Container, "--", "sh", "-c", script, "cache-observation", s.ArtifactPath)
	if err != nil {
		return errors.New("cached artifact observation unavailable")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return errors.New("cached artifact observation malformed")
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || fields[0] != s.SHA256 || size != s.Size {
		return errors.New("cached artifact identity changed")
	}
	after, err := s.identities(ctx)
	if err != nil || after != mount {
		return errors.New("cache selection changed during observation")
	}
	return nil
}
