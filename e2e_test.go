package flac_test

import (
	"github.com/takafumiokamoto/flac"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDecoderSubset(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/flac-test-files/subset/*.flac")
	const fileCount = 64
	if len(files) != fileCount {
		t.Fatalf("missing subset files, want:%d, got:%d", fileCount, len(files))
	}
	if err != nil {
		t.Fatalf("failed to glob flac files:%v", err)
	}
	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			run(t, fileName, true)
		})
	}
}

func TestDecoderFaulty(t *testing.T) {
	t.Parallel()
	// 確認観点はpanicまたはfreezeしないこと
	files, err := filepath.Glob("testdata/flac-test-files/faulty/*.flac")
	const fileCount = 11
	if len(files) != fileCount {
		t.Fatalf("missing faulty files, want:%d, got:%d", fileCount, len(files))
	}
	if err != nil {
		t.Fatalf("failed to glob flac files:%v", err)
	}
	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			run(t, fileName, false)
		})
	}
}

func TestDecoderUncommon(t *testing.T) {
	t.Parallel()
	// 確認観点は10, 11を除く全てがデコードできること(10, 11はfLaCマーカーなしで始まるため対象外)
	files, err := filepath.Glob("testdata/flac-test-files/uncommon/*.flac")
	const fileCount = 11
	if len(files) != fileCount {
		t.Fatalf("missing uncommon files, want:%d, got:%d", fileCount, len(files))
	}
	if err != nil {
		t.Fatalf("failed to glob flac files:%v", err)
	}
	for _, fileName := range files {
		t.Run(fileName, func(t *testing.T) {
			switch filepath.Base(fileName) {
			case "10 - file starting at frame header.flac", "11 - file starting with unparsable data.flac":
				t.Skipf("known limitation, skipped %s", fileName)
			}
			run(t, fileName, true)
		})
	}
}

func run(t *testing.T, fileName string, failOnErr bool) {
	t.Helper()
	f, err := os.Open(fileName)
	if err != nil {
		t.Fatalf("failed to read :%s, err:%v", fileName, err)
	}
	defer func() {
		_ = f.Close()
	}()
	dec, err := flac.NewDecoder(f)
	if err != nil {
		if failOnErr {
			t.Errorf("test bench failed on decoding metadata: file name:%s, err :%v", fileName, err)
			return
		}
		t.Logf("decoder rejects %s in metadata, err:%v", fileName, err)
		return
	}
	_, err = io.Copy(io.Discard, dec)
	if err != nil {
		if failOnErr {
			t.Errorf("test bench failed: file name:%s, err :%v", fileName, err)
			return
		}
		t.Logf("decoder rejects :%s on decoding, err:%v", fileName, err)
	}
}
