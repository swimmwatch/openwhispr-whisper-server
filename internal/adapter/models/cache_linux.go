package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"golang.org/x/sys/unix"
)

const (
	cacheLockPollInterval   = 10 * time.Millisecond
	downloadRetryInterval   = 100 * time.Millisecond
	cacheDirectoryBatchSize = 128
)

type artifactCache struct {
	root      *os.Root
	artifacts *os.Root
	locks     *os.Root
	path      string
}

func openCache(path string) (*artifactCache, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errUnsafeFile
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}

	cache := &artifactCache{root: root, path: path}
	for _, target := range []struct {
		name string
		root **os.Root
	}{{"artifacts", &cache.artifacts}, {"locks", &cache.locks}} {
		*target.root, err = openCacheDirectory(root, target.name)
		if err != nil {
			cache.close()
			return nil, err
		}
	}

	return cache, nil
}

func openCacheDirectory(root *os.Root, name string) (*os.Root, error) {
	if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errUnsafeFile
	}
	return root.OpenRoot(name)
}

func (c *artifactCache) close() {
	if c.artifacts != nil {
		_ = c.artifacts.Close()
	}
	if c.locks != nil {
		_ = c.locks.Close()
	}
	_ = c.root.Close()
}

func openRegular(root *os.Root, name string) (*os.File, error) {
	flags := os.O_RDONLY | unix.O_NONBLOCK
	var file *os.File
	var err error
	if root == nil {
		file, err = os.OpenFile(name, flags, 0)
	} else {
		file, err = root.OpenFile(name, flags|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errUnsafeFile
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (c *artifactCache) lock(ctx context.Context, hash string) (*os.File, error) {
	file, err := c.locks.OpenFile(hash+".lock", os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errUnsafeFile
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	for {
		if err = ctx.Err(); err != nil {
			break
		}
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			break
		}
		if err = pause(ctx, cacheLockPollInterval); err != nil {
			break
		}
	}

	_ = file.Close()
	return nil, err
}

func (c *artifactCache) obtain(ctx context.Context, model config.Model, opts Options, client *http.Client, download bool) (Artifact, error) {
	hash := strings.ToLower(model.Source.SHA256)
	lock, err := c.lock(ctx, hash)
	if err != nil {
		return Artifact{}, err
	}
	defer lock.Close() // Closing the last descriptor releases flock; never unlink it.

	if err := c.cleanPartials(ctx, hash); err != nil {
		return Artifact{}, err
	}

	name := hash + ".bin"
	file, err := openRegular(c.artifacts, name)
	if err == nil {
		artifact, verifyErr := verifyArtifact(ctx, file, model)
		_ = file.Close()
		if verifyErr == nil {
			artifact.Path = filepath.Join(c.path, "artifacts", name)
			return artifact, nil
		}
		if ctx.Err() != nil {
			return Artifact{}, ctx.Err()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Artifact{}, err
	}

	if !download {
		return Artifact{}, errIntegrity
	}

	for attempt := 0; attempt < opts.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Artifact{}, err
		}
		artifact, downloadErr := c.download(ctx, model, client)
		if downloadErr == nil {
			return artifact, nil
		}
		err = downloadErr
		if attempt+1 < opts.Attempts {
			if err := pause(ctx, downloadRetryInterval); err != nil {
				return Artifact{}, err
			}
		}
	}

	if ctx.Err() != nil {
		return Artifact{}, ctx.Err()
	}
	return Artifact{}, err
}

func (c *artifactCache) cleanPartials(ctx context.Context, hash string) error {
	dir, err := c.artifacts.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		entries, err := dir.ReadDir(cacheDirectoryBatchSize)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), hash+".partial-") && !entry.IsDir() {
				if removeErr := c.artifacts.Remove(entry.Name()); removeErr != nil {
					return removeErr
				}
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
