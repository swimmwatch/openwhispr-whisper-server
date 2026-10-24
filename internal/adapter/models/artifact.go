package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func verifyArtifact(ctx context.Context, file *os.File, model config.Model) (Artifact, error) {
	hash := sha256.New()
	reader := io.TeeReader(contextReader{ctx, file}, hash)
	metadata, err := ReadMetadata(ctx, reader)
	if err != nil {
		return Artifact{}, errors.Join(errIntegrity, err)
	}

	n, err := io.Copy(io.Discard, reader)
	if err != nil {
		return Artifact{}, err
	}
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if (model.Source.ExpectedBytes != 0 && n+modelHeaderBytes != model.Source.ExpectedBytes) ||
		(model.Source.SHA256 != "" && !strings.EqualFold(digest, model.Source.SHA256)) {
		return Artifact{}, errIntegrity
	}

	return Artifact{ID: model.ID, SHA256: digest, Metadata: metadata}, nil
}
