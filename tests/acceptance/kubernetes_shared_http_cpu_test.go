package acceptance

import (
	"context"
	"errors"
	"testing"
)

func TestP08SharedHTTPIdentityBracket(t *testing.T) {
	base := p08HeapOwner{Containers: [2]p08HeapContainer{{Image: "api-image"}, {Image: "backend-image"}}}
	base.Process.containers = [2]p08ContainerIdentity{{ID: "api"}, {ID: "backend"}}
	for _, tc := range []struct {
		name                string
		before, after       p08HeapOwner
		observeErrAt        int
		workOK              bool
		wantCalls, wantWork int
		wantOK              bool
	}{
		{"stable", base, base, 0, true, 2, 1, true},
		{"initial-image-mismatch", p08HeapOwner{Containers: [2]p08HeapContainer{{Image: "wrong"}, {Image: "backend-image"}}}, base, 0, true, 1, 0, false},
		{"initial-unavailable", base, base, 1, true, 1, 0, false},
		{"final-unavailable", base, base, 2, true, 2, 1, false},
		{"work-failed-still-verifies", base, base, 0, false, 2, 1, false},
		{"container-changed", base, p08HeapOwner{Containers: base.Containers}, 0, true, 2, 1, false},
		{"image-changed", base, p08HeapOwner{Process: base.Process, Containers: [2]p08HeapContainer{{Image: "other"}, {Image: "backend-image"}}}, 0, true, 2, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, work := 0, 0
			observe := func(context.Context) (p08HeapOwner, error) {
				calls++
				if calls == tc.observeErrAt {
					return p08HeapOwner{}, errors.New("unavailable")
				}
				if calls == 1 {
					return tc.before, nil
				}
				return tc.after, nil
			}
			err := runP08BoundHTTPSlice(t.Context(), [2]string{"api-image", "backend-image"}, observe, func() bool { work++; return tc.workOK })
			if (err == nil) != tc.wantOK || calls != tc.wantCalls || work != tc.wantWork {
				t.Fatalf("identity bracket outcome: failed=%t observations=%d workloads=%d", err != nil, calls, work)
			}
		})
	}
}
