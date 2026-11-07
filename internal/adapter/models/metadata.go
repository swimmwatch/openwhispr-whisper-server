package models

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

const (
	modelHeaderBytes              = 48
	modelMagic                    = 0x67676d6c
	modelParameterCount           = 11
	multilingualVocabularyMinimum = 51865
)

// ReadMetadata reads the fixed prefix consumed by the pinned whisper.cpp loader.
// Tensor layout and native/CUDA compatibility require an actual native load.
func ReadMetadata(ctx context.Context, source io.Reader) (domain.ModelMetadata, error) {
	var prefix [modelHeaderBytes]byte
	if _, err := io.ReadFull(contextReader{ctx, source}, prefix[:]); err != nil {
		return domain.ModelMetadata{}, provisioningFailure(err)
	}
	if binary.LittleEndian.Uint32(prefix[:4]) != modelMagic {
		return domain.ModelMetadata{}, provisioningFailure(errMetadata)
	}

	var params [modelParameterCount]int32
	for i := range params {
		value := binary.LittleEndian.Uint32(prefix[4+i*4:])
		if value > math.MaxInt32 || (i < modelParameterCount-1 && value == 0) {
			return domain.ModelMetadata{}, provisioningFailure(errMetadata)
		}
		params[i] = int32(value)
	}

	if params[2] != params[6] {
		return domain.ModelMetadata{}, provisioningFailure(errMetadata)
	}

	return domain.ModelMetadata{VocabularySize: params[0], Multilingual: params[0] >= multilingualVocabularyMinimum}, nil
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
