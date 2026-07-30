package flac

import (
	"errors"
	"fmt"
	"io"
)

type metadata struct {
	// Only streamInfo is needed for now.
	streamInfo streamInfo
}

func readMetadata(r io.Reader) (metadata, error) {
	firstMetaHeader, err := readMetadataBlockHeader(r)
	if err != nil {
		return metadata{}, err
	}
	if firstMetaHeader.blockType != metadataBlockTypeStreamInfo {
		return metadata{}, fmt.Errorf("%w, got type %d",
			ErrFirstBlockIsNotStreamInfo, firstMetaHeader.blockType)
	}
	if firstMetaHeader.length != streamInfoLength {
		return metadata{}, fmt.Errorf("%w, length %d, want %d",
			ErrInvalidStremInfoLength, firstMetaHeader.length, streamInfoLength)
	}
	st, err := readStreamInfo(r)
	if err != nil {
		return metadata{}, err
	}
	meta := metadata{
		streamInfo: st,
	}
	if firstMetaHeader.isLast {
		return meta, nil
	}
	var (
		typeSeekTableExists     = false
		typeVorbisCommentExists = false
	)
	for {
		metaHeader, err := readMetadataBlockHeader(r)
		if err != nil {
			return metadata{}, err
		}
		switch metaHeader.blockType {
		case metadataBlockTypeStreamInfo:
			return metadata{}, ErrDuplicatedStreamInfo
		case metadataBlockTypeSeekTable:
			if typeSeekTableExists {
				return metadata{}, ErrDuplicatedSeekTable
			}
			typeSeekTableExists = true
		case metadataBlockTypeVorbisComment:
			if typeVorbisCommentExists {
				return metadata{}, ErrDuplicatedVorbisComment
			}
			typeVorbisCommentExists = true
		default:
		}
		// skip all metadata except for streamInfo.
		if err := skipMetadata(r, metaHeader); err != nil {
			return metadata{}, err
		}
		if metaHeader.isLast {
			break
		}
	}
	return meta, nil
}

func skipMetadata(r io.Reader, metadataHeader metadataBlockHeader) error {
	if _, err := io.CopyN(io.Discard, r, int64(metadataHeader.length)); err != nil {
		return fmt.Errorf("flac: failed to skip metadata block (type %d): %w", metadataHeader.blockType, err)
	}
	return nil
}

// validateMarker validates the FLAC marker, "fLaC"
func validateMarker(r io.Reader) error {
	var wantMarker = [4]byte{'f', 'L', 'a', 'C'}
	var buf = [4]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return fmt.Errorf("flac: failed to validate marker (fLaC): %w", err)
	}
	if wantMarker != buf {
		return fmt.Errorf("flac: failed to validate marker, got: % X", buf)
	}
	return nil
}

// streamInfoLength is the fixed size of a STREAMINFO metadata block: 272 bits = 34 bytes.
const streamInfoLength = 34

