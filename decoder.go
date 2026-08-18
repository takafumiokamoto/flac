package flac

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrRead     = errors.New("flac: failed to read input")
	ErrMarker   = errors.New("flac: invalid stream marker")
	ErrMetaData = errors.New("flac: invalid metadata")
	ErrFrame    = errors.New("flac: invalid frame")
	ErrMD5      = errors.New("flac: MD5 sum does not match")
)

type MetaData struct {
	Name   string
	Length time.Duration
}

type PCM struct {
	MetaData
	Data []byte
}

func Decode(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRead, err)
	}
	br := bytes.NewReader(b)
	if err := validateMarker(br); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMarker, err)
	}
	meta, err := readMetadata(br)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetaData, err)
	}
	si := meta.streamInfo
	offset := len(b) - br.Len()
	var pcm []byte
	for offset < len(b) {
		header, frame, next, err := decodeFrame(b, offset, si)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFrame, err)
		}
		offset = next
		// PCMに変換
		pcm = append(pcm, toPCMSample(header, frame)...)
	}
	if si.md5Sum == [16]byte{} {
		// MD5がstreaminfoに格納されていない場合はMD5の比較を行わない
		return pcm, nil
	}
	// デコード結果をMD5で検証
	sum := md5.Sum(pcm)
	if sum != si.md5Sum {
		return nil, fmt.Errorf("%w: stored:%x, got:%x", ErrMD5, si.md5Sum, sum)
	}
	return pcm, nil
}

func toPCMSample(header frameHeader, frame []int64) []byte {
	// PCMに変換
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
