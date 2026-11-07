package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestP08HelmPolicyReadRejectsUnsafeOrChangedState(t *testing.T) {
	for _, tc := range []struct {
		name, policy, strategy, uid, rv, selectorRelease, selectorComponent, selectorName, owner string
		expressions                                                                              bool
		want                                                                                     bool
	}{
		{"safe", "Retain", "RollingUpdate", "set-uid", "7", "release", "backend", "app", "set-uid", false, true},
		{"delete-on-removal", "Delete", "RollingUpdate", "set-uid", "7", "release", "backend", "app", "set-uid", false, false},
		{"on-delete", "Retain", "OnDelete", "set-uid", "7", "release", "backend", "app", "set-uid", false, false},
		{"recreated", "Retain", "RollingUpdate", "other", "7", "release", "backend", "app", "set-uid", false, false},
		{"changed", "Retain", "RollingUpdate", "set-uid", "8", "release", "backend", "app", "set-uid", false, false},
		{"wrong-release-selector", "Retain", "RollingUpdate", "set-uid", "7", "other", "backend", "app", "set-uid", false, false},
		{"wrong-component-selector", "Retain", "RollingUpdate", "set-uid", "7", "release", "api", "app", "set-uid", false, false},
		{"wrong-other-selector", "Retain", "RollingUpdate", "set-uid", "7", "release", "backend", "other", "set-uid", false, false},
		{"unknown-selector-expression", "Retain", "RollingUpdate", "set-uid", "7", "release", "backend", "app", "set-uid", true, false},
		{"wrong-owner", "Retain", "RollingUpdate", "set-uid", "7", "release", "backend", "app", "other", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/config", Context: "ctx", Namespace: "ns", NamespaceUID: "namespace-uid", Release: "release", APIPod: "api-0", APIUID: "api-uid", APIContainer: "api", BackendPod: "backend-0", BackendUID: "backend-uid", BackendContainer: "backend"}
			reads := 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				switch cmd := strings.Join(args, " "); {
				case strings.Contains(cmd, "get namespace"):
					return []byte("namespace-uid"), nil
				case strings.Contains(cmd, "get pod"):
					return json.Marshal(map[string]any{"metadata": map[string]any{"uid": "backend-uid", "labels": map[string]string{"app.kubernetes.io/instance": "release", "app.kubernetes.io/component": "backend", "app.kubernetes.io/name": "app"}, "ownerReferences": []any{map[string]any{"apiVersion": "apps/v1", "kind": "StatefulSet", "name": "backend", "uid": tc.owner, "controller": true}}}})
				case strings.Contains(cmd, "get statefulset"):
					reads++
					uid, rv := "set-uid", "7"
					if reads == 2 {
						uid, rv = tc.uid, tc.rv
					}
					selector := map[string]any{"matchLabels": map[string]string{"app.kubernetes.io/instance": tc.selectorRelease, "app.kubernetes.io/component": tc.selectorComponent, "app.kubernetes.io/name": tc.selectorName}}
					if tc.expressions {
						selector["matchExpressions"] = []any{map[string]any{"key": "x", "operator": "Exists"}}
					}
					return json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid, "resourceVersion": rv, "labels": map[string]string{"app.kubernetes.io/instance": "release"}}, "spec": map[string]any{"selector": selector, "replicas": 1, "podManagementPolicy": "OrderedReady", "updateStrategy": map[string]string{"type": tc.strategy}, "persistentVolumeClaimRetentionPolicy": map[string]string{"whenDeleted": tc.policy, "whenScaled": "Retain"}, "volumeClaimTemplates": []any{map[string]any{"metadata": map[string]string{"name": "cache"}}}}})
				default:
					return nil, errors.New("unexpected read")
				}
			}
			err := observeP08HelmPolicy(context.Background(), target)
			if (err == nil) != tc.want {
				t.Fatalf("unexpected policy observation result: %v", err)
			}
		})
	}
}
