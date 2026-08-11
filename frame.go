package flac

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
)

const (
	frameSyncCode uint16 = 0b111111111111100
)

type blockingStrategy int

const (
	blockingStrategyFixed    blockingStrategy = 0
	blockingStrategyVariable blockingStrategy = 1
)

var (
	errFrameSync          = errors.New("flac: invalid frame sync")
	errBitDepth           = errors.New("flac: invalid bit depth")
	errCodedNumber        = errors.New("flac: invalid coded number")
	errBlockSize          = errors.New("flac: invalid block size")
	errChannel            = errors.New("flac: invalid channel")
	errUncommonBlockSize  = errors.New("flac: invalid uncommon block size")
	errSampleRate         = errors.New("flac: invalid sample rate")
	errUncommonSampleRate = errors.New("flac: invalid uncommon sample rate")
	errCRC                = errors.New("flac: invalid CRC")
)

type frameHeader struct {
	blockingStrategy blockingStrategy
	blockSizeBits    uint8
	blockSize        uint16
	sampleRateBits   uint8
	sampleRateHz     uint32
	channel          channelAssignment
	bitDepth         uint8
	codedNumber      uint64
	crc              uint8
}

type channelAssignment uint8

const (
	channelsMono      channelAssignment = 0b0000 // 1ch
	channelsStereo    channelAssignment = 0b0001 // 2ch: left, right
	channels3         channelAssignment = 0b0010
	channels4         channelAssignment = 0b0011
	channels5         channelAssignment = 0b0100
	channels6         channelAssignment = 0b0101
	channels7         channelAssignment = 0b0110
	channels8         channelAssignment = 0b0111
	channelsLeftSide  channelAssignment = 0b1000 // subframe0=left, subframe1=side
	channelsSideRight channelAssignment = 0b1001 // subframe0=side, subframe1=right
	channelsMidSide   channelAssignment = 0b1010 // subframe0=mid,  subframe1=side
	// 0b1011 - 0b1111 are reserved
)

