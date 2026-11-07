package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func verifyArtifact(ctx context.Context, file *os.File, model config.Model) (domain.ModelArtifact, error) {
	hash := sha256.New()
	reader := io.TeeReader(contextReader{ctx, file}, hash)
	metadata, err := ReadMetadata(ctx, reader)
	if err != nil {
		return domain.ModelArtifact{}, errors.Join(errIntegrity, err)
	}

	n, err := io.Copy(io.Discard, reader)
	if err != nil {
		return domain.ModelArtifact{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.ModelArtifact{}, err
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if (model.Source.ExpectedBytes != 0 && n+modelHeaderBytes != model.Source.ExpectedBytes) ||
		(model.Source.SHA256 != "" && !strings.EqualFold(digest, model.Source.SHA256)) {
		return domain.ModelArtifact{}, errIntegrity
	}

	return domain.ModelArtifact{ID: model.ID, SHA256: digest, Metadata: metadata}, nil
}