// readStreamInfo reads STREAMINFO metadata.
// The streaminfo block contains information about the whole stream, such as
// the sample rate, the number of channels, and the total number of interchannel samples.
// For more information, see:
// https://datatracker.ietf.org/doc/html/rfc9639#name-streaminfo
func readStreamInfo(r io.Reader) (streamInfo, error) {
	var buf = [streamInfoLength]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return streamInfo{}, fmt.Errorf("flac: failed to read Streaminfo: %w", err)
	}
	minBlockSize := uint16(buf[0])<<8 | uint16(buf[1])
	maxBlockSize := uint16(buf[2])<<8 | uint16(buf[3])
	minFrameSize := uint32(buf[4])<<16 | uint32(buf[5])<<8 | uint32(buf[6])
	maxFrameSize := uint32(buf[7])<<16 | uint32(buf[8])<<8 | uint32(buf[9])
	sampleRate := (uint32(buf[10])<<16 | uint32(buf[11])<<8 | uint32(buf[12])) >> 4
	// channels occupies bits 100-102 (byte 12 plus 4 bits), stored as (number of channels)-1.
	// buf[12] holds bits 96-103: shift out the trailing bps bit, then mask the low 3 bits.
	channels := ((buf[12] >> 1) & 0x07) + 1
	// bitsPerSample occupies bits 103-107, spanning bytes 12-13, stored as (bits per sample)-1.
	// The top bit is the last bit of buf[12] (mask 0x01), lifted 4 places to make room for
	// the low 4 bits, which are the high nibble of buf[13] (the shift discards the rest).
	bitsPerSample := ((buf[12]&1)<<4 | buf[13]>>4) + 1
	// totalSamples occupies bits 108-143, spanning bytes 13-17.
	// buf[13] starts at bit 104.
	// buf[17] covers bits 136 to 143.
	totalSamples := uint64(buf[13]&0x0f)<<32 | uint64(buf[14])<<24 | uint64(buf[15])<<16 | uint64(buf[16])<<8 | uint64(buf[17])
	var md5Sum = [16]byte{}
	copy(md5Sum[:], buf[18:34])
	st := streamInfo{
		minBlockSize:  minBlockSize,
		maxBlockSize:  maxBlockSize,
		minFrameSize:  minFrameSize,
		maxFrameSize:  maxFrameSize,
		sampleRate:    sampleRate,
		channels:      channels,
		bitsPerSample: bitsPerSample,
		totalSamples:  totalSamples,
		md5Sum:        md5Sum,
	}
	if err := validateStreamInfo(st); err != nil {
		return streamInfo{}, err
	}
	return st, nil
}

func validateStreamInfo(s streamInfo) error {
	// The minimum block size should be within 16...65535, but the maximum value
	// of uint16 is exactly 65535, so only the lower bound is checked.
	if s.minBlockSize < 16 {
		return fmt.Errorf("flac: minimum block size in streaminfo isn't in 16-65535 range: %d", s.minBlockSize)
	}
	// The maximum block size should be within 16...65535, but the maximum value
	// of uint16 is exactly 65535, so only the lower bound is checked.
	if s.maxBlockSize < 16 {
		return fmt.Errorf("flac: maximum block size in streaminfo isn't in 16-65355 range: %d", s.maxBlockSize)
	}
	if s.maxBlockSize < s.minBlockSize {
		return fmt.Errorf("flac: minimum block size is greater than maximum block size: min:%d, max:%d", s.minBlockSize, s.maxBlockSize)
	}
	return nil
}

type streamInfo struct {
	minBlockSize  uint16
	maxBlockSize  uint16
	minFrameSize  uint32
	maxFrameSize  uint32
	sampleRate    uint32
	channels      uint8
	bitsPerSample uint8
	totalSamples  uint64
	md5Sum        [16]byte
}

type metadataBlockType uint8

const (
	metadataBlockTypeStreamInfo    metadataBlockType = 0
	metadataBlockTypePadding       metadataBlockType = 1
	metadataBlockTypeApplication   metadataBlockType = 2
	metadataBlockTypeSeekTable     metadataBlockType = 3
	metadataBlockTypeVorbisComment metadataBlockType = 4
	metadataBlockTypeCueSheet      metadataBlockType = 5
	metadataBlockTypePicture       metadataBlockType = 6
	metadataBlockTypeForbidden     metadataBlockType = 127
)

type metadataBlockHeader struct {
	isLast    bool
	blockType metadataBlockType
	length    uint32
}

// readMetadataBlockHeader reads the header of a metadata block.
// For more information, see:
// https://datatracker.ietf.org/doc/html/rfc9639#name-metadata-block-header
func readMetadataBlockHeader(r io.Reader) (metadataBlockHeader, error) {
	var buf = [4]byte{}
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return metadataBlockHeader{},
			fmt.Errorf("flac: failed to read header of metadata block: %w", err)
	}
	blockType := metadataBlockType(buf[0] & 0x7f)
	h := metadataBlockHeader{
		isLast:    buf[0]&0x80 != 0,
		blockType: blockType,
		// The last three bytes of the metadata header encode the payload length as a 24-bit big-endian integer.
		length: uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]),
	}
	if h.blockType == metadataBlockTypeForbidden {
		return metadataBlockHeader{},
			errors.New("flac: block type 127 is invalid")
	}
	return h, nil
}
