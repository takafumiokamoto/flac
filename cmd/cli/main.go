package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/takafumiokamoto/flac"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "err:%v\n", err)
		return
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("please specify flac file")
	}
	inputFilePath := os.Args[1]
	f, err := os.Open(inputFilePath)
	if err != nil {
		return fmt.Errorf("failed to open file:%w", err)
	}
	defer f.Close()
	pcm, err := flac.Decode(f)
	if err != nil {
		return err
	}
	wav, err := toWAV(pcm)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(inputFilePath), filepath.Ext(inputFilePath))
	outPath := "./" + name + ".wav"
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create WAV file:%w", err)
	}
	defer out.Close()
	_, err = out.Write(wav)
	if err != nil {
		return fmt.Errorf("failed to write WAV file:%w", err)
	}
	fmt.Printf("output .wav file in: %s\n", outPath)
	return nil
}

// toWAV wraps decoded PCM in a RIFF/WAVE container (WAVE_FORMAT_PCM, 44-byte header).
func toWAV(pcm flac.PCM) ([]byte, error) {
	// WAV は RFC 9639 の範囲外。レイアウトは Microsoft の RIFF/WAVE 仕様(WAVEFORMATEX)に従う。
	// Decode が返す Data は §8.2 の並び(インターリーブ・LE・バイト境界まで符号拡張)で、
	// 16/24/32 bit ならそのまま data チャンクの中身になる。
	if pcm.SampleRate == 0 {
		// §8.2: 音声を含むなら sample rate は 0 であってはならない(0 は非音声データ用)。WAV にはできない。
		return nil, errors.New("sample rate is 0 (non-audio stream)")
	}
	if pcm.Channels == 0 {
		return nil, errors.New("channels is 0")
	}
	if pcm.BitsPerSample%8 != 0 {
		// TODO: 12/20 bit は WAV ではコンテナ(16/24 bit)に MSB 詰め(左寄せ)で入れるのが慣習。
		// §8.2 の符号拡張(右寄せ)とは逆なので、Data を << (コンテナ幅 − bps) してから書く必要がある。
		return nil, fmt.Errorf("bits per sample %d is not a multiple of 8: not supported yet", pcm.BitsPerSample)
	}
	if pcm.BitsPerSample == 8 {
		// TODO: 8 bit の WAV は符号なし(0〜255、中心 128)。§8.2 の符号付きから各バイトを ^0x80 して書く必要がある。
		return nil, errors.New("8 bit is not supported yet (WAV stores 8-bit PCM unsigned)")
	}

	const headerSize = 44
	bytesPerSample := uint32(pcm.BitsPerSample) / 8
	blockAlign := uint32(pcm.Channels) * bytesPerSample // 1 インターチャンネルサンプルのバイト数
	byteRate := pcm.SampleRate * blockAlign             // 1 秒あたりのバイト数
	dataSize := len(pcm.Data)
	pad := dataSize % 2                         // RIFF ではチャンクを偶数長に揃える。奇数なら末尾に 1 バイト詰める(dataSize には含めない)
	riffSize := headerSize - 8 + dataSize + pad // "RIFF" とサイズ欄の 8 バイトを除いた全長
	if uint64(riffSize) > math.MaxUint32 {
		return nil, errors.New("PCM data is too large for a RIFF file (4 GiB limit)")
	}

	out := make([]byte, headerSize, headerSize+dataSize+pad)
	// RIFF チャンク
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(riffSize))
	copy(out[8:12], "WAVE")
	// fmt チャンク(WAVEFORMATEX の PCM 形式、本体 16 バイト)
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // wFormatTag = WAVE_FORMAT_PCM
	binary.LittleEndian.PutUint16(out[22:24], uint16(pcm.Channels))
	binary.LittleEndian.PutUint32(out[24:28], pcm.SampleRate)
	binary.LittleEndian.PutUint32(out[28:32], byteRate)
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], uint16(pcm.BitsPerSample))
	// data チャンク
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))
	out = append(out, pcm.Data...)
	if pad == 1 {
		out = append(out, 0)
	}
	return out, nil
}
