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

func readSubFrameHeader(br *bitReader) (subframeHeader, error) {

	firstBit, err := br.readBits(1)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("flac: failed to read Subframe Header: %w", err)
	}
	if firstBit == 1 {
		return subframeHeader{}, errors.New("flac: first bit of Subframe Header must be 0")
	}
	sbf := subframeHeader{}

	typ, err := br.readBits(6)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("flac: failed to read Subframe Header: %w", err)
	}

	switch v := typ; {
	case v == 0b000000:
		sbf.typ = subframeConstant
	case v == 0b000001:
		sbf.typ = subframeVerbatim
	case 0b000010 <= v && v <= 0b000111:
		return subframeHeader{}, fmt.Errorf("flac: invalid subframe type [Reserved]: %b", v)
	case 0b001000 <= v && v <= 0b001100:
		sbf.typ = subframeFixed
		sbf.predictorOrder = uint8(v) - 8
	case 0b001101 <= v && v <= 0b011111:
		return subframeHeader{}, fmt.Errorf("flac: invalid subframe type [Reserved]: %b", v)
	case 0b100000 <= v && v <= 0b111111:
		sbf.typ = subframeLPC
		sbf.predictorOrder = uint8(v) - 31
	}

	flg, err := br.readBits(1)
	if err != nil {
		return subframeHeader{}, fmt.Errorf("flac: failed to read flag in subframe header: %w", err)
	}
	if flg == 0 {
		return sbf, nil
	}

	unary, err := br.readUnary()
	if err != nil {
		return subframeHeader{}, fmt.Errorf("flac: failed to read unary: %w", err)
	}
	sbf.wastedBits = unary + 1
	return sbf, nil

}

func decodeConstant(br *bitReader, bps uint8, blockSize uint16) ([]int64, error) {
	s, err := br.readSigned(uint(bps))
	if err != nil {
		return nil, fmt.Errorf("flac: failed to read constant subframe:%w", err)
	}
	samples := make([]int64, blockSize)
	for i := range samples {
		samples[i] = s
	}
	return samples, nil
}

func decodeVerbatim(br *bitReader, bps uint8, blockSize uint16) ([]int64, error) {
	samples := make([]int64, blockSize)
	for i := range blockSize {
		s, err := br.readSigned(uint(bps))
		if err != nil {
			return nil, fmt.Errorf("flac: failed to read verbatim subframe:%w", err)
		}
		samples[i] = s
	}
	return samples, nil
}

func decodeResidual(br *bitReader, predictorOrder uint8, blockSize uint16) ([]int64, error) {
	// codingMethodは各パーティションのRICEパラメータのbit数を表す
	codingMethod, err := br.readBits(2)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to read coding method in coded residual: %w", err)
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
		return nil, fmt.Errorf("flac: invalid coding method in coded residual:%b", codingMethod)
	}

	// partitionOrderはパーティション数の元になる数
	partitionOrder, err := br.readBits(4)
	if err != nil {
		return nil, fmt.Errorf("flac: failed to read partition order: %w", err)
	}

	// パーティション数は2 ^ partition orderで求まる。partition orderが3の場合は2 ^ 3 = 8 partitions
	partitionCount := 1 << partitionOrder
	// パーティションサイズはblockSize >> partition orderで求まる
	// BlockSizeはあらかじめ2^(partition order)で割り切れるようになっている
	partitionSize := blockSize >> partitionOrder

	if int(blockSize)%partitionCount != 0 {
		return nil, fmt.Errorf("flac: invalid partiion order, block size should be divisible by partition count, partiion order:%b, blocksize:%b", partitionOrder, blockSize)
	}

	if uint64(partitionSize) <= uint64(predictorOrder) {
		return nil, fmt.Errorf("flac: partition order should be smaller than partition size, partiion order:%b, partition size:%b", partitionOrder, partitionSize)
	}

	residuals := make([]int64, 0, partitionCount*int(partitionSize))
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
			return nil, fmt.Errorf("flac: failed to read rice parameter: %w", err)
		}
		if riceParam == uint64(escape) {
			// 取得したRICE符号がescapeシーケンスと一致している場合はRICE符号として扱わない。
			// escapeの場合は次の5bitに残差の幅が記載されている
			residualWidth, err := br.readBits(5)
			if err != nil {
				return nil, fmt.Errorf("flac: failed to read residual witdth (escape sequeence path), partition index:%d, err%w", i, err)
			}
			// このパーティションの残りは生の残差として扱う。
			for j := range residualCount {
				residual, err := br.readSigned(uint(residualWidth))
				if err != nil {
					return nil, fmt.Errorf("flac: failed to read residual (escape sequeence path), partition index:%d, residual index:%d, err%w", i, j, err)
				}
				residuals = append(residuals, int64(residual))
			}
			continue
		}
		for j := range residualCount {
			// 商
			quotient, err := br.readUnary()
			if err != nil {
				return nil, fmt.Errorf("flac: failed to read quotient, partition index:%d, residual index:%d, err%w", i, j, err)
			}
			// 余り
			remainder, err := br.readBits(uint(riceParam))
			if err != nil {
				return nil, fmt.Errorf("flac: failed to read remainder, partition index:%d, residual index:%d, err%w", i, j, err)
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
			residuals = append(residuals, residual)
		}
	}
	return residuals, nil
}
