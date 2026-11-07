package integration

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildContextExcludesPrivateMedia(t *testing.T) {
	imageTests(t)

	context := t.TempDir()
	if err := os.WriteFile(filepath.Join(context, ".dockerignore"), artifact(t, ".dockerignore"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(context, "Dockerfile"), []byte("FROM scratch\nCOPY . /context/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inputs := filepath.Join(context, "internal", "fixtures")
	if err := os.MkdirAll(inputs, 0700); err != nil {
		t.Fatal(err)
	}
	for _, extension := range []string{
		"wav", "mp3", "mp4", "mpeg", "mpga", "m4a", "ogg", "flac", "webm", "opus",
		"WAV", "MP3", "MP4", "MPEG", "MPGA", "M4A", "OGG", "FLAC", "WEBM", "OPUS",
		"WaV", "MpEg", "OpUs",
	} {
		if err := os.WriteFile(filepath.Join(inputs, "private."+extension), []byte(privateMarker), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const source = "package fixture\n"
	if err := os.WriteFile(filepath.Join(inputs, "source.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}

	output := t.TempDir()
	docker(t, "buildx", "build", "--network=none", "--no-cache", "--progress=plain",
		"--output=type=local,dest="+output, context)
	data, err := os.ReadFile(filepath.Join(output, "context", "internal", "fixtures", "source.go"))
	if err != nil || string(data) != source {
		t.Fatalf("build context lost required source input: %v", err)
	}
	if err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(privateMarker)) {
			t.Errorf("private media bytes reached the build output: %s", filepath.Base(path))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
