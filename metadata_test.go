package flac

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateMarker(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid marker", input: "fLaC", wantErr: false},
		{name: "invalid marker", input: "FLAC", wantErr: true},
		{name: "truncated marker", input: "fLa", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMarker(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMarker() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestReadMetadataBlockHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    metadataBlockHeader
		wantErr bool
	}{
		{
			name:  "valid STREAMINFO header",
			input: []byte{0x80, 0x00, 0x00, 0x22},
			want: metadataBlockHeader{
				isLast:    true,
				blockType: metadataBlockTypeStreamInfo,
				length:    34,
			},
		},
		{
			name:  "valid non-last header with 24-bit length",
			input: []byte{0x04, 0x12, 0x34, 0x56},
			want: metadataBlockHeader{
				isLast:    false,
				blockType: 4,
				length:    0x123456,
			},
		},
		{
			name:    "invalid block type 127",
			input:   []byte{0xFF, 0x00, 0x00, 0x64}, // 1 + 111 1111 (127)
			wantErr: true,
		},
		{
			name:    "truncated header",
			input:   []byte{0x80, 0x00, 0x00},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readMetadataBlockHeader(bytes.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("readMetadataBlockHeader() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err != nil {
				t.Logf("expected error: %v", err)
				return
			}
			if tt.want != got {
				t.Errorf("readMetadataBlockHeader() mismatch want:%#v got:%#v", tt.want, got)
			}
		})
	}
}

func TestReadStreamInfo(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    streamInfo
		wantErr bool
	}{
		{
			// example_1.flacではバイト境界をまたぐビット(bpsの最上位ビット、totalSamplesの上位ニブル)が0なので、
			// このベクタではそれらを立てておく。
			name: "byte-crossing bits set",
			input: []byte{
				0x00, 0x10, // 最小ブロックサイズ: 16
				0xFF, 0xFF, // 最大ブロックサイズ: 65535
				0x00, 0x00, 0x00, // 最小フレームサイズ: 不明
				0xFF, 0xFF, 0xFF, // 最大フレームサイズ: 16777215
				0x12, 0x34, 0x4B, // サンプルレート: 0x12344 = 74564, チャンネル数: 0b101 + 1 = 6, bpsの最上位ビット: 1
				0x3A,                   // bps: 0b10011 + 1 = 20, totalSamplesの上位ニブル: 0xA
				0x00, 0x00, 0x00, 0x01, // totalSamplesの下位32ビット
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // MD5
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
			want: streamInfo{
				minBlockSize:  16,
				maxBlockSize:  65535,
				minFrameSize:  0,
				maxFrameSize:  16777215,
				sampleRate:    74564,
				channels:      6,
				bitsPerSample: 20,
				totalSamples:  0xA_0000_0001,
				md5Sum: [16]byte{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
					0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
				},
			},
		},
		{
			name:    "truncated",
			input:   []byte{0x00, 0x10, 0xFF},
			wantErr: true,
		},
		{
			name: "minimum block size is invalid",
			input: []byte{
				0x00, 0x0f, // 最小ブロックサイズ: 15
				0xFF, 0xFF, // 最大ブロックサイズ: 65535
				0x00, 0x00, 0x00, // 最小フレームサイズ: 不明
				0xFF, 0xFF, 0xFF, // 最大フレームサイズ: 16777215
				0x12, 0x34, 0x4B, // サンプルレート: 0x12344 = 74564, チャンネル数: 0b101 + 1 = 6, bpsの最上位ビット: 1
				0x3A,                   // bps: 0b10011 + 1 = 20, totalSamplesの上位ニブル: 0xA
				0x00, 0x00, 0x00, 0x01, // totalSamplesの下位32ビット
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // MD5
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
			wantErr: true,
		},
		{
			name: "maximum block size is invalid",
			input: []byte{
				0x00, 0x10, // 最小ブロックサイズ: 16
				0x00, 0x0f, // 最大ブロックサイズ: 15
				0x00, 0x00, 0x00, // 最小フレームサイズ: 不明
				0xFF, 0xFF, 0xFF, // 最大フレームサイズ: 16777215
				0x12, 0x34, 0x4B, // サンプルレート: 0x12344 = 74564, チャンネル数: 0b101 + 1 = 6, bpsの最上位ビット: 1
				0x3A,                   // bps: 0b10011 + 1 = 20, totalSamplesの上位ニブル: 0xA
				0x00, 0x00, 0x00, 0x01, // totalSamplesの下位32ビット
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // MD5
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
			wantErr: true,
		},
		{
			name: "minimum block size is greater than maximum block size",
			input: []byte{
				0x00, 0x11, // 最小ブロックサイズ: 17
				0x00, 0x10, // 最大ブロックサイズ: 16
				0x00, 0x00, 0x00, // 最小フレームサイズ: 不明
				0xFF, 0xFF, 0xFF, // 最大フレームサイズ: 16777215
				0x12, 0x34, 0x4B, // サンプルレート: 0x12344 = 74564, チャンネル数: 0b101 + 1 = 6, bpsの最上位ビット: 1
				0x3A,                   // bps: 0b10011 + 1 = 20, totalSamplesの上位ニブル: 0xA
				0x00, 0x00, 0x00, 0x01, // totalSamplesの下位32ビット
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // MD5
				0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readStreamInfo(bytes.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("readStreamInfo() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err != nil {
				t.Logf("expected error: %v", err)
				return
			}
			if tt.want != got {
				t.Errorf("readStreamInfo() mismatch want:%v got:%v", tt.want, got)
			}
		})
	}
}

// 期待値はRFC 9639 Appendix Dに記載されているもの。
func TestReadStreamInfoRealFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		wantMD5 string
		want    streamInfo
	}{
		{
			// Appendix D.1.3
			name:    "example_1",
			file:    "example_1.flac",
			wantMD5: "3e84b41807dc690307586a3dad1a2e0f",
			want: streamInfo{
				minBlockSize:  4096,
				maxBlockSize:  4096,
				minFrameSize:  15,
				maxFrameSize:  15,
				sampleRate:    44100,
				channels:      2,
				bitsPerSample: 16,
				totalSamples:  1,
			},
		},
		{
			// Appendix D.2.3
			name:    "example_2",
			file:    "example_2.flac",
			wantMD5: "d5b0564975e98b8d8b930422757b8103",
			want: streamInfo{
				minBlockSize:  16,
				maxBlockSize:  16,
				minFrameSize:  23,
				maxFrameSize:  68,
				sampleRate:    44100,
				channels:      2,
				bitsPerSample: 16,
				totalSamples:  19,
			},
		},
		{
			// Appendix D.3.3。MD5はD.3.1のhex dumpにしか出てこない
			name:    "example_3",
			file:    "example_3.flac",
			wantMD5: "f8f9e396f5cbcfc6dc807f9977906b32",
			want: streamInfo{
				minBlockSize:  4096,
				maxBlockSize:  4096,
				minFrameSize:  31,
				maxFrameSize:  31,
				sampleRate:    32000,
				channels:      1,
				bitsPerSample: 8,
				totalSamples:  24,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fPath := filepath.Join("testdata", "flac-specification", tt.file)
			f, err := os.ReadFile(fPath)
			if err != nil {
				t.Fatalf("readStreamInfo() failed to read test file %s, err:%v", fPath, err)
			}
			b, err := hex.DecodeString(tt.wantMD5)
			if err != nil || len(b) != 16 {
				t.Fatalf("bad expected MD5 literal %q", tt.wantMD5)
			}
			want := tt.want
			copy(want.md5Sum[:], b)
			got, err := readStreamInfo(bytes.NewReader(f[8:]))
			if err != nil {
				t.Fatalf("readStreamInfo() error = %v", err)
			}
			if want != got {
				t.Errorf("readStreamInfo() mismatch want:%#v got:%#v", tt.want, got)
			}
		})
	}
}

var (
	validStreamInfoHeader = []byte{
		0x00, 0x00, 0x00, 0x22, // STREAMINFOのヘッダ
	}
	seekTableMetadata = []byte{
		0x03, 0x00, 0x00, 0x01, 0x00,
	}
	vorbisCommentMetadata = []byte{
		0x04, 0x00, 0x00, 0x01, 0x00,
	}
	validStreamInfoBytes = []byte{
		0x00, 0x10, // 最小ブロックサイズ: 16
		0xFF, 0xFF, // 最大ブロックサイズ: 65535
		0x00, 0x00, 0x00, // 最小フレームサイズ: 不明
		0xFF, 0xFF, 0xFF, // 最大フレームサイズ: 16777215
		0x12, 0x34, 0x4B, // サンプルレート: 0x12344 = 74564, チャンネル数: 0b101 + 1 = 6, bpsの最上位ビット: 1
		0x3A,                   // bps: 0b10011 + 1 = 20, totalSamplesの上位ニブル: 0xA
		0x00, 0x00, 0x00, 0x01, // totalSamplesの下位32ビット
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, // MD5
		0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
	}
)

func TestReadMetadata(t *testing.T) {
	wantStreamInfo := streamInfo{
		minBlockSize:  16,
		maxBlockSize:  65535,
		minFrameSize:  0,
		maxFrameSize:  16777215,
		sampleRate:    74564,
		channels:      6,
		bitsPerSample: 20,
		totalSamples:  0xA_0000_0001,
		md5Sum: [16]byte{
			0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
			0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
		},
	}
	tests := []struct {
		name    string
		input   []byte
		want    metadata
		wantErr error
	}{
		{"empty", nil, metadata{}, io.EOF},
		{"first metadata is not a streamInfo",
			[]byte{0x81, // padding
				0x00, 0x00, 0x22},
			metadata{}, errFirstBlockIsNotStreamInfo},
		{"length of streaminfo is not 34bytes",
			[]byte{0x80, // streamInfo
				0x00, 0x00,
				0x23, // 35バイト
			},
			metadata{}, errInvalidStremInfoLength},
		{"first metadata header is streamInfo and last metadata",
			slices.Concat([]byte{0x80, 0x00, 0x00, 0x22}, validStreamInfoBytes, []byte{}),
			metadata{
				streamInfo: wantStreamInfo,
			}, nil},
		{"duplicate streamInfo",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, validStreamInfoHeader),
			metadata{
				streamInfo: wantStreamInfo,
			}, errDuplicatedStreamInfo},
		{"duplicate seek table",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, seekTableMetadata, seekTableMetadata),
			metadata{
				streamInfo: wantStreamInfo,
			}, errDuplicatedSeekTable},
		{"duplicate vorbis comment",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, vorbisCommentMetadata, vorbisCommentMetadata),
			metadata{
				streamInfo: wantStreamInfo,
			}, errDuplicatedVorbisComment},
		{"skip other metadata",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, []byte{0x81, 0x00, 0x00, 0x01, 0x01}),
			metadata{
				streamInfo: wantStreamInfo,
			},
			nil,
		},
		{"doesn't reject Padding",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, []byte{0x81, 0x00, 0x00, 0x01, 0x01}),
			metadata{
				streamInfo: wantStreamInfo,
			},
			nil,
		},
		{"doesn't reject Application",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, []byte{0x82, 0x00, 0x00, 0x01, 0x01}),
			metadata{
				streamInfo: wantStreamInfo,
			},
			nil,
		},
		{"doesn't reject CueSheet",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, []byte{0x85, 0x00, 0x00, 0x01, 0x01}),
			metadata{
				streamInfo: wantStreamInfo,
			},
			nil,
		},
		{"doesn't reject Picture",
			slices.Concat(validStreamInfoHeader, validStreamInfoBytes, []byte{0x86, 0x00, 0x00, 0x01, 0x01}),
			metadata{
				streamInfo: wantStreamInfo,
			},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readMetadata(bytes.NewReader(tt.input))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("readMetadata() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if tt.want != got {
				t.Errorf("readMetadata() mismatch want:%#v go:%#v", tt.want, got)
			}
		})
	}
}

func TestReadMetadataDoesntRejectReserved(t *testing.T) {
	baseMetadata := slices.Concat(validStreamInfoHeader, validStreamInfoBytes)
	testMetaHeader := []byte{0x80, 0x00, 0x00, 0x01, 0x01}
	for i := 7; i <= 126; i++ {
		testMetaHeader[0] = 0x80 | byte(i)
		_, err := readMetadata(bytes.NewReader(slices.Concat(baseMetadata, testMetaHeader)))
		if err != nil {
			t.Errorf("readMetadata() rejected block type %d, err:%v", i, err)
		}
	}
}
