package flac

import (
	"errors"
	"fmt"
)

type subframeType uint8

const (
	subframeConstant subframeType = iota
	subframeVerbatim
	subframeFixed
	subframeLPC
)

type subframeHeader struct {
	typ            subframeType
	predictorOrder uint8
	wastedBits     uint64
}

func decodeSubframe(br *bitReader, bps uint8, blockSize uint16, dst []int64) error {
	if len(dst) != int(blockSize) {
		return fmt.Errorf("length of destination buffer must be equal to block size: destination buffer:%d, block size:%d", len(dst), int(blockSize))
	}
	h, err := readSubFrameHeader(br)
	if err != nil {
		return fmt.Errorf("failed to read subframe header: %w", err)
	}
	if h.wastedBits >= uint64(bps) {
		return fmt.Errorf("wasted bits must be smaller than bits per sample, wasted bits:%d, bits per sample:%d", h.wastedBits, bps)
	}
	bps -= uint8(h.wastedBits)
	switch h.typ {
	case subframeConstant:
		if err := decodeConstant(br, bps, blockSize, dst[:]); err != nil {
			return fmt.Errorf("failed to decode constant subframe: %w", err)
		}
	case subframeVerbatim:
		if err := decodeVerbatim(br, bps, blockSize, dst[:]); err != nil {
			return fmt.Errorf("failed to decode verbatim subframe: %w", err)
		}
	case subframeFixed:
		if err := decodeFixed(br, h.predictorOrder, bps, blockSize, dst[:]); err != nil {
			return fmt.Errorf("failed to decode fixed predictor subframe: %w", err)
		}
	case subframeLPC:
		if err := decodeLPC(br, h.predictorOrder, bps, blockSize, dst[:]); err != nil {
			return fmt.Errorf("failed to decode linear predictor subframe: %w", err)
		}
	default:
		return fmt.Errorf("invalid subframe type:%d", h.typ)
	}
	for i := range dst {
		dst[i] <<= h.wastedBits
	}
	return nil
}

func readSubFrameHeader(br *bitReader) (subframeHeader, error) {

	firstBit, err := br.readBits(1)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("failed to read subframe header: %w", err)
	}
	if firstBit == 1 {
		return subframeHeader{}, errors.New("first bit of subframe header must be 0")
	}
	sbf := subframeHeader{}

	typ, err := br.readBits(6)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("failed to read subframe header: %w", err)
	}

	switch v := typ; {
	case v == 0b000000:
		sbf.typ = subframeConstant
	case v == 0b000001:
		sbf.typ = subframeVerbatim
	case 0b000010 <= v && v <= 0b000111:
		return subframeHeader{}, fmt.Errorf("invalid subframe type (reserved):%b", v)
	case 0b001000 <= v && v <= 0b001100:
		sbf.typ = subframeFixed
		sbf.predictorOrder = uint8(v) - 8
	case 0b001101 <= v && v <= 0b011111:
		return subframeHeader{}, fmt.Errorf("invalid subframe type (reserved):%b", v)
	case 0b100000 <= v && v <= 0b111111:
		sbf.typ = subframeLPC
		sbf.predictorOrder = uint8(v) - 31
	}

	flg, err := br.readBits(1)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("failed to read flag in subframe header: %w", err)
	}
	if flg == 0 {
		return sbf, nil
	}

	unary, err := br.readUnary()
	if err != nil {
		return subframeHeader{}, fmt.Errorf("failed to read unary: %w", err)
	}
	sbf.wastedBits = unary + 1
	return sbf, nil

}

func decodeConstant(br *bitReader, bps uint8, blockSize uint16, dst []int64) error {
	s, err := br.readSigned(uint(bps))
	if err != nil {
		return fmt.Errorf("failed to read constant subframe: %w", err)
	}
	for i := range blockSize {
		dst[i] = s
	}
	return nil
}

func decodeVerbatim(br *bitReader, bps uint8, blockSize uint16, dst []int64) error {
	for i := range blockSize {
		s, err := br.readSigned(uint(bps))
		if err != nil {
			return fmt.Errorf("failed to read verbatim subframe: %w", err)
		}
		dst[i] = s
	}
	return nil
}

