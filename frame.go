package flac

import (
	"errors"
	"fmt"
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
	errFrameSync   = errors.New("flac: invalid frame sync,")
	errBitDepth    = errors.New("flac: invalid bit depth,")
	errCodedNumber = errors.New("flac: invalid Coded Number,")
)

type frameHeader struct {
	blockingStrategy blockingStrategy
	blockSize        uint8
	sampleRate       uint8
	channel          uint8
	bitDepth         uint8
	codedNumber      uint64
}

func readFrameHeader(b []byte, si streamInfo) (frameHeader, error) {
	if len(b) < 4 {
		return frameHeader{}, fmt.Errorf("flac: frame header is shorter than 4bytes, %d", len(b))
	}
	// frame sync code check : https://datatracker.ietf.org/doc/html/rfc9639#section-9.1
	frameSync := uint16(b[0])<<8 | uint16(b[1])
	if frameSync>>1 != frameSyncCode {
		return frameHeader{}, fmt.Errorf("%w first 15bits of frame header doesn't match frame sync code: %015b", errFrameSync, frameSync)
	}
	h := frameHeader{}
	if frameSync&1 == 0 {
		h.blockingStrategy = blockingStrategyFixed
	} else {
		h.blockingStrategy = blockingStrategyVariable
	}
	// Block Size Bits https://datatracker.ietf.org/doc/html/rfc9639#section-9.1.1
	h.blockSize = b[2] >> 4
	// Sample Rate Bits https://datatracker.ietf.org/doc/html/rfc9639#name-sample-rate-bits
	h.sampleRate = b[2] & 0x0F
	// Channels Bits https://datatracker.ietf.org/doc/html/rfc9639#name-channels-bits
	h.channel = b[3] >> 4
	// Bits Depth Bits https://datatracker.ietf.org/doc/html/rfc9639#name-bit-depth-bits
	h.bitDepth = (b[3] >> 1) & 0x07
	// The next bit is reserved and MUST be zero.
	if b[3]&0x1 != 0 {
		return frameHeader{}, fmt.Errorf("%w reserved value in the last bit of bit depth", errBitDepth)
	}
	// Coded Number https://datatracker.ietf.org/doc/html/rfc9639#section-9.1.5
	_, _, err := decodeCodedNumber(b[4:])
	if err != nil {
		return frameHeader{}, err
	}
	return h, nil
}

func decodeCodedNumber(b []byte) (uint64, int, error) {
	//https://datatracker.ietf.org/doc/html/rfc9639#name-coded-number
	if len(b) < 1 {
		return 0, 0, fmt.Errorf("%w length of coded number is zero", errCodedNumber)
	}
	if b[0] == 0xFF || (b[0]>>6) == 0b10 {
		return 0, 0, fmt.Errorf("%w invalid first byte of codedNumber: %#x", errCodedNumber, b[0])
	}
	payloadLength := bits.LeadingZeros8(^b[0]) // returns leading 0 =>(reverse)=> returns count of 1
	if payloadLength == 0 {
		return uint64(b[0]), 1, nil
	}
	if len(b) < payloadLength {
		return 0, 0, fmt.Errorf("%w payload length is %d but actual length is %d", errCodedNumber, payloadLength, len(b))
	}
	val := uint64(b[0]) & (1<<(7-payloadLength) - 1)
	for i := 1; i < payloadLength; i++ {
		if b[i]>>6 != 0b10 {
			return 0, 0, fmt.Errorf("%w second byte doesn't start with 0b10: %#x, positoin:%d", errCodedNumber, b[i], i+1)
		}
		val = val<<6 | uint64(b[i]&0x7F)
	}
	return val, payloadLength, nil
}
