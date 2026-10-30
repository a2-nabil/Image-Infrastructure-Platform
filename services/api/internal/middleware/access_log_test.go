package middleware

import "testing"

func TestSanitizeForLogStripsNewlines(t *testing.T) {
	got := sanitizeForLog("ok\nfake_level=ERROR forged\r\n", 256)
	if got != "ok fake_level=ERROR forged" {
		t.Fatalf("unexpected sanitized value: %q", got)
	}
}

func TestSanitizeForLogTruncates(t *testing.T) {
	got := sanitizeForLog("abcdefghijklmnopqrstuvwxyz", 8)
	if len(got) > 8 {
		t.Fatalf("expected max 8 chars, got %q", got)
	}
}

func TestSanitizeForLogDropsControlChars(t *testing.T) {
	got := sanitizeForLog("a\x00b\tc", 256)
	if got != "ab c" {
		t.Fatalf("unexpected sanitized value: %q", got)
	}
}
