// Package flac provides a decoder fro FLAC (Free Lossless Audio Codec) as specified in RFC 9639.
package flac

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrRead is returned when reading the input fails.
	ErrRead = errors.New("flac: failed to read input")

	// ErrMarker is returned when the stream does not begin with the
	// "fLaC" marker (Section 6).
	ErrMarker = errors.New("flac: invalid stream marker")

	// ErrMetaData is returned when a metadata block is invalid
	// (Section 8).
	ErrMetaData = errors.New("flac: invalid metadata")

	// ErrFrame is returned when an audio frame is invalid (Section 9).
	ErrFrame = errors.New("flac: invalid frame")

	// ErrMD5 is returned when the decoded samples do not match the MD5
	// checksum stored in the streaminfo metadata block (Section 8.2).
	ErrMD5 = errors.New("flac: MD5 sum does not match")
)

// Metadata holds the metadata of a FLAC stream.
type Metadata struct {
	// Currently, StreamInfo only.
	StreamInfo
}

// StreamInfo: https://www.rfc-editor.org/rfc/rfc9639.html#name-streaminfo
type StreamInfo struct {
	MinBlockSize  uint16
	MaxBlockSize  uint16
	MinFrameSize  uint32
	MaxFrameSize  uint32
	SampleRate    uint32
	Channels      uint8
	BitsPerSample uint8
	TotalSamples  uint64
	Md5Sum        [16]byte
}

// Decoder decodes a FLAC steram.
type Decoder struct {
	r    io.Reader
	meta Metadata
}

// NewDecoder returns a Decoder.
//
// NewDecoder reads the "fLaC" marker and all metadata blocks before returning.
// These metadata blocks are available via [Decoder.Metadata] and [Decoder.StreamInfo]
func NewDecoder(r io.Reader) (*Decoder, error) {
	if err := validateMarker(r); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMarker, err)
	}
	meta, err := readMetadata(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetaData, err)
	}
	return &Decoder{
		r:    r,
		meta: meta,
	}, nil
}

// Metadata returns the matadata of a FLAC Stream.
//
// https://www.rfc-editor.org/rfc/rfc9639.html#name-file-level-metadata
func (d *Decoder) Metadata() Metadata {
	return d.meta
}

// StreamInfo returns streaminfo of a FLAC stream.
//
// https://www.rfc-editor.org/rfc/rfc9639.html#name-streaminfo
func (d *Decoder) StreamInfo() StreamInfo {
	return d.meta.StreamInfo
}

// Decode decodes the FLAC stream to interleaved PCM samples and writes it to w.
//
// If the streaminfo metadata block stores an MD5 checksum, Decode
// verifies the decoded samples against it and returns [ErrMD5] if it
// does not match; an all-zero checksum means none is stored, and
// verification is skipped.
//
// Decode returns [ErrFrame] if an audio frame is invalid. This includes
// a mismatch of the frame header CRC (Section 9.1.8) or the
// frame footer CRC (Section 9.3).
func (d *Decoder) Decode(w io.Writer) error {
	// FIMXE: mock
	return nil
}

type PCM struct {
	StreamInfo
	Data []byte
}

func Decode(r io.Reader) (PCM, error) {
	// FIXME: ストリーム化
	b, err := io.ReadAll(r)
	if err != nil {
		return PCM{}, fmt.Errorf("%w: %w", ErrRead, err)
	}
	br := bytes.NewReader(b)
	if err := validateMarker(br); err != nil {
		return PCM{}, fmt.Errorf("%w: %w", ErrMarker, err)
	}
	meta, err := readMetadata(br)
	if err != nil {
		return PCM{}, fmt.Errorf("%w: %w", ErrMetaData, err)
	}
	si := meta.StreamInfo
	offset := len(b) - br.Len()
	// FIXME: サイズ予測ができるように
	var sample []byte
	for offset < len(b) {
		// TODO: Frameのデコードは並行して行えるが、デコードをしないと次のFrameのバイト境界が分からない。
		// もしgoroutineで並行デコードするなら事前にバイト境界のみ先読みする必要がある。
		header, frame, next, err := decodeFrame(b, offset, si)
		if err != nil {
			return PCM{}, fmt.Errorf("%w: %w", ErrFrame, err)
		}
		offset = next
		// PCMに変換
		sample = append(sample, toPCMSample(header, frame)...)
	}
	pcm := PCM{
		Data:       sample,
		StreamInfo: si,
	}
	if si.Md5Sum == [16]byte{} {
		// MD5がstreaminfoに格納されていない場合はMD5の比較を行わない
		return pcm, nil
	}
	// デコード結果をMD5で検証
	sum := md5.Sum(sample)
	if sum != si.Md5Sum {
		return PCM{}, fmt.Errorf("%w: stored:%x, got:%x", ErrMD5, si.Md5Sum, sum)
	}
	return pcm, nil
}

func toPCMSample(header frameHeader, frame []int64) []byte {
	var buf []byte
	n := (header.bitDepth + 7) / 8 // ビット深度をバイト位置に切り上げ
	for i := range header.blockSize {
		for j := range header.channel.count() { // チャンネルを順番に処理
			s := frame[int(j)*int(header.blockSize)+int(i)] // 各チャンネル(pannar-flat上の)の開始位置を特定
			for k := range n {
				// "下位"からビット深度分をシフトして格納
				// 下位バイトから格納するので結果はlittle-endianになる。
				buf = append(buf, byte(s>>(8*k)))
			}
		}
	}
	return buf
}
