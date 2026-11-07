package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSharedSDKRunnerRejectsProcessAndIdentityFailures(t *testing.T) {
	fixture := clientCodecFixtures[0]
	for _, tc := range []struct {
		name          string
		exitErr       bool
		wrongIdentity bool
		malformed     bool
		want          bool
	}{
		{"valid-fake-result-not-real-sdk-evidence", false, false, false, true},
		{"failed-exit", true, false, false, false},
		{"wrong-fixture", false, true, false, false},
		{"malformed-output", false, false, true, false},
	} {
		for _, format := range []string{"json", "text"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				calls := 0
				run := func(_ context.Context, root, executable string, args ...string) ([]byte, error) {
					calls++
					if root != "/root" || executable != "uv" || !strings.Contains(strings.Join(args, " "), "tests/acceptance/openai_client.py") || !strings.Contains(strings.Join(args, " "), "--response-format "+format) {
						t.Error("SDK runner command changed")
					}
					sha := fixture.SHA256
					if tc.wrongIdentity {
						sha = strings.Repeat("a", 64)
					}
					data, _ := json.Marshal(map[string]any{"success": true, "sdk": "openai-python", "sdk_version": "2.11.0", "response_format": format, "fixture_sha256": sha, "fixture_name": fixture.Name, "fixture_bytes": fixture.Bytes, "fixture_media_type": fixture.MediaType, "transcript_characters": 25, "transcript_sha256": strings.Repeat("b", 64), "qualification": "not_established"})
					if tc.malformed {
						data = []byte("invalid")
					}
					if tc.exitErr {
						return data, errors.New("private process diagnostic")
					}
					return data, nil
				}
				result := runSharedSDKFixture(t.Context(), "/root", "uv", "http://127.0.0.1:8080/v1", "/fixture", format, fixture, run)
				if calls != 1 || result.Succeeded != tc.want {
					t.Fatal("runner exit or fixture identity incorrectly accepted")
				}
			})
		}
	}
}
