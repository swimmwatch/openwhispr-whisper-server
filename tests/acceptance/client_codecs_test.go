package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
)

// These identities belong to the pinned public JFK fixture catalog. The SDK
// uploads the original encoded bytes: no extension-only MIME inference or client
// transcoding substitutes for the advertised API file families.
type clientCodecFixture struct {
	Name      string
	Bytes     int
	SHA256    string
	MediaType string
}

var clientCodecFixtures = []clientCodecFixture{
	{"jfk.wav", 352078, publicJFKIdentity, "audio/wav"},
	{"jfk.mp3", 133533, "ad6047a27423cd670bc9f39cce46b46054576f584449cff5fba5dc7c72737923", "audio/mpeg"},
	{"jfk.mpeg", 133533, "a50de786e9d2ad6623c2929143c0e1c163d2aee77e2fd3f677c2a0a4089da56d", "audio/mpeg"},
	{"jfk.mpga", 133533, "a50de786e9d2ad6623c2929143c0e1c163d2aee77e2fd3f677c2a0a4089da56d", "audio/mpeg"},
	{"jfk.mp4", 98499, "b67e593dbc28ee702613ad362318ae5313f020234f8aa3ca73616841fbaa4455", "audio/mp4"},
	{"jfk.m4a", 98479, "7928bc013b1a049153566a40116b62546d68534a006cba0973f8e222accdcb1b", "audio/mp4"},
	{"jfk.ogg", 54629, "bcf5ea9dbd0c51b2762144b30536ffdab95f10dfb10cbf4db74ada0639de8482", "audio/ogg"},
	{"jfk-opus.ogg", 91296, "fd5c2a62821cc88bf77ad9a0f7f5e644fe558d3d5537cd6665d088c5888cf66f", "audio/ogg"},
	{"jfk.webm", 94707, "0e8ad03325894c7a0ff4d66b0f3e7aff5d2cf9932bbc4c603729439335cf1ac7", "audio/webm"},
	{"jfk.flac", 203269, "448d17921a7f352519d3d73194fa72bfaf58189ba1749e9caf5c987b3cc11557", "audio/flac"},
}

func TestClientCodecEvidenceBindsRequestedFixture(t *testing.T) {
	for _, fixture := range clientCodecFixtures {
		for _, format := range []string{"json", "text"} {
			t.Run(fixture.Name+"/"+format, func(t *testing.T) {
				valid := map[string]any{
					"success": true, "sdk": "openai-python", "sdk_version": "2.11.0",
					"response_format": format, "fixture_name": fixture.Name,
					"fixture_sha256": fixture.SHA256, "fixture_bytes": fixture.Bytes,
					"fixture_media_type": fixture.MediaType, "transcript_characters": 25,
					"transcript_sha256": strings.Repeat("a", 64), "qualification": "not_established",
					"route": "PRIVATE_ENDPOINT_MARKER", "text": "PRIVATE_TRANSCRIPT_MARKER",
				}
				content, _ := json.Marshal(valid)
				result, err := readClientFixtureCaseResult(content, "openai", format, fixture)
				if err != nil || result.FixtureName != fixture.Name || result.FixtureSHA256 != fixture.SHA256 || result.FixtureBytes != fixture.Bytes || result.FixtureMediaType != fixture.MediaType || result.ResponseFormat != format {
					t.Fatal("actual SDK codec evidence was not retained")
				}
				safe, _ := json.Marshal(result)
				if strings.Contains(string(safe), "PRIVATE_") {
					t.Fatal("private client response escaped sanitized codec evidence")
				}
				for _, field := range []string{"fixture_name", "fixture_sha256", "fixture_bytes", "fixture_media_type", "response_format"} {
					original := valid[field]
					valid[field] = "incorrect"
					content, _ = json.Marshal(valid)
					if _, err := readClientFixtureCaseResult(content, "openai", format, fixture); err == nil {
						t.Fatalf("incorrect %s accepted as requested codec evidence", field)
					}
					delete(valid, field)
					content, _ = json.Marshal(valid)
					if _, err := readClientFixtureCaseResult(content, "openai", format, fixture); err == nil {
						t.Fatalf("missing %s accepted as requested codec evidence", field)
					}
					valid[field] = original
				}
			})
		}
	}
}
