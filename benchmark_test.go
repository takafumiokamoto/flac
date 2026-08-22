package flac_test

import (
	"bytes"
	"github.com/takafumiokamoto/flac"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkTestFile(b *testing.B) {
	fileName := "01 - blocksize 4096.flac"
	f, err := os.ReadFile(filepath.Join("testdata/flac-test-files/subset", fileName))
	if err != nil {
		b.Fatalf("failed to read benchmark file:%s, err:%v", fileName, err)
	}
	for b.Loop() {
		_, err := flac.Decode(bytes.NewReader(f))
		if err != nil {
			b.Fatalf("decode error: %v", err)
		}
	}
}
