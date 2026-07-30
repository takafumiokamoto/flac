package flac

import (
	"bufio"
	"errors"
	"io"
)

var (
	ErrFirstBlockIsNotStreamInfo = errors.New("flac: first metadata is not streaminfo")
	ErrInvalidStremInfoLength    = errors.New("flac: invalid streamInfoLength")
	ErrDuplicatedStreamInfo      = errors.New("flac: block type Streaminfo appears more than once.")
	ErrDuplicatedSeekTable       = errors.New("flac: block type SeekTable appears more than once.")
	ErrDuplicatedVorbisComment   = errors.New("flac: block type Vorbis Comment appears more than once.")
)

type Decoder struct {
	src  *bufio.Reader
	meta metadata
}

func NewDecoder(r io.Reader) (*Decoder, error) {
	// https://datatracker.ietf.org/doc/html/rfc9639#section-6
	if err := validateMarker(r); err != nil {
		return nil, err
	}
	meta, err := readMetadata(r)
	if err != nil {
		return nil, err
	}
	return &Decoder{
		src:  bufio.NewReader(r),
		meta: meta,
	}, nil
}

func (d *Decoder) Decode() ([]byte, error) {
	return nil, errors.New("not implemented")
}