func decodeFixed(br *bitReader, order uint8, bps uint8, blockSize uint16, dst []int64) error {
	if blockSize <= uint16(order) {
		return fmt.Errorf("block size must be larger than prediction order: block size:%d, prediction order:%d", blockSize, order)
	}
	if order > 4 {
		return fmt.Errorf("invalid prediction order:%d", order)
	}
	for i := range int(order) {
		sample, err := br.readSigned(uint(bps))
		if err != nil {
			return fmt.Errorf("failed to read warm up samples: %w", err)
		}
		dst[i] = sample
	}
	err := decodeResidual(br, order, blockSize, dst[order:])
	if err != nil {
		return fmt.Errorf("failed to read residuals: %w", err)
	}
	for i := int(order); i < len(dst); i++ {
		var prediction int64
		switch order {
		case 0:
			prediction = 0
		case 1:
			// a(n-1)
			prediction = dst[i-1]
		case 2:
			// 2 * a(n-1) - a(n-2)
			prediction = 2*dst[i-1] - dst[i-2]
		case 3:
			// 3 * a(n-1) - 3 * a(n-2) + a(n-3)
			prediction = 3*dst[i-1] - 3*dst[i-2] + dst[i-3]
		case 4:
			// 4 * a(n-1) - 6 * a(n-2) + 4 * a(n-3) - a(n-4)
			prediction = 4*dst[i-1] - 6*dst[i-2] + 4*dst[i-3] - dst[i-4]
		}
		dst[i] = prediction + dst[i]
	}
	return nil
}

func decodeLPC(br *bitReader, order uint8, bps uint8, blockSize uint16, dst []int64) error {
	if blockSize <= uint16(order) {
		return fmt.Errorf("block size must be larger than prediction order: block size:%d, prediction order:%d", blockSize, order)
	}
	for i := range int(order) {
		sample, err := br.readSigned(uint(bps))
		if err != nil {
			return fmt.Errorf("failed to read warm up samples: %w", err)
		}
		dst[i] = sample
	}
	precision, err := br.readBits(4)
	if err != nil {
		return fmt.Errorf("failed to read precision: %w", err)
	}
	if precision == 0b1111 {
		return errors.New("invalid precision 0b1111")
	}
	precision += 1 // precisionは-1の値が格納されている
	shift, err := br.readSigned(5)
	if err != nil {
		return fmt.Errorf("failed to read shift: %w", err)
	}
	if shift < 0 {
		return fmt.Errorf("invalid shift:%d", shift)
	}
	var coefficients [32]int64 // orderは6bit符号のため1~32(§9.2.1)
	for i := range order {
		coefficient, err := br.readSigned(uint(precision))
		if err != nil {
			return fmt.Errorf("failed to read coefficient, index:%d: %w", i, err)
		}
		coefficients[i] = coefficient
	}
	err = decodeResidual(br, order, blockSize, dst[order:])
	if err != nil {
		return fmt.Errorf("failed to read residuals: %w", err)
	}
	for i := int(order); i < len(dst); i++ {
		// 論理シフトすると左に0が入って巨大な整数になってしまう。
		// 明示的に算術シフトをするためにint64
		var sum int64 = 0
		// cosの加法定理により、正弦波のある一点は直前の2点があれば求めることができる。
		// 実際の波は正弦波ではないが、LPCでは係数部分をサブフレームの信号ごとにエンコーダが決定する。
		// そうすることで実際の信号とのずれを抑えることができる。
		// 予測が当たるほどresidualが0に近づき、小さいビット数で表現できる。
		for j, c := range coefficients[:order] {
			sum += c * dst[i-1-j]
		}
		// 予測係数は小数点部分が左シフトされ整数になっている。
		// ここではシフト分を戻してからresidualを足す。
		dst[i] = (sum >> shift) + dst[i]
	}
	return nil
}

