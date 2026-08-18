package flac_test

import (
	"bytes"
	"github.com/takafumiokamoto/flac"
	"os"
	"path/filepath"
	"testing"
)

func TestDecoderSubset(t *testing.T) {
	files, err := filepath.Glob("testdata/flac-test-files/subset/*.flac")
	const fileCount = 64
	if len(files) != fileCount {
		t.Errorf("missing subset files want:%d, got:%d", fileCount, len(files))
	}
	if err != nil {
		t.Fatalf("failed to glob flac files:%v", err)
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			// os.Openにするとループ内でdeferすることによりメモリ消費が増加する。
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("failed to read :%s, err:%v", f, err)
			}
			_, err = flac.Decode(bytes.NewReader(b))
			if err != nil {
				t.Errorf("test bench failed: file name:%s, err :%v", f, err)
			}
		})
	}
}

func TestDecoderFaulty(t *testing.T) {
	// 確認観点はpanicまたはfreezeしないこと
	files, err := filepath.Glob("testdata/flac-test-files/faulty/*.flac")
	const fileCount = 11
	if len(files) != fileCount {
		t.Errorf("missing subset files want:%d, got:%d", fileCount, len(files))
	}
	if err != nil {
		t.Fatalf("failed to glob flac files:%v", err)
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			// os.Openにするとループ内でdeferすることによりメモリ消費が増加する。
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("failed to read :%s, err:%v", f, err)
			}
			_, err = flac.Decode(bytes.NewReader(b))
			if err != nil {
				t.Logf("rejected: %v", err)
			}
		})
	}
}
