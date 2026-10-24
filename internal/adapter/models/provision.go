package models

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

type Options struct {
	Models   config.Models
	CacheDir string
	Attempts int
	Timeout  time.Duration
}

type Artifact struct {
	ID       string
	Path     string
	SHA256   string
	Metadata Metadata
}

func Provision(ctx context.Context, opts Options, client *http.Client) (Artifact, error) {
	opts.Models.Catalog = slices.Clone(opts.Models.Catalog)
	opts.Models.DownloadSet = slices.Clone(opts.Models.DownloadSet)
	if opts.Models.Validate() != nil || !filepath.IsAbs(opts.CacheDir) || opts.Attempts <= 0 || opts.Timeout <= 0 ||
		client == nil {
		return Artifact{}, config.ErrInvalid
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Artifact{}, provisioningFailure(err)
	}

	byID := make(map[string]config.Model, len(opts.Models.Catalog))
	for _, model := range opts.Models.Catalog {
		byID[model.ID] = model
	}
	served := byID[opts.Models.Served]

	var cache *artifactCache
	if len(opts.Models.DownloadSet) != 0 || served.Source.Kind == "download" {
		var err error
		cache, err = openCache(opts.CacheDir)
		if err != nil {
			return Artifact{}, provisioningFailure(err)
		}
		defer cache.close()
	}

	var artifact Artifact
	for _, id := range opts.Models.DownloadSet {
		prepared, err := cache.obtain(ctx, byID[id], opts, client, true)
		if err != nil {
			return Artifact{}, provisioningFailure(err)
		}
		if id == served.ID {
			artifact = prepared
		}
	}

	if artifact.ID == served.ID {
		return artifact, nil
	}

	if served.Source.Kind == "prepared" {
		file, err := openRegular(nil, served.Source.Path)
		if err != nil {
			return Artifact{}, provisioningFailure(err)
		}
		defer file.Close()
		artifact, err = verifyArtifact(ctx, file, served)
		artifact.Path = served.Source.Path
		if err != nil {
			return Artifact{}, provisioningFailure(err)
		}
		return artifact, nil
	}

	artifact, err := cache.obtain(ctx, served, opts, client, false)
	if err != nil {
		return Artifact{}, provisioningFailure(err)
	}
	return artifact, nil
}

var errIntegrity = errors.New("model verification failed")
var errUnsafeFile = errors.New("unsafe model cache entry")
