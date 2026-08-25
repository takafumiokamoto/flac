package benchmark

import (
	"bytes"
	"crypto/md5"
	mewkiz "github.com/mewkiz/flac"
	"github.com/takafumiokamoto/flac"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkThis(b *testing.B) {
	fileName := "01 - blocksize 4096.flac"
	f, err := os.ReadFile(filepath.Join("../testdata/flac-test-files/subset", fileName))
	if err != nil {
		b.Fatalf("failed to read benchmark file:%s, err:%v", fileName, err)
	}
	b.SetBytes(int64(len(f)))
	b.ReportAllocs()
	buf := make([]byte, 1024*32)
	for b.Loop() {
		dec, err := flac.NewDecoder(bytes.NewReader(f))
		if err != nil {
			b.Fatalf("failed to initialize decoder: %v", err)
		}
		for {
			_, err := dec.Read(buf)
			if err != nil {
				if err == io.EOF {
					break
				}
				b.Fatalf("failed to parse frame: %v", err)
			}
		}
	}
}

func BenchmarkMewkiz(b *testing.B) {
	fileName := "01 - blocksize 4096.flac"
	f, err := os.ReadFile(filepath.Join("../testdata/flac-test-files/subset", fileName))
	if err != nil {
		b.Fatalf("failed to read benchmark file:%s, err:%v", fileName, err)
	}
	b.SetBytes(int64(len(f)))
	b.ReportAllocs()
	for b.Loop() {
		stream, err := mewkiz.New(bytes.NewReader(f))
		if err != nil {
			b.Fatalf("failed to initialize mewkiz flac: %v", err)
		}
		md5sum := md5.New()
		for {
			frame, err := stream.ParseNext()
			if err != nil {
				if err == io.EOF {
					break
				}
				b.Fatalf("failed to parse frame: %v", err)
			}
			frame.Hash(md5sum)
		}
		if !bytes.Equal(md5sum.Sum(nil), stream.Info.MD5sum[:]) {
			b.Fatalf("md5 sum does not match: want:%X, got:%X", stream.Info.MD5sum[:], md5sum.Sum(nil))
		}
	}
}
