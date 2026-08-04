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
	r   io.ByteReader
	acc uint64 // accumulator
	cnt uint   // the top cnt bits of the accumulator are valid
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
		// The valid bits end at bit 64-cnt, so the incoming byte occupies bits 63-cnt down to 56-cnt.
		br.acc |= uint64(b) << (56 - br.cnt)
		br.cnt += 8
	}
	return nil
}

// read reads n bits from the underlying reader and returns them as the low n bits of the result.
func (br *bitReader) read(n uint) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if err := br.fill(n); err != nil {
		return 0, err
	}
	// The valid bits are left aligned, so the requested bits are the top n bits.
	v := br.acc >> (64 - n)
	br.acc <<= n
	br.cnt -= n
	return v, nil
}
