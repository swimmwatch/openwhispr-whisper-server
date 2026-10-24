package config

import "testing"

func fingerprintSettings(t *testing.T) Settings {
	t.Helper()
	c, err := Load([]string{"--download-set-json=[]"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	c.Models.Catalog = []Model{{ID: "base", Source: Source{Kind: "prepared", Path: "/models/a.bin"}}}
	return c
}

func TestCanonicalFingerprint(t *testing.T) {
	c := fingerprintSettings(t)
	want := "bc7ffae51e3da44601328b3e545adc57538efc7526a44040ad42c607b7b80714"
	got, err := CanonicalFingerprint(c)
	if err != nil || got != want {
		t.Fatalf("canonical vector: %q %v", got, err)
	}

	for _, change := range []func(*Settings){
		func(c *Settings) {
			c.Backend.GPUDevice++
		},
		func(c *Settings) {
			c.Models.Catalog[0].Source.Path = "/models/b.bin"
		},
		func(c *Settings) {
			c.Models.Catalog[0].Source.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		},
		func(c *Settings) {
			c.Models.Catalog[0].Source.ExpectedBytes = 48
		},
		func(c *Settings) {
			c.Models.Catalog[0].ID = "other"
			c.Models.Served = "other"
		},
		func(c *Settings) {
			c.Models.Catalog[0].Source = Source{
				Kind:   "download",
				URL:    "https://example.org/a.bin",
				SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}
		},
	} {
		other := fingerprintSettings(t)
		change(&other)
		hash, err := CanonicalFingerprint(other)
		if err != nil || hash == got {
			t.Fatalf("served configuration not included: %q %v", hash, err)
		}
	}

	other := fingerprintSettings(t)
	other.Models.Catalog = append([]Model{
		{
			ID: "extra",
			Source: Source{
				Kind:   "download",
				URL:    "https://example.org/extra",
				SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}, other.Models.Catalog...)
	other.Models.DownloadSet = []string{"extra"}
	other.Storage.CacheDir = "/another-cache"
	other.API.Port++
	other.API.BackendURL = "http://different:9000"
	other.Backend.Port++
	other.Backend.NativePort++
	other.Logging.Level = "debug"
	other.Queue.WaitingCapacity++
	other.Limits.AudioBytes++
	other.Timeouts.OperationSeconds++
	other.Downloads.Attempts++
	if hash, err := CanonicalFingerprint(other); err != nil || hash != got {
		t.Fatalf("excluded configuration changed fingerprint: %q %v", hash, err)
	}

	c.Models.Served = "missing"
	if _, err := CanonicalFingerprint(c); err != ErrInvalid {
		t.Fatalf("invalid settings accepted: %v", err)
	}
}
