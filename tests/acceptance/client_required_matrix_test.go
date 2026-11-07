package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiredClientMatrixDoesNotLaunchDesktop(t *testing.T) {
	results := runClientCaseMatrix(t, t.Context(), "/test-root", "uv", "http://127.0.0.1:8080/v1", "/fixtures/jfk.wav", func(_ context.Context, _, executable string, args ...string) ([]byte, error) {
		if executable != "uv" {
			t.Error("required HTTP/SDK matrix attempted a Desktop/Electron process")
			return nil, errors.New("Desktop execution is not an authorized required client")
		}
		var audio, format string
		for i := 0; i+1 < len(args); i++ {
			switch args[i] {
			case "--audio":
				audio = filepath.Base(args[i+1])
			case "--response-format":
				format = args[i+1]
			}
		}
		for _, fixture := range clientCodecFixtures {
			if audio == fixture.Name {
				return json.Marshal(map[string]any{"success": true, "sdk": "openai-python", "sdk_version": "2.11.0", "response_format": format, "fixture_sha256": fixture.SHA256, "fixture_name": fixture.Name, "fixture_bytes": fixture.Bytes, "fixture_media_type": fixture.MediaType, "transcript_characters": 25, "transcript_sha256": strings.Repeat("a", 64), "qualification": "not_established"})
			}
		}
		t.Error("client process received an unknown audio fixture")
		return nil, errors.New("unknown fixture")
	})
	if len(results) != 2*len(clientCodecFixtures) {
		t.Fatal("required JSON/text fixture calls were omitted or an application result was added")
	}
	for _, result := range results {
		if !result.Succeeded || result.ObservedVersion != "2.11.0" || result.FullUI != "not_executed" {
			t.Fatal("required SDK results were not validated or claimed application execution")
		}
	}
}
