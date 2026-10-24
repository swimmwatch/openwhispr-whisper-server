package models

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func modelBytes(vocabulary int32) []byte {
	header := []int32{0x67676d6c, vocabulary, 1500, 512, 8, 6, 448, 512, 8, 6, 80, 1}
	var result bytes.Buffer
	for _, value := range header {
		_ = binary.Write(&result, binary.LittleEndian, value)
	}

	result.WriteString("small deterministic tensor fixture")
	return result.Bytes()
}

func TestModelMetadata(t *testing.T) {
	for _, vocabulary := range []int32{51864, 51865, 51866, 2147483647} {
		data := modelBytes(vocabulary)
		reader := bytes.NewReader(data)
		metadata, err := ReadMetadata(context.Background(), reader)
		if err != nil || metadata.VocabularySize != vocabulary || metadata.Multilingual != (vocabulary >= 51865) {
			t.Fatalf("vocabulary %d: %+v %v", vocabulary, metadata, err)
		}
		if reader.Len() != len(data)-48 {
			t.Fatalf("read beyond fixed prefix: %d", reader.Len())
		}
	}

	for length := 0; length < 48; length++ {
		if _, err := ReadMetadata(context.Background(), bytes.NewReader(modelBytes(51865)[:length])); err == nil {
			t.Fatalf("accepted truncated header: %d", length)
		}
	}

	for _, offset := range []int{0, 4, 8, 28, 44} {
		data := modelBytes(51865)
		binary.LittleEndian.PutUint32(data[offset:], 0xffffffff)
		if _, err := ReadMetadata(context.Background(), bytes.NewReader(data)); err == nil {
			t.Fatalf("accepted invalid header at %d", offset)
		}
	}

	data := modelBytes(51865)
	binary.LittleEndian.PutUint32(data[28:], 256)
	if _, err := ReadMetadata(context.Background(), bytes.NewReader(data)); err == nil {
		t.Fatal("accepted unequal text/audio state")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadMetadata(ctx, bytes.NewReader(modelBytes(51865))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
