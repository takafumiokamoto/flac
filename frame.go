package flac

import (
	"bufio"
	"bytes"
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
	// FIXME: organize
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
	// 0b1011〜0b1111は予約済み(reserved)
)

// count returns the number of channels (and thus subframes) coded in a frame with this channel assignment.
func (c channelAssignment) count() uint {
	// Channels Bits: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.3
	// Table 16 では 0b0000〜0b0111 が「値+1」チャンネルの独立符号化、
	// 0b1000〜0b1010(left-side / side-right / mid-side)は常に 2 チャンネル。
	// 0b1011 以上は reserved で readFrameHeader が弾いているので、ここには来ない。
	if c >= channelsLeftSide {
		return 2
	}
	return uint(c + 1)
}

type frameDecoder struct {
	r          *bufio.Reader
	si         StreamInfo
	frameCRC16 uint16
}

func newFrameDecoder(r *bufio.Reader, si StreamInfo) *frameDecoder {
	return &frameDecoder{
		r:  r,
		si: si,
	}
}

func (f *frameDecoder) setCRC16(b []byte) {
	f.frameCRC16 = crc16(b)
}

func (f *frameDecoder) ReadByte() (byte, error) {
	b, err := f.r.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			// bitReaderで読む部分で途中でEOFになる場合はフレームが途中で切れている。
			// 呼び出し元はio.EOFが返却されると正常終了する慣例なので、io.ErrUnexpectedEOFに置き換える
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}
	// crcを逐次で計算する。
	f.frameCRC16 = crc16Update(f.frameCRC16, b)
	return b, nil
}

// Frame Header: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1
//
//	固定部なのは5バイト
//	  §9.1   frame sync code 15ビット + blocking strategy bit 1ビット      = 2バイト
//	  §9.1.1 block size bits 4ビット  + §9.1.2 sample rate bits 4ビット    = 1バイト
//	  §9.1.3 channel bits 4ビット + §9.1.4 bit depth bits 3 + reserved 1    = 1バイト
//	  §9.1.8 CRC-8                     1バイト
const (
	// 固定部 + coded number(最大 7)+ uncommon block size(最大 2)+ uncommon sample rate(最大 2)
	maxFrameHeaderSize = 5 + 7 + 2 + 2 // 16
	// 固定部 + coded number(最小 1)+ uncommon block size(なし)+ uncommon sample rate(なし)
	minFrameHeaderSize = 5 + 1 + 0 + 0 // 6
)

func (f *frameDecoder) decodeFrame() (frameHeader, []int64, error) {
	hBuf, err := f.r.Peek(16)
	if err != nil && !errors.Is(err, io.EOF) {
		return frameHeader{}, nil, err
	}
	switch {
	case len(hBuf) == 0:
		return frameHeader{}, nil, io.EOF
	case len(hBuf) < minFrameHeaderSize:
		return frameHeader{}, nil, io.ErrUnexpectedEOF
	}

	h, consumed, err := readFrameHeader(hBuf, f.si)
	if err != nil {
		return frameHeader{}, nil, err
	}
	// frameのCRC16の逐次計算
	f.setCRC16(hBuf[:consumed])

	// 実際に読んだ分だけ捨てる
	if _, err := f.r.Discard(consumed); err != nil {
		return frameHeader{}, nil, fmt.Errorf("flac: failed to discard consumed bytes when reading frame header:%w", err)
	}

	br := newBitReader(f)
	// FIXME: 使い回しのバッファを検討
	var samples []int64
	switch h.channel {
	case channelsMono, channelsStereo, channels3, channels4, channels5, channels6, channels7, channels8:
		s, err := decodeIndependent(br, h.channel, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, err
		}
		samples = s
	case channelsLeftSide:
		s, err := decodeLeftSide(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, err
		}
		samples = s
	case channelsSideRight:
		s, err := decodeSideRight(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, err
		}
		samples = s
	case channelsMidSide:
		s, err := decodeMidSide(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, err
		}
		samples = s
	default:
		return frameHeader{}, nil, fmt.Errorf("flac: invalid channel :%d", h.channel)
	}

	// 余りを読み込んで次のバイト境界CRC16の始まりに揃える
	if _, err = br.readBits(br.cnt); err != nil {
		return frameHeader{}, nil, fmt.Errorf("flac: failed to read padding bits:%w", err)
	}

	// CRC-16の直前までのCRC16
	wantCRC16 := f.frameCRC16

	fistCRC16Bits, err := f.ReadByte()
	if err != nil {
		return frameHeader{}, nil, fmt.Errorf("flac: faild to read first byte of CRC-16:%w", err)
	}
	secondCRC16Bits, err := f.ReadByte()
	if err != nil {
		return frameHeader{}, nil, fmt.Errorf("flac: faild to read first byte of CRC-16:%w", err)
	}
	storedCRC := uint16(fistCRC16Bits)<<8 | uint16(secondCRC16Bits)
	// CRC16の検証
	if storedCRC != wantCRC16 {
		return frameHeader{}, nil, fmt.Errorf("%w: CRC-16 does not match: stored:%02x, got:%02x", errCRC, storedCRC, wantCRC16)
	}

	return h, samples, nil
}