func decodeResidual(br *bitReader, predictorOrder uint8, blockSize uint16, dst []int64) error {
	// codingMethodは各パーティションのRICEパラメータのbit数を表す
	codingMethod, err := br.readBits(2)
	if err != nil {
		return fmt.Errorf("failed to read coding method in coded residual: %w", err)
	}
	var riceBits uint
	var escape uint8
	switch codingMethod {
	case 0b00:
		// 各パーティションの4bitがriceパラメータ
		riceBits = 4
		escape = 0b1111
	case 0b01:
		// 各パーティションの5bitがriceパラメータ
		riceBits = 5
		escape = 0b11111
	default:
		return fmt.Errorf("invalid coding method in coded residual:%b", codingMethod)
	}

	// partitionOrderはパーティション数の元になる数
	partitionOrder, err := br.readBits(4)
	if err != nil {
		return fmt.Errorf("failed to read partition order: %w", err)
	}

	// パーティション数は2 ^ partition orderで求まる。partition orderが3の場合は2 ^ 3 = 8 partitions
	partitionCount := 1 << partitionOrder
	// パーティションサイズはblockSize >> partition orderで求まる
	// BlockSizeはあらかじめ2^(partition order)で割り切れるようになっている
	partitionSize := blockSize >> partitionOrder

	if int(blockSize)%partitionCount != 0 {
		return fmt.Errorf("invalid partition order, block size should be divisible by partition count, partition order:%b, block size:%b", partitionOrder, blockSize)
	}

	if uint64(partitionSize) <= uint64(predictorOrder) {
		return fmt.Errorf("predictor order must be smaller than partition size, predictor order:%b, partition size:%b", predictorOrder, partitionSize)
	}

	n := 0
	for i := range partitionCount {
		residualCount := partitionSize
		if i == 0 {
			// warm-up sampleはパーティションの中ではなく、coded residualより前に格納されている（Table 21/22）。
			// パーティションはブロックの時間軸を等分するため、先頭パーティションの担当範囲のうち
			// 先頭predictorOrder個はwarm-upがカバー済みで、その分だけ残差の個数が少ない:
			// residualCount(パーティションサイズ) - predictorOrder(warm-up samples数)
			residualCount -= uint16(predictorOrder)
		}
		riceParam, err := br.readBits(riceBits)
		if err != nil {
			return fmt.Errorf("failed to read rice parameter: %w", err)
		}
		if riceParam == uint64(escape) {
			// 取得したRICE符号がescapeシーケンスと一致している場合はRICE符号として扱わない。
			// escapeの場合は次の5bitに残差の幅が記載されている
			residualWidth, err := br.readBits(5)
			if err != nil {
				return fmt.Errorf("failed to read residual width (escape sequence path), partition index:%d: %w", i, err)
			}
			// このパーティションの残りは生の残差として扱う。
			for j := range residualCount {
				residual, err := br.readSigned(uint(residualWidth))
				if err != nil {
					return fmt.Errorf("failed to read residual (escape sequence path), partition index:%d, residual index:%d: %w", i, j, err)
				}
				dst[n] = int64(residual)
				n++
			}
			continue
		}
		for j := range residualCount {
			// 商
			quotient, err := br.readUnary()
			if err != nil {
				return fmt.Errorf("failed to read quotient, partition index:%d, residual index:%d: %w", i, j, err)
			}
			// 余り
			remainder, err := br.readBits(uint(riceParam))
			if err != nil {
				return fmt.Errorf("failed to read remainder, partition index:%d, residual index:%d: %w", i, j, err)
			}
			// 商と余りを連結。riceParamとremainderは桁数が一致している
			folded := (quotient << riceParam) | remainder
			var residual int64
			if folded&1 == 0 {
				// zigzag符号化の場合で０以上の場合は末尾bitをシフトで消せば絶対値が求まる
				residual = int64(folded >> 1)
			} else {
				// 負の数の場合は以下の計算で求まる。
				// foldedが25の場合
				// 25 + 1 = 26 => これがfolded25の次の数（偶数）
				// 26 / 2 = 13 => 次の数の絶対値を求める
				// zigzag符号は奇数にマイナス、偶数にプラスが入っているので、-13の次は+13
				// そのため"次の正の数"の絶対値の符号を逆転すれば良い
				//
				// 2進数の場合は、末尾1bitをシフトすることで絶対値-1が求まる。
				// 25(0b11001)の場合は12(0b1100)が求まる
				// 手計算の「+1して符号を逆転」がビット反転1回で求まる（^x = -x-1）。÷2は上のシフトで済んでいる。
				// 以下25の場合の検算
				// 0b11001 >> 1 => 0b1100 (12)
				// 0b1100 (12) => 12は符号付き4bitに入らない（正の最大+7）ので、符号の席の0を足して5bitの0b01100にする
				// 全5bitを反転して 0b10011（先頭の0が1に裏返る）
				// 反転後の0b10011は -16 + 3 = -13
				// 答えが-13になり手計算と一致する。
				residual = ^int64(folded >> 1)
			}
			dst[n] = residual
			n++
		}
	}
	return nil
}
