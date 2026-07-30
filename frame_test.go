package flac

import (
	"testing"
)

func TestDecodeCodedNumber(t *testing.T) {
	type want struct {
		val       uint64
		readCount int
		err       bool
	}
	tests := []struct {
		name string
		in   []byte
		want
	}{
		{"first byte start with 0b10", []byte{0xBF, 0xFF}, want{0, 0, true}},
		{"first byte doesn't end with 0", []byte{0xF7, 0xFF}, want{0, 0, true}},
		{"second byte doesn't start with 0b10", []byte{0xC0, 0xFF}, want{0, 0, true}},
		{"1 bytes data", []byte{0x7F}, want{0b1111111, 1, false}},
		{"2 bytes data", []byte{0xC0, 0xBF}, want{0b111111, 2, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, readCount, err := decodeCodedNumber(tt.in)
			if !(tt.want.err) && err != nil {
				t.Fatalf("decodeCodedNumber() unexpected error: %v", err)
			}
			if err != nil {
				t.Logf("expected err:%v", err)
			}
			if val != tt.val {
				t.Errorf("decodeCodedNumber() val, want:%d, got:%d", tt.val, val)
			}
			if readCount != tt.readCount {
				t.Errorf("decodeCodedNumber() readCount, want:%d, got:%d", tt.readCount, readCount)
			}
		})
	}
}
