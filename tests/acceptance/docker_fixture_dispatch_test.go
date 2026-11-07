package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDockerFixtureDispatchFitsOwnedCommandBudget(t *testing.T) {
	parentRaceOptions := os.Getenv("GORACE")
	target, _ := dockerFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for i := 0; i < 4; i++ {
		started := time.Now()
		data, err := target.run(ctx, "CPU fixture inspection", false, "inspect", dockerBackendID)
		t.Logf("completed CPU fixture command %d in %s", i+1, time.Since(started))
		if err != nil {
			t.Fatal("synchronous CPU fixture exhausted its owner budget after producing its response", err)
		}
		var state DockerInspection
		if json.Unmarshal(data, &state) != nil || state.ID != dockerBackendID || !state.Running {
			t.Fatal("CPU fixture did not preserve its actual inspection response")
		}
	}
	if os.Getenv("GORACE") != parentRaceOptions {
		t.Fatal("child fixture changed the parent's race options")
	}
}
