package models

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestPreparedArtifactExpectedSize(t *testing.T) {
	data := modelBytes(51865)
	path := filepath.Join(t.TempDir(), "PRIVATE-prepared.bin")
	if err := os.WriteFile(path, data, 0444); err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []struct {
		name string
		size int64
	}{
		{
			name: "exact",
			size: int64(len(data)),
		},
		{
			name: "short",
			size: int64(len(data) - 1),
		},
		{
			name: "long",
			size: int64(len(data) + 1),
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			opts := options(t, "https://example.org/PRIVATE", data)
			opts.Models.DownloadSet = nil
			opts.Models.Catalog[0].Source = config.Source{
				Kind:          "prepared",
				Path:          path,
				SHA256:        checksum(data),
				ExpectedBytes: scenario.size,
			}

			artifact, err := Provision(context.Background(), opts, http.DefaultClient)
			if scenario.name == "exact" {
				if err != nil || artifact.SHA256 != checksum(data) || artifact.Path != path {
					t.Fatalf("valid prepared size/checksum rejected: %+v %v", artifact, err)
				}
			} else {
				requireFailure(t, err)
				if !errors.Is(err, errIntegrity) || artifact != (Artifact{}) {
					t.Fatalf("size mismatch returned an artifact or lost integrity cause: %+v %v", artifact, err)
				}
			}

			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("verification changed prepared bytes", err)
			}
			if _, err := os.Stat(opts.CacheDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("prepared-only verification created a cache", err)
			}
		})
	}
}

func TestOfflineCacheCleansOnlyStaleArtifactPartials(t *testing.T) {
	data := modelBytes(51865)
	opts := options(t, "https://example.org/PRIVATE", data)
	opts.Models.DownloadSet = nil
	final := writeCache(t, opts, data)
	dir := filepath.Dir(final)
	hash := checksum(data)
	stale := filepath.Join(dir, hash+".partial-stale")
	unrelated := filepath.Join(dir, strings.Repeat("a", 64)+".partial-unrelated")
	ownedDirectory := filepath.Join(dir, hash+".partial-directory")
	outside := filepath.Join(t.TempDir(), "PRIVATE-sentinel")
	link := filepath.Join(dir, hash+".partial-symlink")

	for _, path := range []string{stale, unrelated, outside} {
		if err := os.WriteFile(path, []byte("PRIVATE sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(ownedDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	artifact, err := Provision(context.Background(), opts, http.DefaultClient)
	if err != nil || artifact.Path != final {
		t.Fatalf("offline recovery failed: %+v %v", artifact, err)
	}

	for _, removed := range []string{stale, link} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stale partial survived", err)
		}
	}

	for _, preserved := range []string{unrelated, outside} {
		got, err := os.ReadFile(preserved)
		if err != nil || string(got) != "PRIVATE sentinel" {
			t.Fatal("cleanup changed an unowned file", err)
		}
	}
	if info, err := os.Stat(ownedDirectory); err != nil || !info.IsDir() {
		t.Fatal("partial cleanup removed a directory", err)
	}
}
