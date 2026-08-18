package flac

import (
	"errors"
	"fmt"
	"io"
)

var (
	errBitReader = errors.New("flac: failed to read bits")
)

// bitReader reads a bit stream most significant bit first.
type bitReader struct {
	r         io.ByteReader
	acc       uint64 // アキュムレータ。取り込んだビットを左詰め(bit63側)で保持する
	cnt       uint   // 上位cntビットが有効(未消費)。fillは必要になるまで次のバイトを取り込まないので、readBitsの後は常に0〜7
	bytesRead uint   // 下のreaderから取り込んだバイト数。cnt > 0のとき最後の1バイトは読みかけ(消費し終えていない)
}

func newBitReader(r io.ByteReader) *bitReader {
	return &bitReader{
		r: r,
	}
}

// fill buffers bytes until at least n bits are available in the accumulator.
func (br *bitReader) fill(n uint) error {
	if n > 57 {
		return fmt.Errorf("%w: cannot read more than 57 bits at once, given:%d", errBitReader, n)
	}
	for br.cnt < n {
		b, err := br.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && br.cnt > 0 {
				return fmt.Errorf("%w: needed:[%d] bits but only [%d] bits are available. %w", errBitReader, n, br.cnt, io.ErrUnexpectedEOF)
			}
			return err
		}
		// 有効ビットはbit63からbit(64-cnt)までを占めているので、次のバイトはその直後のbit(63-cnt)〜bit(56-cnt)に置く。
		br.acc |= uint64(b) << (56 - br.cnt)
		br.bytesRead++
		br.cnt += 8
	}
	return nil
}

// readBits reads n bits from the underlying reader and returns them as the low n bits of the result.
func (br *bitReader) readBits(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if err := br.fill(n); err != nil {
		return 0, err
	}
	// 有効ビットは左詰めなので、要求されたnビットは上位nビット。
	v := br.acc >> (64 - n)
	br.acc <<= n
	br.cnt -= n
	return v, nil
}

// readSigned reads signed n bits
func (br *bitReader) readSigned(n uint) (int64, error) {
	bits, err := br.readBits(n)
	if err != nil {
		return 0, err
	}
	// 符号付きint64にキャストしてから左に64-nシフトして先頭をbit63に持っていく
	// その右に64-n"算術"シフトして戻す。
	// 算術シフトした場合はbit63が右にコピーされるので、先頭が1(マイナス)の場合でも符号を維持できる。
	return int64(bits<<(64-n)) >> (64 - n), nil
}

func (br *bitReader) readUnary() (uint64, error) {
	var unary int = 0
	for {
		b, err := br.readBits(1)
		if err != nil {
			return 0, err
		}
		if b == 1 {
			return uint64(unary), nil
		}
		unary++
	}
}
