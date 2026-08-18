package flac

// crc8 computes the CRC-8 of src as defined in RFC 9639 Section 9.1.8:
// polynomial x^8 + x^2 + x^1 + x^0, initialized with 0.
func crc8(src []byte) uint8 {
	// Frame Header CRC: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.8
	// x^8 + x^2 + x^1 + x^0 のうち、最高位の x^8 はシフトで溢れる最上位ビットが担うので、
	// 定数には残りの x^2 + x^1 + x^0 = 0b0000_0111 だけを持つ。
	const poly uint8 = 0x07
	var crc uint8 // 初期値 0
	for _, b := range src {
		// 次のバイトを取り込む。crc が 8 ビット幅なので、そのまま XOR すれば最上位バイトに載る。
		crc ^= b
		// 8 ビットぶん、筆算の割り算を進める。
		// 最上位ビットが 1 なら「割れる」ので 1 桁ずらして多項式を引く(XOR)、0 なら 1 桁ずらすだけ。
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// crc16 computes the CRC-16 of src as defined in RFC 9639 Section 9.3:
// polynomial x^16 + x^15 + x^2 + x^0, initialized with 0.
func crc16(src []byte) uint16 {
	// Frame Footer: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.3
	// x^16 + x^15 + x^2 + x^0 のうち、最高位の x^16 はシフトで溢れる最上位ビットが担うので、
	// 定数には残りの x^15 + x^2 + x^0 = 0b1000_0000_0000_0101 だけを持つ。
	const poly uint16 = 0x8005
	var crc uint16 // 初期値 0
	for _, b := range src {
		// 次のバイトを取り込む。crc が 16 ビット幅なので、上位バイトに載せてから XOR する。
		crc ^= uint16(b) << 8
		// 以下は crc8 と同じ筆算。判定する最上位ビットが 0x8000 になる。
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
