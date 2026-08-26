package flac

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestReadSubFrameHeader(t *testing.T) {
	type want struct {
		header subframeHeader
		err    bool
	}
	tests := []struct {
		name string
		in   []byte
		want
	}{
		{
			"verbatim with 2 wasted bits (Appendix D.1.4)",
			[]byte{0x03, 0x58},
			want{subframeHeader{typ: subframeVerbatim, wastedBits: 2}, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := newBitReader(bytes.NewReader(tt.in))
			got, err := readSubFrameHeader(br)
			if !tt.err && err != nil {
				t.Fatalf("readSubFrameHeader() unexpected error: %v", err)
			}
			if tt.err {
				if err == nil {
					t.Fatal("readSubFrameHeader() expected an error, got none")
				}
				t.Logf("expected err:%v", err)
				return
			}
			if got != tt.header {
				t.Errorf("readSubFrameHeader(), want:%+v, got:%+v", tt.header, got)
			}
		})
	}
}

func TestDecodeResidual(t *testing.T) {
	appendixD2 := `
00000088: 11111111 11111000 01101001 10011000  ..i.
0000008c: 00000000 00001111 10011001 00010010  ....
00000090: 00001000 01100111 00000001 01100010  .g.b
00000094: 00111101 00010100 01000010 10011001  =.B.
00000098: 10001111 01011101 11110111 00001101  .]..
0000009c: 01101111 11100000 00001100 00010111  o...
000000a0: 11001010 11101011 00100001 00000000  ..!.
000000a4: 00001110 11100111 10100111 01111010  ...z
000000a8: 00100100 10100001 01011001 00001100  $.Y.
000000ac: 00010010 00010111 10110110 00000011  ....
000000b0: 00001001 01111011 01111000 01001111  .{xO
000000b4: 10101010 10011010 00110011 11010010  ..3.
000000b8: 10000101 11100000 01110000 10101101  ..p.
000000bc: 01011011 00011011 01001000 01010001  [.HQ
000000c0: 10110100 00000001 00001101 10011001  ....
000000c4: 11010010 11001101 00011010 01101000  ...h
000000c8: 11110001 11100110 10111000 00010000  ....
000000cc: 11111111 11111000 01101001 00011000  ..i.
000000d0: 00000001 00000010 10100100 00000010  ....
000000d4: 11000011 10000010 11000100 00001011  ....
000000d8: 11000001 01001010 00000011 11101110  .J..
000000dc: 01001000 11011101 00000011 10110110  H...
000000e0: 01111100 00010011 00110000           |.0
	`
	appendixD3 := `
0000002a: 11111111 11111000 01101000 00000010  ..h.
0000002e: 00000000 00010111 11101001 01000100  ...D
00000032: 00000000 01001111 01101111 00110001  .Oo1
00000036: 00111101 00010000 01000111 11010010  =.G.
0000003a: 00100111 11001011 01101101 00001001  '.m.
0000003e: 00001000 00110001 01000101 00101011  .1E+
00000042: 11011100 00101000 00100010 00100010  .(""
00000046: 10000000 01010111 10100011           .W.
	`
	tests := []struct {
		name      string
		in        []byte
		skip      uint
		order     uint8
		blocksize uint16
		want      []int64
	}{
		// coded residualは0x92+1から。dumpは0x88始まりなのでスキップは(0x92-0x88)*8+1ビット。
		// fixed 1st order, block size 16。期待値はTable 39のResidual Sample Value列。
		{"Appendix D.2", mustBinDump(t, appendixD2), (0x92-0x88)*8 + 1, 1, 16,
			[]int64{3194, -1297, 1228, -943, 952, -696, 768, -524, 599, -401, -13172, -316, 274, -267, 134}},

		// coded residualは0x37+5から。dumpは0x2a始まりなのでスキップは(0x37-0x2a)*8+5ビット。
		// LPC 3rd order, block size 24。期待値はTable 49のResidual列(warm-up 3行は含まない)。
		{"Appendix D.3.2", mustBinDump(t, appendixD3), (0x37-0x2a)*8 + 5, 3, 24,
			[]int64{3, -1, -13, -10, -6, 2, 8, 8, 6, 0, -3, -5, -4, -1, 1, 1, 4, 2, 2, 2, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := newBitReader(bytes.NewReader(tt.in))
			for range tt.skip {
				_, err := br.readBits(1)
				if err != nil {
					t.Fatalf("decodeResidual() failed to discard skip bits: %v", err)
				}
			}
			got := make([]int64, int(tt.blocksize)-int(tt.order))
			err := decodeResidual(br, tt.order, tt.blocksize, got[:])
			if err != nil {
				t.Fatalf("decodeResidual() failed to discard skip bits: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("decodeResidual() want:%v, got:%v", tt.want, got)
			}
		})
	}
}

func mustBinDump(t *testing.T, dump string) []byte {
	t.Helper()
	var out []byte
	for tok := range strings.FieldsSeq(dump) {
		if len(tok) != 8 || strings.Trim(tok, "01") != "" { // Trimの第2引数はcutSetなので0と1が対象
			continue
		}
		bin, err := strconv.ParseUint(tok, 2, 8)
		if err != nil {
			t.Fatalf("failed to convert hex dump: tok:%s, err:%v", tok, err)
		}
		out = append(out, byte(bin))
	}
	return out
}
