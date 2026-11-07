package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestP08MemoryDecoderObservation(t *testing.T) {
	for _, tc := range []struct {
		name, output, category                       string
		changed, ownerError, commandError, wantError bool
		want                                         uint64
	}{
		{name: "descendants", output: "2:20:4096\n3:30:1024\nOK\n", want: 5120},
		{name: "measured absent", output: "OK\n", want: 0},
		{name: "missing report", output: "2:20:unknown\nOK\n", wantError: true, category: "decoder observation: parse"},
		{name: "missing terminator", output: "2:20:1\n", wantError: true, category: "decoder observation: parse"},
		{name: "changed owner", output: "OK\n", changed: true, wantError: true, category: "decoder observation: owner_after"},
		{name: "unknown owner", output: "OK\n", ownerError: true, wantError: true, category: "decoder observation: owner_before"},
		{name: "probe error", commandError: true, wantError: true, category: "decoder observation: probe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "context", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
			calls := 0
			original := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "api"}, {ID: "backend"}}, apiStart: 10, backendStart: 20, nativePID: 42, nativeStart: 30, generation: "g", fingerprint: "f"}
			owner := func(context.Context) (p08RepeatedIdentity, error) {
				calls++
				if tc.ownerError {
					return p08RepeatedIdentity{}, errors.New("private")
				}
				current := original
				if tc.changed && calls > 1 {
					current.apiStart++
				}
				return current, nil
			}
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "p08-memory-decoder") || !strings.HasSuffix(joined, "/proc 1 10") {
					t.Fatal("unscoped decoder probe")
				}
				if tc.commandError {
					return nil, errors.New("private")
				}
				return []byte(tc.output), nil
			}
			got, err := p08MemoryDecoderRSS(t.Context(), target, original, owner)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil && tc.category != "" && err.Error() != tc.category {
				t.Fatalf("wrong safe error category: %v", err)
			}
			if err == nil && (got == nil || *got != tc.want) {
				t.Fatalf("incorrect decoder observation: %v", got)
			}
			if err != nil && got != nil {
				t.Fatal("unknown observation became a value")
			}
		})
	}
}