func readFrameHeader(b []byte, si StreamInfo) (frameHeader, int, error) {
	// 長さは事前に呼び出し元でチェック済みではある。
	if len(b) < 4 {
		return frameHeader{}, 0, fmt.Errorf("flac: frame header is shorter than 4 bytes (got %d): %w", len(b), io.ErrUnexpectedEOF)
	}
	// Frame Header: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1
	// 先頭15ビットは14ビットのsync codeと、0でなければならない(MUST)予約ビット1つ。
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
	case 0b000: // bit depthはSTREAMINFOにのみ格納されている
		h.bitDepth = si.BitsPerSample
	case 0b001: // 8ビット
		h.bitDepth = 8
	case 0b010: // 12ビット
		h.bitDepth = 12
	case 0b011: // 予約済み(reserved)
		return frameHeader{}, 0, fmt.Errorf("%w: bit depth bits %03b are reserved", errBitDepth, bitDepthBits)
	case 0b100: // 16ビット
		h.bitDepth = 16
	case 0b101: // 20ビット
		h.bitDepth = 20
	case 0b110: // 24ビット
		h.bitDepth = 24
	case 0b111: // 32ビット
		h.bitDepth = 32
	}

	// 次の1ビットは予約ビットで、0でなければならない(MUST)。
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
	case v == 0b0000: // 予約済み(reserved)
		return frameHeader{}, 0, fmt.Errorf("%w: block size bits %04b are reserved", errBlockSize, v)
	case v == 0b0001: // 192
		h.blockSize = 192
	case 0b0010 <= v && v <= 0b0101: // 144 * (2^v)
		h.blockSize = uint16(144 * (1 << v))
	case v == 0b0110: // 一般的でないブロックサイズ−1を8ビットで格納
		if len(b) < nextIndex+1 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 8-bit value is truncated: %w", errUncommonBlockSize, io.ErrUnexpectedEOF)
		}
		h.blockSize = uint16(b[nextIndex]) + 1
		nextIndex += 1
	case v == 0b0111: // 一般的でないブロックサイズ−1を16ビットで格納
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
	case 0b0000: // サンプルレートはSTREAMINFOにのみ格納されている
		h.sampleRateHz = si.SampleRate
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
	case 0b1100: // 一般的でないサンプルレートをkHz単位・8ビットで格納
		if len(b) < nextIndex+1 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 8-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = uint32(b[nextIndex]) * hzPerKHz
		nextIndex += 1
	case 0b1101: // 一般的でないサンプルレートをHz単位・16ビットで格納
		if len(b) < nextIndex+2 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 16-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = uint32(b[nextIndex])<<8 | uint32(b[nextIndex+1])
		nextIndex += 2
	case 0b1110: // 一般的でないサンプルレートをHz/10単位・16ビットで格納
		if len(b) < nextIndex+2 {
			return frameHeader{}, 0, fmt.Errorf("%w: the 16-bit value is truncated: %w", errUncommonSampleRate, io.ErrUnexpectedEOF)
		}
		h.sampleRateHz = (uint32(b[nextIndex])<<8 | uint32(b[nextIndex+1])) * 10
		nextIndex += 2
	case 0b1111: // 禁止(forbidden)
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
	crcSum := crc8(b[:nextIndex])
	if h.crc != crcSum {
		return frameHeader{}, 0, fmt.Errorf("%w: the CRC does not match: stored:%02x, got:%02x", errCRC, h.crc, crcSum)
	}
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
	// ^b[0]で全ビットを反転すると、反転後の先頭0の数 = b[0]の先頭1の数になる。
	// これがcoded number全体のバイト長。
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

