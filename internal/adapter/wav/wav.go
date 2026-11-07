// Package wav owns the canonical mono 16 kHz signed 16-bit PCM wire format.
// It is not a general WAV parser: decoder output and private inference use this
// one exact header, without extra chunks or format negotiation.
package wav

import (
	"bytes"
	"encoding/binary"
	"math"
)

const (
	HeaderBytes = 44
	SampleRate  = 16000
	Channels    = 1
	FrameBytes  = Channels * bitsPerSample / 8
	MinBytes    = HeaderBytes + FrameBytes

	bitsPerSample    = 16
	pcmFormat        = 1
	formatChunkBytes = 16
	riffChunkHeader  = 8
	riffSizeOverhead = HeaderBytes - riffChunkHeader
	bytesPerSecond   = SampleRate * FrameBytes
	maxRIFFFileBytes = int64(math.MaxUint32) + riffChunkHeader
)

// Header encodes a PCM payload whose size has already been bounded and aligned
// by its owner. Native-specific silence padding is not part of this format.
func Header(dataBytes uint32) [HeaderBytes]byte {
	var h [HeaderBytes]byte
	copy(h[:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], dataBytes+riffSizeOverhead)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], formatChunkBytes)
	binary.LittleEndian.PutUint16(h[20:], pcmFormat)
	binary.LittleEndian.PutUint16(h[22:], Channels)
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], bytesPerSecond)
	binary.LittleEndian.PutUint16(h[32:], FrameBytes)
	binary.LittleEndian.PutUint16(h[34:], bitsPerSample)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], dataBytes)
	return h
}

// ValidHeader checks exactly the header bytes, not the borrowed PCM reader or
// its EOF. The caller remains responsible for consuming the declared payload.
func ValidHeader(header []byte, fileBytes int64) bool {
	if len(header) != HeaderBytes || fileBytes < MinBytes || fileBytes > maxRIFFFileBytes ||
		(fileBytes-HeaderBytes)%FrameBytes != 0 {
		return false
	}
	want := Header(uint32(fileBytes - HeaderBytes))
	return bytes.Equal(header, want[:])
}

// MaxFileBytes derives the private request/reply cap from a validated positive
// duration. Preserve the RIFF container bound even when it is not frame-aligned.
func MaxFileBytes(seconds int) int64 {
	if int64(seconds) > (maxRIFFFileBytes-HeaderBytes)/bytesPerSecond {
		return maxRIFFFileBytes
	}
	return HeaderBytes + int64(seconds)*bytesPerSecond
}

// MaxDataBytes is the decoder's frame-aligned PCM cap, excluding the header.
// At RIFF saturation it leaves one byte of the container allowance unused.
func MaxDataBytes(seconds int) int64 {
	limit := MaxFileBytes(seconds) - HeaderBytes
	return limit - limit%FrameBytes
}
