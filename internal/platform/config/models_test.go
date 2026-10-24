package config

import (
	"errors"
	"testing"
)

func TestModelSets(t *testing.T) {
	prepared := `{"models":{"catalog":[{"id":"base","source":{"kind":"prepared","path":"/models/a.bin"}}],"served":"base","downloadSet":["base"]}}`
	for _, flags := range [][]string{nil, {"--download-set-json=[]"}} {
		_, err := Load(append([]string{"--config", configFile(t, prepared)}, flags...), nil)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("prepared download entry accepted, overrides %v: %v", flags, err)
		}
	}

	for _, body := range []string{
		`{"models":{"catalog":[{"id":"base","source":{"kind":"prepared","path":"/models/a.bin"}}],"served":"missing","downloadSet":[]}}`,
		`{"models":{"downloadSet":["missing"]}}`,
	} {
		if _, err := Load([]string{"--config", configFile(t, body)}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid reference accepted: %v", err)
		}
	}

	if _, err := Load([]string{"--download-set-json=[]"}, nil); err != nil {
		t.Fatalf("served download may be outside download set: %v", err)
	}

	if _, err := Load([]string{
		"--catalog-json=[{\"id\":\"base\",\"source\":{\"kind\":\"prepared\",\"path\":\"/models/a.bin\"}}]",
		"--download-set-json=[]",
	}, nil); err != nil {
		t.Fatalf("partial sources can supply relationships separately: %v", err)
	}
}
