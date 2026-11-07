package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestP08GPUAllocationRequiresUniqueKnownOwnedBytes(t *testing.T) {
	for _, tc := range []struct {
		name, data   string
		commandError bool
		expected     uint64
		category     p08GPUAllocationError
	}{
		{name: "valid", data: "101, GPU-test, 10\n", expected: 10},
		{name: "other processes", data: "102, GPU-test, 40\n101, GPU-test, 10\n101, GPU-other, 20\n", expected: 10},
		{name: "query failure", commandError: true, category: p08GPUAllocationQuery},
		{name: "absent", data: "102, GPU-test, 10\n", category: p08GPUAllocationMissing},
		{name: "wrong GPU", data: "101, GPU-other, 10\n", category: p08GPUAllocationMissing},
		{name: "duplicate", data: "101, GPU-test, 10\n101, GPU-test, 10\n", category: p08GPUAllocationAmbiguous},
		{name: "unknown", data: "101, GPU-test, N/A\n", category: p08GPUAllocationParse},
		{name: "zero", data: "101, GPU-test, 0\n", category: p08GPUAllocationParse},
		{name: "malformed", data: "101, GPU-test\n", category: p08GPUAllocationParse},
		{name: "bad PID", data: "private, GPU-test, 10\n", category: p08GPUAllocationParse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fakeCUDAObserver("good")
			calls := 0
			a.observer.run = func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				if !strings.Contains(strings.Join(args, " "), "--query-compute-apps=pid,gpu_uuid,used_gpu_memory") {
					t.Fatal("unexpected GPU call")
				}
				if tc.commandError {
					return nil, errors.New("private identity")
				}
				return []byte(tc.data), nil
			}
			got, err := a.allocation(t.Context(), 101)
			if tc.category == "" {
				if err != nil || got != tc.expected || calls != 1 {
					t.Fatal("known allocation lost")
				}
				return
			}
			var category p08GPUAllocationError
			if got != 0 || !errors.As(err, &category) || category != tc.category || strings.Contains(err.Error(), "private") || calls != 1 {
				t.Fatal("unknown allocation or safe category lost")
			}
		})
	}
}
