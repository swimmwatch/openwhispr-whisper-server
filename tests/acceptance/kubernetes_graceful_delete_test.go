package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"time"
)

// The parent supplies the exact identity captured before the workload. The
// precondition is enforced by the API server, not by a preceding GET alone.
type gracefulDeleteTarget struct {
	target                                              developmentQueueTarget
	apiUID, backendUID, container, containerID, imageID string
	role                                                string // Empty retains the API shutdown binding.
	run                                                 func(context.Context, []string, []byte) ([]byte, error)
}

var kubernetesDNSLabel = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
var kubernetesDNSName = regexp.MustCompile(`^[a-z0-9](?:[-.a-z0-9]*[a-z0-9])?$`)

func gracefulKubectl(ctx context.Context, args []string, input []byte) ([]byte, error) {
	return runKubernetesCommand(ctx, "kubectl", 5*time.Second, 1<<20, input, args...)
}

func (g gracefulDeleteTarget) command(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	t := g.target
	run := g.run
	if run == nil {
		run = gracefulKubectl
	}
	prefix := []string{"--kubeconfig", t.Kubeconfig, "--context", t.Context, "--namespace", t.Namespace, "--request-timeout=4s"}
	return run(ctx, append(prefix, args...), input)
}

func (g gracefulDeleteTarget) delete(ctx context.Context) error {
	t := g.target
	role, pod, uid := g.role, t.APIPod, g.apiUID
	if role == "" {
		role = "api"
	}
	if role == "backend" {
		pod, uid = t.BackendPod, g.backendUID
	}
	if role != "api" && role != "backend" {
		return errors.New("invalid deletion role")
	}
	if !filepath.IsAbs(t.Kubeconfig) || t.Context == "" || t.NamespaceUID == "" || t.Release == "" || g.apiUID == "" || g.backendUID == "" || g.container == "" || g.containerID == "" || g.imageID == "" || len(t.Namespace) > 63 || !kubernetesDNSLabel.MatchString(t.Namespace) || len(t.APIPod) > 253 || !kubernetesDNSName.MatchString(t.APIPod) || len(t.BackendPod) > 253 || !kubernetesDNSName.MatchString(t.BackendPod) || t.APIPod == t.BackendPod {
		return errors.New("complete explicit private deletion target required")
	}
	ns, err := g.command(ctx, nil, "get", "namespace", t.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(ns) != t.NamespaceUID {
		return errors.New("namespace identity unavailable")
	}
	for _, item := range []struct{ name, uid, role string }{{t.APIPod, g.apiUID, "api"}, {t.BackendPod, g.backendUID, "backend"}} {
		data, err := g.command(ctx, nil, "get", "pod", item.name, "-o", "json")
		var p struct {
			Metadata struct {
				UID               string            `json:"uid"`
				Labels            map[string]string `json:"labels"`
				DeletionTimestamp *string           `json:"deletionTimestamp"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				ContainerStatuses []struct {
					Name        string `json:"name"`
					Ready       bool   `json:"ready"`
					ContainerID string `json:"containerID"`
					ImageID     string `json:"imageID"`
				} `json:"containerStatuses"`
			} `json:"status"`
		}
		if err != nil || json.Unmarshal(data, &p) != nil || p.Metadata.UID != item.uid || p.Metadata.DeletionTimestamp != nil || p.Metadata.Labels["app.kubernetes.io/instance"] != t.Release || p.Metadata.Labels["app.kubernetes.io/component"] != item.role {
			return errors.New("selected Pod identity unavailable")
		}
		ready := 0
		for _, c := range p.Status.Conditions {
			if c.Type == "Ready" {
				if c.Status != "True" {
					return errors.New("selected Pod is not ready")
				}
				ready++
			}
		}
		if ready != 1 {
			return errors.New("selected Pod is not ready")
		}
		if item.role == role {
			matched := 0
			for _, c := range p.Status.ContainerStatuses {
				if c.Name == g.container {
					if !c.Ready || c.ContainerID != g.containerID || c.ImageID != g.imageID {
						return errors.New("selected API container identity unavailable")
					}
					matched++
				}
			}
			if matched != 1 {
				return errors.New("selected API container identity unavailable")
			}
		}
	}
	body, err := json.Marshal(struct {
		APIVersion    string `json:"apiVersion"`
		Kind          string `json:"kind"`
		Preconditions struct {
			UID string `json:"uid"`
		} `json:"preconditions"`
	}{APIVersion: "v1", Kind: "DeleteOptions", Preconditions: struct {
		UID string `json:"uid"`
	}{UID: uid}})
	if err != nil {
		return errors.New("cannot encode deletion precondition")
	}
	_, err = g.command(ctx, body, "delete", "--raw", "/api/v1/namespaces/"+t.Namespace+"/pods/"+pod, "-f", "-")
	if err != nil {
		return errors.New("graceful preconditioned deletion was not confirmed")
	}
	return nil
}
