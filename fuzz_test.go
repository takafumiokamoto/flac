package flac_test

import (
	"bytes"
	"github.com/takafumiokamoto/flac"
	"os"
	"testing"
)

func FuzzDecode(f *testing.F) {
	for _, path := range []string{
		"testdata/flac-specification/example_1.flac",
		"testdata/flac-specification/example_2.flac",
		"testdata/flac-specification/example_3.flac",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("failed to read seed %s: %v", path, err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// panicしないことを確認
		_, _ = flac.Decode(bytes.NewReader(data))
	})
}
