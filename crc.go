package flac

const (
	// x^8 + x^2 + x^1 + x^0 のうち、最高位の x^8 はシフトで溢れる最上位ビットが担うので、
	// 定数には残りの x^2 + x^1 + x^0 = 0b0000_0111 だけを持つ。
	poly8 uint8 = 0x07
	// x^16 + x^15 + x^2 + x^0 のうち、最高位の x^16 はシフトで溢れる最上位ビットが担うので、
	// 定数には残りの x^15 + x^2 + x^0 = 0b1000_0000_0000_0101 だけを持つ。
	poly16 uint16 = 0x8005
)

var crc16Table [256]uint16 = makeCRC16Table()

func makeCRC16Table() [256]uint16 {
	// crc16の事前計算
	// CRCは8bitずつ計算するので、2^8 = 256通りを先に計算しておく。
	var tb [256]uint16
	for i := range tb {
		tb[i] = crc16UpdateBit(0, byte(i))
	}
	return tb
}

// crc8 computes the CRC-8.
// polynomial is x^8 + x^2 + x^1 + x^0, initialized with 0.
//
// Frame Header CRC: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.1.8
func crc8(src []byte) uint8 {
	var crc uint8 // 初期値 0
	for _, b := range src {
		crc = crc8Update(crc, b)
	}
	return crc
}

func crc8Update(crc uint8, b byte) uint8 {
	// 次のバイトを取り込む。crc が 8 ビット幅なので、そのまま XOR すれば最上位バイトに載る。
	crc ^= b
	// 8 ビットぶん、筆算の割り算を進める。
	// 最上位ビットが 1 なら「割れる」ので 1 桁ずらして多項式を引く(XOR)、0 なら 1 桁ずらすだけ。
	for range 8 {
		if crc&0x80 != 0 {
			crc = crc<<1 ^ poly8
		} else {
			crc <<= 1
		}
	}
	return crc
}

// crc16 computes the CRC-16.
// polynomial is x^16 + x^15 + x^2 + x^0, initialized with 0.
//
// Frame Footer: https://www.rfc-editor.org/rfc/rfc9639.html#section-9.3
func crc16(src []byte) uint16 {
	var crc uint16 // 初期値 0
	for _, b := range src {
		crc = crc16Update(crc, b)
	}
	return crc
}

func crc16UpdateBit(crc uint16, b byte) uint16 {
	// 次のバイトを取り込む。crc が 16 ビット幅なので、上位バイトに載せてから XOR する。
	crc ^= uint16(b) << 8
	// 以下は crc8 と同じ筆算。判定する最上位ビットが 0x8000 になる。
	for range 8 {
		if crc&0x8000 != 0 {
			crc = crc<<1 ^ poly16
		} else {
			crc <<= 1
		}
	}
	return crc
}

func crc16Update(crc uint16, b byte) uint16 {
	// crc16UpdateBitの
	//for range 8 {
	//		if crc&0x8000 != 0 {
	//			crc = crc<<1 ^ poly16
	//		} else {
	//			crc <<= 1
	//		}
	//	}
	// の部分を事前計算してテーブルから引いている
	//
	// このループに入る前は
	// crc ^= uint16(b) << 8
	// をしているが、ここではcrc16Tableが上位8ビット分は事前に計算して配列に格納している。
	// ここでは上位8bitをシフト降ろしてから上記と同じようにXORをしてテーブルから引く
	return crc<<8 ^ crc16Table[byte(crc>>8)^b]
}
