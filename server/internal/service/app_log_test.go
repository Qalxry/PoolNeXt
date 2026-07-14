package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeAppLogMessageNormalizesControlCharacters(t *testing.T) {
	got := sanitizeAppLogMessage("  login\nfailed\tuser=alice\x00  ")
	if got != "login failed user=alice" {
		t.Fatalf("unexpected sanitized message: %q", got)
	}
}

func TestSanitizeAppLogMessageTruncatesAtUTF8Boundary(t *testing.T) {
	got := sanitizeAppLogMessage(strings.Repeat("界", maxAppLogMessageBytes))
	if len(got) > maxAppLogMessageBytes {
		t.Fatalf("sanitized message exceeds byte limit: %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("sanitized message is not valid UTF-8")
	}
	if !strings.HasSuffix(got, appLogTruncatedSuffix) {
		t.Fatalf("truncated message lacks suffix: %q", got[len(got)-32:])
	}
}
