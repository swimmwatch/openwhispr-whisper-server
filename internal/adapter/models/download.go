package models

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

const maxDownloadRedirects = 10

var errDownload = errors.New("model download failed")

func (c *artifactCache) download(ctx context.Context, model config.Model, caller *http.Client) (artifact Artifact, err error) {
	hash := strings.ToLower(model.Source.SHA256)
	partial := hash + ".partial-" + rand.Text()
	file, err := c.artifacts.OpenFile(partial, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Artifact{}, err
	}
	defer func() {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		if removeErr := c.artifacts.Remove(partial); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()

	// The transport remains caller-owned. In particular, do not close its idle
	// connections or change the original client's cookies/redirect policy.
	client := *caller
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Hostname() == "" {
			return errDownload
		}
		return nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, model.Source.URL, nil)
	if err != nil {
		return Artifact{}, err
	}

	response, err := client.Do(request)
	if err != nil {
		return Artifact{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Artifact{}, errDownload
	}

	digest := sha256.New()
	reader := contextReader{ctx, response.Body}
	writer := io.MultiWriter(file, digest)
	if err := copyModelBytes(writer, reader, model.Source.ExpectedBytes); err != nil {
		return Artifact{}, err
	}

	actual := hex.EncodeToString(digest.Sum(nil))
	if actual != hash {
		return Artifact{}, errIntegrity
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Artifact{}, err
	}

	metadata, err := ReadMetadata(ctx, file)
	if err != nil {
		return Artifact{}, err
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	if err := file.Sync(); err != nil {
		return Artifact{}, err
	}

	closeErr := file.Close()
	file = nil
	if closeErr != nil {
		return Artifact{}, closeErr
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	name := hash + ".bin"
	if err := c.artifacts.Rename(partial, name); err != nil {
		return Artifact{}, err
	}

	dir, err := c.artifacts.Open(".")
	if err != nil {
		return Artifact{}, err
	}

	syncErr := dir.Sync()
	closeErr = dir.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return Artifact{}, err
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	return Artifact{ID: model.ID, Path: filepath.Join(c.path, "artifacts", name), SHA256: actual, Metadata: metadata}, nil
}

func copyModelBytes(writer io.Writer, reader io.Reader, expected int64) error {
	if expected == 0 {
		_, err := io.Copy(writer, reader)
		return err
	}

	n, err := io.Copy(writer, io.LimitReader(reader, expected))
	if err != nil {
		return err
	}
	if n != expected {
		return errIntegrity
	}

	// Do not add one to an untrusted int64 expected length.
	var extra [1]byte
	if _, err := io.ReadFull(reader, extra[:]); err != io.EOF {
		if err != nil {
			return err
		}
		return errIntegrity
	}
	return nil
}