func readFrameHeader(b []byte, si streamInfo) (frameHeader, int, error) {
	if len(b) < 4 {
		return frameHeader{}, 0, fmt.Errorf("flac: frame header is shorter than 4 bytes (got %d): %w", len(b), io.ErrUnexpectedEOF)
	}
	// Frame Header: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1
	// The first 15 bits cover the 14-bit sync code plus the reserved bit, which MUST be zero.
	frameSync := uint16(b[0])<<8 | uint16(b[1])
	if frameSync>>1 != frameSyncCode {
		return frameHeader{}, 0, fmt.Errorf("%w: the first 15 bits do not match the sync code and the reserved bit: %015b", errFrameSync, frameSync>>1)
	}
	h := frameHeader{}
	if frameSync&1 == 0 {
		h.blockingStrategy = blockingStrategyFixed
	} else {
		h.blockingStrategy = blockingStrategyVariable
	}
	// Block Size Bits: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.1
	h.blockSizeBits = b[2] >> 4
	// Sample Rate Bits: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.2
	h.sampleRateBits = b[2] & 0x0F
	// Channels Bits: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.3
	h.channel = channelAssignment(b[3] >> 4)
	if h.channel > channelsMidSide {
		return frameHeader{}, 0, fmt.Errorf("%w: channel bits between 0b1011-0b1111 are reserved", errChannel)
	}

	// Bit Depth Bits: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.4
	bitDepthBits := (b[3] >> 1) & 0x07
	switch bitDepthBits {
	case 0b000: // Bit depth only stored in the streaminfo metadata block
		h.bitDepth = si.bitsPerSample
	case 0b001: // 8 bits per sample
		h.bitDepth = 8
	case 0b010: // 12 bits per sample
		h.bitDepth = 12
	case 0b011: // Reserved
		return frameHeader{}, 0, fmt.Errorf("%w: bit depth bits %03b are reserved", errBitDepth, bitDepthBits)
	case 0b100: // 16 bits per sample
		h.bitDepth = 16
	case 0b101: // 20 bits per sample
		h.bitDepth = 20
	case 0b110: // 24 bits per sample
		h.bitDepth = 24
	case 0b111: // 32 bits per sample
		h.bitDepth = 32
	}

	// The next bit is reserved and MUST be zero.
	if b[3]&0x1 != 0 {
		return frameHeader{}, 0, fmt.Errorf("%w: the reserved bit after the bit depth bits is not zero", errBitDepth)
	}
	// Coded Number: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.5
	codedNumber, size, err := decodeCodedNumber(b[4:])
	if err != nil {
		return frameHeader{}, 0, err
	}
	h.codedNumber = codedNumber
	nextIndex := 4 + size

	// Uncommon Block Size: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.6
	switch v := h.blockSizeBits; {
	case v == 0b0000: // Reserved
		return frameHeader{}, 0, fmt.Errorf("%w: block size bits %04b are reserved", errBlockSize, v)
	case v == 0b0001: // 192
		h.blockSize = 192
	case 0b0010 <= v && v <= 0b0101: // 144 * (2^v)
		h.blockSize = uint16(144 * (1 << v))
	case v == 0b0110: // Uncommon block size minus 1, stored as an 8-bit number
		if len(b) < nextIndex+1 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 8-bit value is truncated: %w", errUncommonBlockSize, io.ErrUnexpectedEOF)
		}
		h.blockSize = uint16(b[nextIndex]) + 1
		nextIndex += 1
	case v == 0b0111: // Uncommon block size minus 1, stored as a 16-bit number
		if len(b) < nextIndex+2 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 16-bit value is truncated: %w", errUncommonBlockSize, io.ErrUnexpectedEOF)
		}
		blockSize := uint16(b[nextIndex])<<8 | uint16(b[nextIndex+1])
		if blockSize == 65535 {
			return frameHeader{}, 0, fmt.Errorf("%w: a block size of 65536 (stored as 65535) is forbidden", errUncommonBlockSize)
		}
		h.blockSize = blockSize + 1
		nextIndex += 2
	case 0b1000 <= v && v <= 0b1111: // 2^v
		h.blockSize = uint16(1 << v)
	}

	// Uncommon Sample Rate: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.7
	const hzPerKHz uint32 = 1000
	switch h.sampleRateBits {
	case 0b0000: // Sample rate only stored in the streaminfo metadata block
		h.sampleRateHz = si.sampleRate
	case 0b0001: // 88.2 kHz
		h.sampleRateHz = 88200
	case 0b0010: // 176.4 kHz
		h.sampleRateHz = 176400
	case 0b0011: // 192 kHz
		h.sampleRateHz = 192000
	case 0b0100: // 8 kHz
		h.sampleRateHz = 8000
	case 0b0101: // 16 kHz
		h.sampleRateHz = 16000
	case 0b0110: // 22.05 kHz
		h.sampleRateHz = 22050
	case 0b0111: // 24 kHz
		h.sampleRateHz = 24000
	case 0b1000: // 32 kHz
		h.sampleRateHz = 32000
	case 0b1001: // 44.1 kHz
		h.sampleRateHz = 44100
	case 0b1010: // 48 kHz
		h.sampleRateHz = 48000
	case 0b1011: // 96 kHz
		h.sampleRateHz = 96000
	case 0b1100: // Uncommon sample rate in kHz, stored as an 8-bit number
		if len(b) < nextIndex+1 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 8-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = uint32(b[nextIndex]) * hzPerKHz
		nextIndex += 1
	case 0b1101: // Uncommon sample rate in Hz, stored as a 16-bit number
		if len(b) < nextIndex+2 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 16-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = uint32(b[nextIndex])<<8 | uint32(b[nextIndex+1])
		nextIndex += 2
	case 0b1110: // Uncommon sample rate in Hz divided by 10, stored as a 16-bit number
		if len(b) < nextIndex+2 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 16-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = (uint32(b[nextIndex])<<8 | uint32(b[nextIndex+1])) * 10
		nextIndex += 2
	case 0b1111: // Forbidden
		return frameHeader{}, 0, fmt.Errorf("%w: sample rate bits %04b are forbidden", errSampleRate, h.sampleRateBits)
	}

	if h.sampleRateHz == 0 {
		return frameHeader{}, 0, fmt.Errorf("%w: sample rate can not be 0", errSampleRate)
	}

	// Frame Header CRC: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.8
	if len(b) < nextIndex+1 {
		return frameHeader{}, 0, fmt.Errorf("%w: the CRC byte is missing: %w", errCRC, io.ErrUnexpectedEOF)
	}
	h.crc = b[nextIndex]
	nextIndex += 1
	return h, nextIndex, nil
}

func decodeCodedNumber(b []byte) (uint64, int, error) {
	// Coded Number: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.5
	if len(b) < 1 {
		return 0, 0, fmt.Errorf("%w: the input is empty: %w", errCodedNumber, io.ErrUnexpectedEOF)
	}
	if b[0] == 0xFF || (b[0]>>6) == 0b10 {
		return 0, 0, fmt.Errorf("%w: invalid first byte %#x", errCodedNumber, b[0])
	}
	// ^b[0] flips every bit, so the leading zeros of the complement count the leading ones of b[0],
	// which is the total length of the coded number in bytes.
	byteLength := bits.LeadingZeros8(^b[0])
	if byteLength == 0 {
		return uint64(b[0]), 1, nil
	}
	if len(b) < byteLength {
		return 0, 0, fmt.Errorf("%w: it needs %d bytes but only %d are available: %w", errCodedNumber, byteLength, len(b), io.ErrUnexpectedEOF)
	}
	val := uint64(b[0]) & (1<<(7-byteLength) - 1)
	for i := 1; i < byteLength; i++ {
		if b[i]>>6 != 0b10 {
			return 0, 0, fmt.Errorf("%w: the continuation byte at position %d does not start with 0b10: %#x", errCodedNumber, i+1, b[i])
		}
		val = val<<6 | uint64(b[i]&0x7F)
	}
	return val, byteLength, nil
}
