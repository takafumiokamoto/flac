package flac_test

import (
	"github.com/takafumiokamoto/flac"
	"io"
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
	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			f, err := os.Open(fileName)
			if err != nil {
				t.Fatalf("failed to read :%s, err:%v", fileName, err)
			}
			defer func() {
				_ = f.Close()
			}()
			dec, err := flac.NewDecoder(f)
			if err != nil {
				t.Fatalf("failed to initialize decoder: err:%v", err)
			}
			_, err = io.Copy(io.Discard, dec)
			if err != nil {
				t.Errorf("test bench failed: file name:%s, err :%v", fileName, err)
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
	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			f, err := os.Open(fileName)
			if err != nil {
				t.Fatalf("failed to read :%s, err:%v", fileName, err)
			}
			defer func() {
				_ = f.Close()
			}()
			dec, err := flac.NewDecoder(f)
			if err != nil {
				t.Logf("decoder rejects %s in metadata, err:%v", fileName, err)
				return
			}
			_, err = io.Copy(io.Discard, dec)
			if err != nil {
				t.Logf("decoder rejects :%s on decoding, err:%v", fileName, err)
				return
			}
		})
	}
}