func decodeFrame(b []byte, startIndex int, si StreamInfo) (frameHeader, []int64, int, error) {
	h, nextIndex, err := readFrameHeader(b[startIndex:], si)
	if err != nil {
		return frameHeader{}, nil, 0, fmt.Errorf("flac: failed to read frame header:%w", err)
	}
	br := newBitReader(bytes.NewReader(b[startIndex+nextIndex:]))
	// FIXME: (perf) 事前バッファ
	var samples []int64
	switch h.channel {
	case channelsMono, channelsStereo, channels3, channels4, channels5, channels6, channels7, channels8:
		s, err := decodeIndependent(br, h.channel, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, 0, err
		}
		samples = s
	case channelsLeftSide:
		s, err := decodeLeftSide(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, 0, err
		}
		samples = s
	case channelsSideRight:
		s, err := decodeSideRight(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, 0, err
		}
		samples = s
	case channelsMidSide:
		s, err := decodeMidSide(br, h.bitDepth, h.blockSize)
		if err != nil {
			return frameHeader{}, nil, 0, err
		}
		samples = s
	default:
		return frameHeader{}, nil, 0, fmt.Errorf("flac: invalid channel :%d", h.channel)
	}
	if _, err = br.readBits(br.cnt); err != nil { // 余りを読み込んで次のバイト境界CRC16の始まりに揃える
		return frameHeader{}, nil, 0, fmt.Errorf("flac: failed to read padding bits:%w", err)
	}
	crcStart := startIndex + nextIndex + int(br.bytesRead)
	if len(b) < crcStart+2 {
		return frameHeader{}, nil, 0, fmt.Errorf("%w: the CRC byte is missing:%w", errCRC, io.ErrUnexpectedEOF)
	}
	storedCRC := uint16(b[crcStart])<<8 | uint16(b[crcStart+1])
	crcSum := crc16(b[startIndex:crcStart])
	if storedCRC != crcSum {
		return frameHeader{}, nil, 0, fmt.Errorf("%w: CRC-16 does not match: stored:%02x, got:%02x", errCRC, storedCRC, crcSum)
	}
	endIndex := crcStart + 2 // CRC-16の分の2を足す
	return h, samples, endIndex, nil
}

func decodeIndependent(
	br *bitReader, channel channelAssignment, bitDepth uint8, blockSize uint16,
) (
	[]int64, error,
) {
	// FIXME: (perf) 事前バッファ
	var samples []int64
	for i := range channel.count() {
		s, err := decodeSubframe(br, bitDepth, blockSize)
		if err != nil {
			return nil, fmt.Errorf("flac: failed to decode subframe %d, err:%w", i, err)
		}
		samples = append(samples, s...)
	}
	return samples, nil
}

func decodeMidSide(br *bitReader, bitDepth uint8, blockSize uint16) ([]int64, error) {
	// FIXME: (perf) 事前バッファ
	var samples []int64
	mid, err := decodeSubframe(br, bitDepth, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe mid, err:%w", err)
	}
	side, err := decodeSubframe(br, bitDepth+1, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe side, err:%w", err)
	}
	// Interchannel Decorrelation: https://www.rfc-editor.org/rfc/rfc9639.html#section-4.2
	left := make([]int64, blockSize)
	right := make([]int64, blockSize)
	for i := range blockSize {
		m := mid[i] << 1
		if side[i]&1 != 0 {
			m += 1
		}
		left[i] = (m + side[i]) >> 1
		right[i] = (m - side[i]) >> 1
	}
	samples = append(samples, left...)
	samples = append(samples, right...)
	return samples, nil
}

func decodeLeftSide(br *bitReader, bitDepth uint8, blockSize uint16) ([]int64, error) {
	// FIXME: (perf) 事前バッファ
	var samples []int64
	left, err := decodeSubframe(br, bitDepth, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe left, err:%w", err)
	}
	side, err := decodeSubframe(br, bitDepth+1, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe side, err:%w", err)
	}
	// Interchannel Decorrelation: https://www.rfc-editor.org/rfc/rfc9639.html#section-4.2
	// sideはleft - rightとして符号化されているので、right = left - sideで復元する。
	right := make([]int64, blockSize)
	for i := range right {
		right[i] = left[i] - side[i]
	}
	samples = append(samples, left...)
	samples = append(samples, right...)
	return samples, nil
}

func decodeSideRight(br *bitReader, bitDepth uint8, blockSize uint16) ([]int64, error) {
	// FIXME: (perf) 事前バッファ
	var samples []int64
	side, err := decodeSubframe(br, bitDepth+1, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe side, err:%w", err)
	}
	right, err := decodeSubframe(br, bitDepth, blockSize)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to decode subframe right, err:%w", err)
	}
	left := make([]int64, blockSize)
	for i := range left {
		left[i] = right[i] + side[i]
	}
	samples = append(samples, left...)
	samples = append(samples, right...)
	return samples, nil
}
