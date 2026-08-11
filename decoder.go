package flac

import (
	"bufio"
	"errors"
	"time"
)

var (
	ErrFirstBlockIsNotStreamInfo = errors.New("flac: first metadata is not streaminfo")
	ErrInvalidStremInfoLength    = errors.New("flac: invalid streamInfoLength")
	ErrDuplicatedStreamInfo      = errors.New("flac: block type Streaminfo appears more than once.")
	ErrDuplicatedSeekTable       = errors.New("flac: block type SeekTable appears more than once.")
	ErrDuplicatedVorbisComment   = errors.New("flac: block type Vorbis Comment appears more than once.")
)

type MetaData struct {
	Name   string
	Length time.Duration
}

type PCM struct {
	MetaData
	Data []byte
}

func Decode(r bufio.Reader) (PCM, error) {
	return PCM{}, errors.New("not implemented")
}
