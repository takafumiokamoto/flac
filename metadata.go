package flac

import (
	"errors"
	"fmt"
	"io"
)

type metadata struct {
	// 今のところ必要なのはstreamInfoだけ。
	streamInfo streamInfo
}

var (
	errFirstBlockIsNotStreamInfo = errors.New("flac: first metadata is not streaminfo")
	errInvalidStremInfoLength    = errors.New("flac: invalid streamInfoLength")
	errDuplicatedStreamInfo      = errors.New("flac: block type Streaminfo appears more than once.")
	errDuplicatedSeekTable       = errors.New("flac: block type SeekTable appears more than once.")
	errDuplicatedVorbisComment   = errors.New("flac: block type Vorbis Comment appears more than once.")
)

func readMetadata(r io.Reader) (metadata, error) {
	firstMetaHeader, err := readMetadataBlockHeader(r)
	if err != nil {
		return metadata{}, err
	}
	if firstMetaHeader.blockType != metadataBlockTypeStreamInfo {
		return metadata{}, fmt.Errorf("%w, got type %d",
			errFirstBlockIsNotStreamInfo, firstMetaHeader.blockType)
	}
	if firstMetaHeader.length != streamInfoLength {
		return metadata{}, fmt.Errorf("%w, length %d, want %d",
			errInvalidStremInfoLength, firstMetaHeader.length, streamInfoLength)
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
			return metadata{}, errDuplicatedStreamInfo
		case metadataBlockTypeSeekTable:
			if typeSeekTableExists {
				return metadata{}, errDuplicatedSeekTable
			}
			typeSeekTableExists = true
		case metadataBlockTypeVorbisComment:
			if typeVorbisCommentExists {
				return metadata{}, errDuplicatedVorbisComment
			}
			typeVorbisCommentExists = true
		default:
		}
		// streamInfo以外のメタデータは読み飛ばす。
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
	// channelsはビット100-102(12バイト目+4ビット)を占め、(チャンネル数)-1で格納されている。
	// buf[12]はビット96-103を保持しているので、末尾のbpsのビットを右シフトで追い出してから下位3ビットをマスクする。
	channels := ((buf[12] >> 1) & 0x07) + 1
	// bitsPerSampleはビット103-107を占め、12-13バイト目にまたがる。(bits per sample)-1で格納されている。
	// 最上位ビットはbuf[12]の最後のビット(mask 0x01)。これを4つ左に持ち上げて下位4ビットの席を空け、
	// そこにbuf[13]の上位ニブルを入れる(右シフトで残りは捨てる)。
	// FIXME: フレームヘッダから参照されるので、bits per sampleが有効な値か検証したい。
	bitsPerSample := ((buf[12]&1)<<4 | buf[13]>>4) + 1
	// totalSamplesはビット108-143を占め、13-17バイト目にまたがる。
	// buf[13]はビット104から始まる。
	// buf[17]はビット136-143。
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
	// 最小ブロックサイズは16...65535の範囲であるべきだが、uint16の最大値がちょうど65535なので下限だけ検証する。
	if s.minBlockSize < 16 {
		return fmt.Errorf("flac: minimum block size in streaminfo isn't in 16-65535 range: %d", s.minBlockSize)
	}
	// 最大ブロックサイズも同様に、下限だけ検証する。
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
		// メタデータヘッダの末尾3バイトが、ペイロード長を24ビットのビッグエンディアン整数で表す。
		length: uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]),
	}
	if h.blockType == metadataBlockTypeForbidden {
		return metadataBlockHeader{},
			errors.New("flac: block type 127 is invalid")
	}
	return h, nil
}
