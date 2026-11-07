package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestP08CUDAEnumerationRequiresCompleteFraming(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		ok         bool
	}{
		{"empty complete", "OK\nEND\n", true}, {"complete", "OK\n101\n102\nEND\n", true}, {"truncated after line", "OK\n101\n", false},
		{"partial missing terminator", "OK\n101", false}, {"missing frame", "101\n", false}, {"duplicate", "OK\n101\n101\nEND\n", false}, {"bad pid", "OK\nprivate\nEND\n", false}, {"error with PID", "ERROR:access\n101\n", false}, {"known guard", "ERROR:guard\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p08CUDAEnumeration([]byte(tc.data))
			if (err == nil) != tc.ok || err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("enumeration framing accepted unavailable evidence")
			}
		})
	}
}

func TestP08CUDAIdentityFailureCategories(t *testing.T) {
	for _, tc := range []struct{ stage, needle string }{
		{"namespace", "get namespace"}, {"driver", "get pod driver"}, {"pod", "get pod backend"}, {"gpu", "--query-gpu=uuid"}, {"enumeration", "perl -e"}, {"cgroup", "/cgroup"}, {"stat", "/stat"}, {"health", "/health"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			a := fakeCUDAObserver("good")
			realRun := a.observer.run
			a.observer.run = func(ctx context.Context, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), tc.needle) {
					return nil, errors.New("private PID and address")
				}
				return realRun(ctx, args...)
			}
			_, err := a.identity(t.Context())
			if err == nil || err.Error() != "CUDA identity: "+tc.stage || strings.Contains(err.Error(), "private") {
				t.Fatal("safe failure category was not preserved")
			}
		})
	}
}

func TestP08CUDAIdentityRejectsMissingAndAmbiguousOwners(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{{"wrong-pod", "native_missing"}, {"multiple", "native_ambiguous"}} {
		t.Run(tc.mode, func(t *testing.T) {
			_, err := fakeCUDAObserver(tc.mode).identity(t.Context())
			if err == nil || err.Error() != "CUDA identity: "+tc.want {
				t.Fatal("missing or ambiguous identity was accepted")
			}
		})
	}
}
