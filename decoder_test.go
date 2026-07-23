package flac

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(metadataBlockHeader{})); diff != "" {
				t.Errorf("readMetadataBlockHeader() mismatch (-want +got):\n%s", diff)
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
			// example_1.flac leaves the byte-crossing bits at 0 (bps top bit,
			// totalSamples high nibble), so this vector sets them.
			name: "byte-crossing bits set",
			input: []byte{
				0x00, 0x10, // min block size: 16
				0xFF, 0xFF, // max block size: 65535
				0x00, 0x00, 0x00, // min frame size: unknown
				0xFF, 0xFF, 0xFF, // max frame size: 16777215
				0x12, 0x34, 0x4B, // sample rate: 0x12344 = 74564, channels: 0b101 + 1 = 6, bps top bit: 1
				0x3A,                   // bps: 0b10011 + 1 = 20, totalSamples high nibble: 0xA
				0x00, 0x00, 0x00, 0x01, // totalSamples low 32 bits
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readStreamInfo(bytes.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("readStreamInfo() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(streamInfo{})); diff != "" {
				t.Errorf("readStreamInfo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadStreamInfoRealFile(t *testing.T) {
	fPath := filepath.Join("testdata", "flac-specification", "example_1.flac")
	f, err := os.ReadFile(fPath)
	if err != nil {
		t.Fatalf("readStreamInfo() failed to read test file %s, err:%v", fPath, err)
	}
	b, err := hex.DecodeString("3e84b41807dc690307586a3dad1a2e0f")
	if err != nil || len(b) != 16 {
		t.Fatalf("bad expected MD5 literal")
	}
	var wantMD5Sum = [16]byte{}
	copy(wantMD5Sum[:], b)
	want := streamInfo{
		minBlockSize:  4096,
		maxBlockSize:  4096,
		minFrameSize:  15,
		maxFrameSize:  15,
		sampleRate:    44100,
		channels:      2,
		bitsPerSample: 16,
		totalSamples:  1,
		md5Sum:        wantMD5Sum,
	}
	got, err := readStreamInfo(bytes.NewReader(f[8:]))
	if err != nil {
		t.Fatalf("readStreamInfo() error = %v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(streamInfo{})); diff != "" {
		t.Errorf("readStreamInfo() mismatch (-want +got):\n%s", diff)
	}
}
