package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileWriterBoundsLogFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded.log")
	writer, err := newRotatingFileWriter(path, 32, 2)
	if err != nil {
		t.Fatalf("create rotating writer: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	for index := 0; index < 10; index++ {
		if _, err := writer.Write([]byte(strings.Repeat(string(rune('a'+index)), 12))); err != nil {
			t.Fatalf("write log chunk %d: %v", index, err)
		}
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatalf("stat rotated log %s: %v", candidate, err)
		}
		if info.Size() > 32 {
			t.Fatalf("rotated log exceeds bound: %s size=%d", candidate, info.Size())
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected log permissions for %s: %o", candidate, info.Mode().Perm())
		}
	}
}
