package flac

import (
	"testing"
)

func TestCrc8(t *testing.T) {
	// https://www.rfc-editor.org/rfc/rfc9639.html#name-audio-framesの表から抜粋
	in := []byte{0xff, 0xf8, 0x69, 0x18, 0x00, 0x00}
	const want uint8 = 0xbf
	got := crc8(in)
	if want != got {
		t.Errorf("crc8() want:%x, got:%x", want, got)
	}
}

func TestCrc16(t *testing.T) {
	// https://www.rfc-editor.org/rfc/rfc9639.html#name-example-file-1-in-hexadecimの表から抜粋
	// フレーム全体 0x2a〜0x38 のうち、末尾の CRC-16(0x37〜0x38 = aa 9a)を除いた 0x2a〜0x36 が入力。
	// フレームヘッダ(CRC-8 の 0xbf を含む)、サブフレーム 2 本、パディング(この例では 0 ビット)をすべて含む。
	in := []byte{
		0xff, 0xf8, 0x69, 0x18, 0x00, 0x00, 0xbf, // フレームヘッダ 0x2a〜0x30
		0x03, 0x58, 0xfd, // サブフレーム 0 (0x31〜0x33)
		0x03, 0x12, 0x8b, // サブフレーム 1 (0x34〜0x36)
	}
	const want uint16 = 0xaa9a
	got := crc16(in)
	if want != got {
		t.Errorf("crc16() want:%x, got:%x", want, got)
	}
}
