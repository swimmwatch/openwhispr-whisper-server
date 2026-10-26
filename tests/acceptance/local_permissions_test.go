package acceptance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestPreparedModelCopyPreservesReadonlyConsumerAccess(t *testing.T) {
	if os.Getenv("P07_PERMISSION_OBSERVATION_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreparedModelCopyPreservesReadonlyConsumerAccess$")
		command.Env = append(os.Environ(), "P07_PERMISSION_OBSERVATION_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("owned restrictive-umask model copy failed: %s", output)
		}
		return
	}
	// Umask is process-global: isolate it in this owned test executable rather
	// than changing the shared test process or the operator's environment.
	syscall.Umask(0077)
	dir := t.TempDir()
	data := []byte("public synthetic prepared model fixture")
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "copied-model")
	if os.WriteFile(source, data, 0600) != nil {
		t.Fatal("cannot create owned model fixture")
	}
	digest := sha256.Sum256(data)
	copyPreparedModel(t, source, destination, config.Source{ExpectedBytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])})
	copied, err := os.ReadFile(destination)
	if err != nil || string(copied) != string(data) {
		t.Fatal("prepared model copy changed its public content")
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm()&0044 != 0044 || info.Mode().Perm()&0022 != 0 {
		t.Fatal("prepared model copy denies the separate non-root readonly consumer or permits foreign writes")
	}
	private, err := os.Stat(dir)
	if err != nil || private.Mode().Perm() != 0700 {
		t.Fatal("owned private evidence parent lost its privacy boundary")
	}
}
