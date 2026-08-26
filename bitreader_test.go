package flac

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestBitReaderRead(t *testing.T) {
	tests := []struct {
		name    string
		bitBuf  []byte
		in      uint
		want    uint64
		wantErr error
	}{
		{"byte boundary", []byte{0x1C}, 8, 28, nil},
		{"stride byte boundary", []byte{0x1C, 0x80}, 9, 57, nil},
		{"truncated read", []byte{0x1C, 0x80}, 20, 0, io.ErrUnexpectedEOF},
		{"read by 0", []byte{0x1C, 0x80}, 0, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := newBitReader(bytes.NewReader(tt.bitBuf))
			got, err := br.readBits(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("bitReader#read(%d), want err:%v, got err:%v", tt.in, tt.wantErr, err)
			}
			if tt.wantErr != nil {
				t.Logf("expected err:%v", err)
				return
			}
			if got != tt.want {
				t.Errorf("bitReader#read(%d), want:%d, got:%d", tt.in, tt.want, got)
			}
		})
	}
}

func TestBitReaderReadMaxBits(t *testing.T) {
	bitBuf := []byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x81}

	t.Run("57 bits are readable", func(t *testing.T) {
		br := newBitReader(bytes.NewReader(bitBuf))

		got, err := br.readBits(57)
		if err != nil {
			t.Fatalf("read(57), err:%v", err)
		}
		if want := uint64(1)<<56 | 1; got != want {
			t.Fatalf("read(57), want:%d, got:%d", want, got)
		}

		got, err = br.readBits(7)
		if err != nil {
			t.Fatalf("read(7), err:%v", err)
		}
		if want := uint64(0b0000001); got != want {
			t.Errorf("read(7), want:%d, got:%d", want, got)
		}
	})

	t.Run("58 bits are rejected", func(t *testing.T) {
		br := newBitReader(bytes.NewReader(bitBuf))

		_, err := br.readBits(58)
		if !errors.Is(err, errBitReader) {
			t.Fatalf("read(58), want err:%v, got err:%v", errBitReader, err)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("read(58), err must not be an EOF error, got err:%v", err)
		}
		t.Logf("expected err:%v", err)
	})
}

func TestBitReaderEOF(t *testing.T) {
	tests := []struct {
		name    string
		bitBuf  []byte
		consume uint  // 入力が尽きる読み取りの前に消費しておくビット数
		in      uint  // 入力が尽きる読み取りで要求するビット数
		wantErr error // 読み取りが返すべきエラー
		notErr  error // 混同してはいけないエラー(errors.Isで偽になること)
	}{
		{"empty input", []byte{}, 0, 8, io.EOF, io.ErrUnexpectedEOF},
		{"input ends on a byte boundary", []byte{0x1C}, 8, 1, io.EOF, io.ErrUnexpectedEOF},
		{"buffered bits are left over", []byte{0x1C}, 4, 8, io.ErrUnexpectedEOF, io.EOF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := newBitReader(bytes.NewReader(tt.bitBuf))
			if _, err := br.readBits(tt.consume); err != nil {
				t.Fatalf("read(%d), err:%v", tt.consume, err)
			}
			_, err := br.readBits(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("read(%d), want err:%v, got err:%v", tt.in, tt.wantErr, err)
			}
			if errors.Is(err, tt.notErr) {
				t.Errorf("read(%d), err must not match %v, got err:%v", tt.in, tt.notErr, err)
			}
			t.Logf("expected err:%v", err)
		})
	}
}

func TestBitReaderConsecutiveRead(t *testing.T) {
	bitBuf := bytes.NewReader([]byte{0b10001100, 0b11110000})
	tests := []struct {
		in   uint
		want uint64
	}{
		{1, 1},
		{5, 3},
		{3, 1},
	}
	r := newBitReader(bitBuf)
	for i, tt := range tests {
		t.Run(fmt.Sprintf("step:%d", i+1), func(t *testing.T) {
			got, err := r.readBits(tt.in)
			if err != nil {
				t.Errorf("read(), err:%v", err)
			}
			if got != tt.want {
				t.Errorf("read(), want:%d, got:%d", tt.want, got)
			}
		})
	}
}

func TestReadSigned(t *testing.T) {
	br := newBitReader(bytes.NewReader([]byte{0b11100000}))
	var want int64 = -1
	got, err := br.readSigned(3)
	if err != nil {
		t.Fatalf("readSigned() err:%v", err)
	}
	if want != got {
		t.Fatalf("readSigned() want:%d, got:%d", want, got)
	}
}

func TestUnary(t *testing.T) {
	br := newBitReader(bytes.NewReader([]byte{0b00000001}))
	var want uint64 = 7
	got, err := br.readUnary()
	if err != nil {
		t.Fatalf("readUnary() err:%v", err)
	}
	if want != got {
		t.Fatalf("readUnary(), want:%d, got:%d", want, got)
	}
}
