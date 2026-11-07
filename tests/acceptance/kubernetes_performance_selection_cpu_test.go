package acceptance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKubernetesPerformancePrivateSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	data := []byte(`{"kubeconfig":"/private/kubeconfig","context":"selected","namespace":"test","namespace_uid":"uid","release":"test","api_pod":"api","backend_pod":"backend"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	target, err := readKubernetesPerformanceTarget(path)
	if err != nil || target.NamespaceUID != "uid" {
		t.Fatal("valid private selection rejected")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readKubernetesPerformanceTarget(path); err == nil {
		t.Fatal("publicly readable target selection accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"namespace":"test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readKubernetesPerformanceTarget(path); err == nil {
		t.Fatal("incomplete target accepted")
	}
}
