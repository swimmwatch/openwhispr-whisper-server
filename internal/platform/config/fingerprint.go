package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CanonicalFingerprint identifies the served declaration and GPU assignment.
// It neither accesses artifacts nor proves artifact integrity or authenticity.
func CanonicalFingerprint(settings Settings) (string, error) {
	if err := settings.Validate(); err != nil {
		return "", err
	}

	// Keep the wire shape closed: every field, including empty optional values,
	// is present and ordered independently of the public configuration encoding.
	type source struct {
		Kind          string `json:"kind"`
		URL           string `json:"url"`
		Path          string `json:"path"`
		SHA256        string `json:"sha256"`
		ExpectedBytes int64  `json:"expected_bytes"`
	}

	var document struct {
		Version int `json:"version"`
		Model   struct {
			ID     string `json:"id"`
			Source source `json:"source"`
		} `json:"model"`
		Backend struct {
			GPUDevice int `json:"gpu_device"`
		} `json:"backend"`
	}

	document.Version = 1
	document.Backend.GPUDevice = settings.Backend.GPUDevice
	for _, model := range settings.Models.Catalog {
		if model.ID == settings.Models.Served {
			s := model.Source
			document.Model.ID = model.ID
			document.Model.Source = source{s.Kind, s.URL, s.Path, s.SHA256, s.ExpectedBytes}
			break
		}
	}

	data, err := json.Marshal(document)
	if err != nil {
		return "", ErrInvalid
	}

	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
