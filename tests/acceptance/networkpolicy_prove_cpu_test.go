package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type proveFake struct {
	p                 provePolicy
	outage            bool
	beforeFence       func()
	ambiguousFence    bool
	ambiguousMutation bool
}

func fakeAnnotationKey(path string) string {
	key := strings.TrimPrefix(path, "/metadata/annotations/")
	return strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
}
func (f *proveFake) read(context.Context) (provePolicy, error) {
	if f.outage {
		return provePolicy{}, errors.New("offline")
	}
	return f.p, nil
}
func (f *proveFake) patch(ctx context.Context, ops []proveOperation) error {
	if f.outage {
		return errors.New("offline")
	}
	if f.beforeFence != nil && len(ops) > 2 && strings.HasPrefix(ops[2].Path, "/metadata/annotations") {
		fn := f.beforeFence
		f.beforeFence = nil
		fn()
	}
	p := f.p
	sel, _ := proveSelector(p)
	for _, op := range ops {
		if op.Op != "test" {
			continue
		}
		var actual any
		switch op.Path {
		case "/metadata/uid":
			actual = p.Metadata.UID
		case "/metadata/resourceVersion":
			actual = p.Metadata.ResourceVersion
		case proveSelectorPath:
			actual = sel
		default:
			actual = p.Metadata.Annotations[fakeAnnotationKey(op.Path)]
		}
		if !reflect.DeepEqual(actual, op.Value) {
			return errors.New("conflict")
		}
	}
	for _, op := range ops {
		switch {
		case op.Op == "replace" && op.Path == proveSelectorPath:
			f.p.Spec.Ingress[0].From[0].PodSelector.MatchLabels = proveCopy(op.Value.(map[string]string))
		case op.Op == "replace" && strings.HasPrefix(op.Path, proveSelectorPath+"/"):
			m := proveCopy(sel)
			m[proveOrdinalKey] = op.Value.(string)
			f.p.Spec.Ingress[0].From[0].PodSelector.MatchLabels = m
		case op.Op == "add" && op.Path == "/metadata/annotations":
			f.p.Metadata.Annotations = proveCopy(op.Value.(map[string]string))
		case op.Op == "add" && strings.HasPrefix(op.Path, "/metadata/annotations/"):
			m := proveCopy(f.p.Metadata.Annotations)
			m[fakeAnnotationKey(op.Path)] = op.Value.(string)
			f.p.Metadata.Annotations = m
		case op.Op == "remove":
			m := proveCopy(f.p.Metadata.Annotations)
			delete(m, fakeAnnotationKey(op.Path))
			f.p.Metadata.Annotations = m
		}
	}
	n, _ := strconv.Atoi(f.p.Metadata.ResourceVersion)
	f.p.Metadata.ResourceVersion = strconv.Itoa(n + 1)
	for _, op := range ops {
		if op.Op == "add" && strings.Contains(op.Path, "fence") && f.ambiguousFence {
			f.ambiguousFence = false
			return errors.New("ambiguous fence response")
		}
		if op.Op == "add" && strings.Contains(op.Path, "mutation-owner") && f.ambiguousMutation {
			f.ambiguousMutation = false
			return errors.New("ambiguous mutation response")
		}
	}
	return nil
}
func proveTestMarkers() proveMarkers {
	return proveMarkers{OwnerAnnotation: "acceptance.example/mutation-owner", OwnerToken: "owner-token", FenceAnnotation: "acceptance.example/fence", FenceToken: "fence-token"}
}
func proveFixture() (*proveFake, map[string]string, map[string]string) {
	f := &proveFake{}
	f.p.Metadata.UID = "uid"
	f.p.Metadata.ResourceVersion = "1"
	f.p.Metadata.Annotations = map[string]string{"owner": "retained"}
	original := map[string]string{"app": "api", proveOrdinalKey: "api-0"}
	changed := proveCopy(original)
	changed[proveOrdinalKey] = "api-1"
	// Initialize via JSON to avoid coupling the fake to the anonymous nested types.
	data := []byte(`{"metadata":{"uid":"uid","resourceVersion":"1","annotations":{"owner":"retained"}},"spec":{"ingress":[{"from":[{"podSelector":{"matchLabels":{"app":"api","statefulset.kubernetes.io/pod-name":"api-0"}}}]}]}}`)
	if json.Unmarshal(data, &f.p) != nil {
		panic("invalid fixture")
	}
	return f, original, changed
}
func TestNetworkPolicyFenceRace(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(strconv.FormatBool(first), func(t *testing.T) {
			f, original, changed := proveFixture()
			baseline := f.p
			mutation := func() error {
				return proveMutation(context.Background(), f, baseline, original, changed, proveTestMarkers())
			}
			if first {
				f.beforeFence = func() {
					if mutation() != nil {
						t.Error("late mutation did not win race")
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := proveRestore(ctx, f, "uid", original, changed, proveTestMarkers()); err != nil {
				t.Fatal(err)
			}
			if !first && mutation() == nil {
				t.Fatal("late mutation escaped fence")
			}
			actual, _ := proveSelector(f.p)
			if !reflect.DeepEqual(actual, original) || f.p.Metadata.Annotations["owner"] != "retained" || f.p.Metadata.Annotations["acceptance.example/fence"] != "" || f.p.Metadata.Annotations["acceptance.example/mutation-owner"] != "" {
				t.Fatal("restore did not preserve owned state")
			}
		})
	}
}
func TestNetworkPolicyCleanupFailures(t *testing.T) {
	t.Run("uid", func(t *testing.T) {
		f, o, c := proveFixture()
		f.p.Metadata.UID = "new"
		if proveRestore(context.Background(), f, "uid", o, c, proveTestMarkers()) == nil {
			t.Fatal("accepted replacement policy")
		}
	})
	t.Run("foreign", func(t *testing.T) {
		f, o, c := proveFixture()
		f.beforeFence = func() {
			f.p.Spec.Ingress[0].From[0].PodSelector.MatchLabels = map[string]string{"app": "foreign"}
			f.p.Metadata.ResourceVersion = "2"
		}
		if proveRestore(context.Background(), f, "uid", o, c, proveTestMarkers()) == nil {
			t.Fatal("accepted foreign selector")
		}
		s, _ := proveSelector(f.p)
		if s["app"] != "foreign" {
			t.Fatal("foreign selector overwritten")
		}
	})
	t.Run("foreign identical selector without ownership", func(t *testing.T) {
		f, o, c := proveFixture()
		f.beforeFence = func() {
			f.p.Spec.Ingress[0].From[0].PodSelector.MatchLabels = proveCopy(c)
			f.p.Metadata.ResourceVersion = "2"
		}
		if proveRestore(context.Background(), f, "uid", o, c, proveTestMarkers()) == nil {
			t.Fatal("accepted foreign selector without ownership")
		}
		s, _ := proveSelector(f.p)
		if !reflect.DeepEqual(s, c) {
			t.Fatal("foreign identical selector overwritten")
		}
	})
	t.Run("outage", func(t *testing.T) {
		f, o, c := proveFixture()
		f.outage = true
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if proveRestore(ctx, f, "uid", o, c, proveTestMarkers()) == nil {
			t.Fatal("outage accepted")
		}
	})
}
func TestNetworkPolicyAmbiguousAndIdempotentRecovery(t *testing.T) {
	f, original, changed := proveFixture()
	baseline := f.p
	markers := proveTestMarkers()
	f.ambiguousMutation = true
	if proveMutation(context.Background(), f, baseline, original, changed, markers) == nil {
		t.Fatal("mutation response should be ambiguous")
	}
	f.ambiguousFence = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proveRestore(ctx, f, "uid", original, changed, markers); err != nil {
		t.Fatal(err)
	}
	if err := proveRestore(ctx, f, "uid", original, changed, markers); err != nil {
		t.Fatal(err)
	}
	selector, _ := proveSelector(f.p)
	if !reflect.DeepEqual(selector, original) || f.p.Metadata.Annotations[markers.OwnerAnnotation] != "" || f.p.Metadata.Annotations[markers.FenceAnnotation] != "" {
		t.Fatal("recovery was not idempotent")
	}
	if proveMutation(context.Background(), f, baseline, original, changed, markers) == nil {
		t.Fatal("late mutation escaped fence")
	}
}

func TestNetworkPolicyWidenedControlFailure(t *testing.T) {
	probe := func(_ context.Context, _, _, ip string, _ int, _ string, allowed bool) error {
		if ip == "control" {
			return errors.New("control failure")
		}
		if allowed {
			return nil
		}
		return errors.New("negative rejected")
	}
	if proveWidened(context.Background(), probe, "ns", "pod", "control", "backend", 1, 2) == nil {
		t.Fatal("control failure treated as detected widening")
	}
}
