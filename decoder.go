// Package flac provides a decoder fro FLAC (Free Lossless Audio Codec) as specified in RFC 9639.
package flac

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"hash"
	"io"
)

var (
	// ErrMarker is returned when the stream does not begin with the
	// "fLaC" marker (Section 6).
	ErrMarker = errors.New("flac: invalid stream marker")

	// ErrMetadata is returned when a metadata block is invalid
	// (Section 8).
	ErrMetadata = errors.New("flac: invalid metadata")

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
	MD5Sum        [16]byte
}

// Decoder decodes a FLAC steram.
type Decoder struct {
	r      *bufio.Reader
	frames *frameDecoder
	meta   Metadata
	// 呼び出し元に返却していないPCM
	pending []byte
	// 処理を継続できないエラー
	terminalErr error
	// PCM全体のMD5ハッシュ
	hash hash.Hash
}

// NewDecoder returns a Decoder.
//
// NewDecoder reads the "fLaC" marker and all metadata blocks before returning.
// These metadata blocks are available via [Decoder.Metadata] and [Decoder.StreamInfo]
func NewDecoder(r io.Reader) (*Decoder, error) {
	rd := bufio.NewReader(r)
	if err := validateMarker(rd); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMarker, err)
	}
	meta, err := readMetadata(rd)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetadata, err)
	}
	return &Decoder{
		r:      rd,
		meta:   meta,
		frames: newFrameDecoder(rd, meta.StreamInfo),
		hash:   md5.New(),
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

// Read decodes FLAC audio and reads up to len(p) bytes of interleaved PCM into p.
//
// The PCM samples are signed and little-endian, with channels interleaved
// on a per-sample basis. If the bit depth is not a whole number of bytes,
// each sample is sign-extended to the next whole number of bytes, as
// specified in RFC 9639 Section 8.2.
//
// Read boundaries are independent of PCM sample and FLAC frame boundaries.
// A sample or frame may therefore be split across multiple calls to Read.
// The stream format is available through [Decoder.StreamInfo].
//
// If STREAMINFO contains a non-zero MD5 checksum, Read verifies it after
// decoding the final frame. Callers must continue reading until Read returns
// [io.EOF]. If the checksum does not match, Read returns [ErrMD5] instead of [io.EOF].
// An all-zero checksum means that no checksum is available, and verification is skipped.
//
// Decoder is stateful and represents a single decoding pass. Read must not
// be called concurrently.
func (d *Decoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// 返却していないPCMがあれば返却
	if len(d.pending) > 0 {
		return d.copyPending(p), nil
	}

	// エラーがあれば返却
	if d.terminalErr != nil {
		return 0, d.terminalErr
	}

	header, samples, err := d.frames.decodeFrame()
	// FIXME: 読み込んだフレームのヘッダの内容がSTREAMINFOと食い違う場合は
	// ここで判定してエラーにする。
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = d.finish() //MD5の検証
			if err != nil {
				d.terminalErr = err
			} else {
				d.terminalErr = io.EOF
			}
		} else {
			d.terminalErr = fmt.Errorf("%w: %w", ErrFrame, err)
		}
		return 0, d.terminalErr
	}
	// PCMに変換
	pcm := toPCMSample(header, samples)
	// MD5のハッシュ計算(逐次)
	// MD5のWriteはエラーを返さないので結果は両方捨てる
	_, _ = d.hash.Write(pcm)
	d.pending = pcm
	return d.copyPending(p), nil
}

// Decode decodes the FLAC stream to interleaved PCM samples and writes it to w.
//
// The PCM samples are signed and little-endian, with channels interleaved
// on a per-sample basis. If the bit depth is not a whole number of bytes,
// each sample is sign-extended to the next whole number of bytes, as
// specified in RFC 9639 Section 8.2.
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
	if _, err := io.Copy(w, d); err != nil {
		return err
	}
	return nil
}

func (d *Decoder) copyPending(p []byte) int {
	n := copy(p, d.pending)
	d.pending = d.pending[n:]

	if len(d.pending) == 0 {
		d.pending = nil
	}

	return n
}

func (d *Decoder) finish() error {
	wantMD5Sum := d.meta.StreamInfo.MD5Sum
	if wantMD5Sum == [16]byte{} {
		// stream infoにMD5が設定されていなければMD5のチェックをしない
		return nil
	}
	gotMD5Sum := [16]byte(d.hash.Sum(nil))
	if wantMD5Sum != gotMD5Sum {
		return fmt.Errorf("%w: want:%x, got:%x", ErrMD5, wantMD5Sum, gotMD5Sum)
	}
	return nil
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
