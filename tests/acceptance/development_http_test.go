package acceptance

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The parent owns target selection, readiness and loopback port forwarding.
// This entry reuses actual HTTP/SDK cases; it does not prove Kubernetes GPU,
// process ownership, isolation or complete published-candidate qualification.
func TestDevelopmentHTTP(t *testing.T) {
	if os.Getenv("P08_DEVELOPMENT_HTTP") != "1" {
		t.Skip("requires an explicitly authorized parent-owned development target")
	}
	endpoint := os.Getenv("P08_DEVELOPMENT_API_URL")
	fixtures := os.Getenv("P08_DEVELOPMENT_FIXTURES")
	evidence := os.Getenv("P08_DEVELOPMENT_EVIDENCE")
	model := os.Getenv("P08_DEVELOPMENT_MODEL")
	if endpoint == "" || fixtures == "" || evidence == "" || model == "" {
		t.Fatal("development API, public fixtures, private evidence and served model are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	// This existing consumer validates the credential-free loopback target and
	// pinned public fixture before making any actual SDK transcription request.
	t.Run("sdk", func(t *testing.T) {
		runClientCases(t, ctx, endpoint, filepath.Join(fixtures, "jfk.wav"), evidence)
	})
	if t.Failed() {
		return
	}
	client, err := NewPublicClient(endpoint, 120*time.Second, 1<<20)
	if err != nil {
		t.Fatal("cannot create bounded development HTTP client")
	}
	defer client.Close()
	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			probe, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			status, received := cacheProbeStatus(probe, client.client, strings.TrimRight(endpoint, "/")+path)
			if !received || status != http.StatusOK {
				t.Fatalf("development health failed: status=%d response_received=%t", status, received)
			}
		})
	}
	if t.Failed() {
		return
	}
	t.Run("public_contract", func(t *testing.T) {
		runPublicCases(t, ctx, client, fixtures, model)
	})
}
