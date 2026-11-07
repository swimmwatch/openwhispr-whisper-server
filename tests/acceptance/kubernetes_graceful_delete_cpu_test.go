package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGracefulDeleteBindsIdentityAndUIDPrecondition(t *testing.T) {
	for _, fault := range []string{"none", "namespace", "api-uid", "backend-uid", "release", "role", "terminating", "not-ready", "container", "image", "delete-conflict"} {
		t.Run(fault, func(t *testing.T) {
			target := gracefulDeleteTarget{target: developmentQueueTarget{Kubeconfig: "/private/kubeconfig", Context: "selected", Namespace: "selected", NamespaceUID: "namespace-id", Release: "release", APIPod: "api-0", BackendPod: "backend-0"}, apiUID: "api-id", backendUID: "backend-id", container: "api", containerID: "container-id", imageID: "image-id"}
			deletes := 0
			target.run = func(_ context.Context, args []string, input []byte) ([]byte, error) {
				if strings.Join(args[:6], " ") != "--kubeconfig /private/kubeconfig --context selected --namespace selected" {
					t.Fatal("unscoped command")
				}
				if args[7] == "delete" {
					deletes++
					if strings.Join(args[7:], " ") != "delete --raw /api/v1/namespaces/selected/pods/api-0 -f -" {
						t.Fatal("incorrect delete route")
					}
					var body struct {
						APIVersion    string `json:"apiVersion"`
						Kind          string `json:"kind"`
						Preconditions struct {
							UID string `json:"uid"`
						} `json:"preconditions"`
						GracePeriodSeconds *int64 `json:"gracePeriodSeconds"`
					}
					if json.Unmarshal(input, &body) != nil || body.APIVersion != "v1" || body.Kind != "DeleteOptions" || body.Preconditions.UID != "api-id" || body.GracePeriodSeconds != nil {
						t.Fatal("invalid UID-preconditioned graceful deletion")
					}
					if fault == "delete-conflict" {
						return nil, errors.New("conflict")
					}
					return []byte(`{}`), nil
				}
				if args[8] == "namespace" {
					if fault == "namespace" {
						return []byte("other"), nil
					}
					return []byte("namespace-id"), nil
				}
				name := args[9]
				role, uid := "api", "api-id"
				if name == "backend-0" {
					role, uid = "backend", "backend-id"
				}
				if fault == role+"-uid" {
					uid = "other"
				}
				release := "release"
				if fault == "release" {
					release = "other"
				}
				if fault == "role" && role == "api" {
					role = "other"
				}
				terminating := "null"
				if fault == "terminating" {
					terminating = `"2026-01-01T00:00:00Z"`
				}
				ready := "True"
				if fault == "not-ready" {
					ready = "False"
				}
				containerID, image := "container-id", "image-id"
				if fault == "container" {
					containerID = "other"
				}
				if fault == "image" {
					image = "other"
				}
				return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"deletionTimestamp":%s,"labels":{"app.kubernetes.io/instance":%q,"app.kubernetes.io/component":%q}},"status":{"conditions":[{"type":"Ready","status":%q}],"containerStatuses":[{"name":"api","ready":true,"containerID":%q,"imageID":%q}]}}`, uid, terminating, release, role, ready, containerID, image)), nil
			}
			err := target.delete(context.Background())
			if (err == nil) != (fault == "none") {
				t.Fatalf("unexpected result: %v", err)
			}
			if (deletes == 1) != (fault == "none" || fault == "delete-conflict") {
				t.Fatal("deletion issued despite failed identity check")
			}
		})
	}
}
