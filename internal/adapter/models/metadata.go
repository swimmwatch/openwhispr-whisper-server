package models

import (
	"context"
	"encoding/binary"
	"errors"
	"io"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

const (
	modelHeaderBytes              = 48
	modelMagic                    = 0x67676d6c
	modelParameterCount           = 11
	multilingualVocabularyMinimum = 51865
)

type Metadata struct {
	VocabularySize int32
	Multilingual   bool
}

// ReadMetadata reads the fixed prefix consumed by the pinned whisper.cpp loader.
// Tensor layout and native/CUDA compatibility require an actual native load.
func ReadMetadata(ctx context.Context, source io.Reader) (Metadata, error) {
	var prefix [modelHeaderBytes]byte
	if _, err := io.ReadFull(contextReader{ctx, source}, prefix[:]); err != nil {
		return Metadata{}, provisioningFailure(err)
	}
	if binary.LittleEndian.Uint32(prefix[:4]) != modelMagic {
		return Metadata{}, provisioningFailure(errMetadata)
	}

	var params [modelParameterCount]int32
	for i := range params {
		params[i] = int32(binary.LittleEndian.Uint32(prefix[4+i*4:]))
		if params[i] < 0 || (i < modelParameterCount-1 && params[i] == 0) {
			return Metadata{}, provisioningFailure(errMetadata)
		}
	}

	if params[2] != params[6] {
		return Metadata{}, provisioningFailure(errMetadata)
	}

	return Metadata{VocabularySize: params[0], Multilingual: params[0] >= multilingualVocabularyMinimum}, nil
}

var errMetadata = errors.New("invalid model header")

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}

func provisioningFailure(cause error) error {
	return &domain.Failure{Kind: domain.ProvisioningFailed, Cause: cause}
}
