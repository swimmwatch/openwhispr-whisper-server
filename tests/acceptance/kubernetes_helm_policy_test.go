package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// observeP08HelmPolicy reads the actual StatefulSet controlling the selected
// backend. This is a point-in-time configuration check, not continuous GPU
// observation, a mutation, or evidence of uninstall/reinstall retention.
func observeP08HelmPolicy(ctx context.Context, target p08ResourceTarget) error {
	fail := errors.New("selected backend retention or rollout policy unavailable or changed")
	if !target.valid() {
		return fail
	}
	readOwner := func() (string, string, map[string]string, error) {
		uid, err := target.command(ctx, "get", "namespace", target.Namespace, "-o", "jsonpath={.metadata.uid}")
		if err != nil || string(uid) != target.NamespaceUID {
			return "", "", nil, fail
		}
		data, err := target.command(ctx, "get", "pod", target.BackendPod, "-o", "json")
		var pod struct {
			Metadata struct {
				UID      string            `json:"uid"`
				Deletion *string           `json:"deletionTimestamp"`
				Labels   map[string]string `json:"labels"`
				Owners   []struct {
					APIVersion, Kind, Name, UID string
					Controller                  *bool `json:"controller"`
				} `json:"ownerReferences"`
			} `json:"metadata"`
		}
		if err != nil || json.Unmarshal(data, &pod) != nil || pod.Metadata.UID != target.BackendUID || pod.Metadata.Deletion != nil || pod.Metadata.Labels["app.kubernetes.io/instance"] != target.Release || pod.Metadata.Labels["app.kubernetes.io/component"] != "backend" {
			return "", "", nil, fail
		}
		name, ownerUID := "", ""
		for _, o := range pod.Metadata.Owners {
			if o.Controller != nil && *o.Controller {
				if name != "" || o.APIVersion != "apps/v1" || o.Kind != "StatefulSet" || !p08Name.MatchString(o.Name) || len(o.Name) > 253 || o.UID == "" {
					return "", "", nil, fail
				}
				name, ownerUID = o.Name, o.UID
			}
		}
		if name == "" {
			return "", "", nil, fail
		}
		return name, ownerUID, pod.Metadata.Labels, nil
	}
	name, ownerUID, podLabels, err := readOwner()
	if err != nil {
		return fail
	}
	readSet := func(labels map[string]string) (string, error) {
		data, err := target.command(ctx, "get", "statefulset", name, "-o", "json")
		var s struct {
			Metadata struct {
				UID, ResourceVersion string
				Deletion             *string           `json:"deletionTimestamp"`
				Labels               map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Selector struct {
					MatchLabels      map[string]string `json:"matchLabels"`
					MatchExpressions []json.RawMessage `json:"matchExpressions"`
				} `json:"selector"`
				Replicas            *int   `json:"replicas"`
				PodManagementPolicy string `json:"podManagementPolicy"`
				UpdateStrategy      struct {
					Type string `json:"type"`
				} `json:"updateStrategy"`
				Retention struct{ WhenDeleted, WhenScaled string } `json:"persistentVolumeClaimRetentionPolicy"`
				Templates []json.RawMessage                        `json:"volumeClaimTemplates"`
			} `json:"spec"`
		}
		if err != nil || json.Unmarshal(data, &s) != nil || s.Metadata.UID != ownerUID || s.Metadata.ResourceVersion == "" || s.Metadata.Deletion != nil || s.Metadata.Labels["app.kubernetes.io/instance"] != target.Release || s.Spec.Selector.MatchLabels["app.kubernetes.io/instance"] != target.Release || s.Spec.Selector.MatchLabels["app.kubernetes.io/component"] != "backend" || len(s.Spec.Selector.MatchExpressions) != 0 || s.Spec.Replicas == nil || *s.Spec.Replicas != 1 || s.Spec.PodManagementPolicy != "OrderedReady" || s.Spec.UpdateStrategy.Type != "RollingUpdate" || s.Spec.Retention.WhenDeleted != "Retain" || s.Spec.Retention.WhenScaled != "Retain" || len(s.Spec.Templates) == 0 {
			return "", fail
		}
		for key, value := range s.Spec.Selector.MatchLabels {
			actual, present := labels[key]
			if !present || actual != value {
				return "", fail
			}
		}
		return s.Metadata.ResourceVersion, nil
	}
	rv, err := readSet(podLabels)
	if err != nil {
		return fail
	}
	name2, uid2, labels2, err := readOwner()
	if err != nil || name2 != name || uid2 != ownerUID {
		return fail
	}
	rv2, err := readSet(labels2)
	if err != nil || rv2 != rv {
		return fail
	}
	return nil
}

func TestP08HelmPolicyObservation(t *testing.T) {
	if os.Getenv("P08_HELM_POLICY_OBSERVATION") != "1" {
		t.Skip("requires explicit private read-only target selection")
	}
	target, err := readP08ResourceTarget(os.Getenv("P08_RESOURCE_TARGET"))
	if err != nil {
		t.Fatal("private target selection unavailable")
	}
	if err := observeP08HelmPolicy(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	t.Log("selected backend policy observed; rollback, continuous no-surge, retention, mutations and P08 remain unproven")
}
