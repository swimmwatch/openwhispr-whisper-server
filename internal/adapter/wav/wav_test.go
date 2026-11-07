package wav

import (
	"bytes"
	"encoding/hex"
	"math"
	"strconv"
	"testing"
)

func TestCanonicalHeader(t *testing.T) {
	for _, tc := range []struct {
		dataBytes uint32
		encoded   string
	}{
		{2, "524946462600000057415645666d74201000000001000100803e0000007d0000020010006461746102000000"},
		{32000, "52494646247d000057415645666d74201000000001000100803e0000007d00000200100064617461007d0000"},
		{4294967258, "52494646feffffff57415645666d74201000000001000100803e0000007d00000200100064617461daffffff"},
	} {
		t.Run(strconv.FormatUint(uint64(tc.dataBytes), 10), func(t *testing.T) {
			want, err := hex.DecodeString(tc.encoded)
			if err != nil {
				t.Fatal(err)
			}
			header := Header(tc.dataBytes)
			if !bytes.Equal(header[:], want) {
				t.Fatalf("canonical PCM header changed: got %x, want %x", header, want)
			}
			if !ValidHeader(want, 44+int64(tc.dataBytes)) {
				t.Fatal("valid canonical wire header rejected")
			}
		})
	}
}

func TestValidHeaderRejectsMalformedInput(t *testing.T) {
	header, err := hex.DecodeString("524946462600000057415645666d74201000000001000100803e0000007d0000020010006461746102000000")
	if err != nil {
		t.Fatal(err)
	}
	// Every byte belongs to a fixed format field or a declared size. Mutating
	// one while retaining the same payload must not reach native inference.
	for i := range header {
		changed := bytes.Clone(header)
		changed[i] ^= 1
		if ValidHeader(changed, 46) {
			t.Errorf("malformed wire header accepted after changing byte %d", i)
		}
	}
	for _, size := range []int64{-1, 0, 44, 45, 47, 48, 4294967303, 4294967304, math.MaxInt64} {
		if ValidHeader(header, size) {
			t.Errorf("inconsistent, empty, unaligned or overflowing file size accepted: %d", size)
		}
	}
	for _, changed := range [][]byte{nil, header[:43], append(bytes.Clone(header), 0)} {
		if ValidHeader(changed, 46) {
			t.Errorf("invalid header length accepted: %d", len(changed))
		}
	}
}

func TestWAVLimits(t *testing.T) {
	for _, tc := range []struct {
		seconds   int
		fileBytes int64
		dataBytes int64
	}{
		{1, 32044, 32000},
		{2, 64044, 64000},
		{134217, 4294944044, 4294944000},
		{134218, 4294967303, 4294967258},
		{math.MaxInt, 4294967303, 4294967258},
	} {
		if got := MaxFileBytes(tc.seconds); got != tc.fileBytes {
			t.Errorf("file cap for %d seconds: got %d, want %d", tc.seconds, got, tc.fileBytes)
		}
		if got := MaxDataBytes(tc.seconds); got != tc.dataBytes {
			t.Errorf("PCM cap for %d seconds: got %d, want %d", tc.seconds, got, tc.dataBytes)
		}
	}
}
