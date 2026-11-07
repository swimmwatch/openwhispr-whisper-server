package acceptance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestP08SharedHTTPSelectionRejectsIncompleteOrUnprivateInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selection.json")
	for _, tc := range []struct {
		name, content string
		mode          os.FileMode
		valid         bool
	}{
		{"valid", `{"resource_target":"/private/target","api_image_id":"api-id","backend_image_id":"backend-id"}`, 0600, true},
		{"missing-image", `{"resource_target":"/private/target","api_image_id":"api-id"}`, 0600, false},
		{"relative-target", `{"resource_target":"target","api_image_id":"api-id","backend_image_id":"backend-id"}`, 0600, false},
		{"unknown-field", `{"resource_target":"/private/target","api_image_id":"api-id","backend_image_id":"backend-id","extra":true}`, 0600, false},
		{"not-private", `{"resource_target":"/private/target","api_image_id":"api-id","backend_image_id":"backend-id"}`, 0644, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err := readP08SharedHTTPSelection(path)
			if (err == nil) != tc.valid {
				t.Fatal("private selection acceptance mismatch")
			}
		})
	}
	if _, err := readP08SharedHTTPSelection("relative.json"); err == nil {
		t.Fatal("relative selection accepted")
	}
}
