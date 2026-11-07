package acceptance

import (
	"fmt"
	"strings"
	"testing"
)

func TestP08CacheSelectionDecodingFailsClosed(t *testing.T) {
	base := `{"stage":"%s","kubeconfig":"/private/config","context":"ctx","namespace":"ns","namespace_uid":"nuid","release":"r","backend_pod":"pod","backend_uid":"puid","container":"backend","claim":"cache","claim_uid":"cuid","artifact_path":"/models/artifact","sha256":"%s","size":12%s}`
	hash := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"before", fmt.Sprintf(base, "before", hash, ""), true},
		{"retained", fmt.Sprintf(base, "retained", hash, ""), true},
		{"after", fmt.Sprintf(base, "after", hash, ""), true},
		{"unknown stage", fmt.Sprintf(base, "offline", hash, ""), false},
		{"unknown field", fmt.Sprintf(base, "before", hash, `,"other":true`), false},
		{"duplicate field", fmt.Sprintf(base, "before", hash, `,"claim":"another"`), false},
		{"bad hash", fmt.Sprintf(base, "before", "abc", ""), false},
		{"trailing JSON", fmt.Sprintf(base, "before", hash, "") + `{}`, false},
		{"oversize", strings.Repeat(" ", 65537), false},
		{"missing identity", strings.Replace(fmt.Sprintf(base, "before", hash, ""), `"namespace_uid":"nuid"`, `"namespace_uid":""`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeKubernetesCacheEntry(strings.NewReader(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected decode result: %v", err)
			}
		})
	}
}
