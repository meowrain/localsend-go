package handlers

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTruncatePreviewShortText(t *testing.T) {
	text := "hello world"
	if got := TruncatePreview(text); got != text {
		t.Fatalf("short text should be untouched, got %q", got)
	}
}

func TestTruncatePreviewExactLimit(t *testing.T) {
	text := strings.Repeat("a", MaxPreviewBytes)
	if got := TruncatePreview(text); got != text {
		t.Fatalf("text exactly at limit should be kept, got len %d", len(got))
	}
}

func TestTruncatePreviewLongText(t *testing.T) {
	text := strings.Repeat("a", MaxPreviewBytes+512)
	got := TruncatePreview(text)
	if len(got) != MaxPreviewBytes {
		t.Fatalf("expected %d bytes, got %d", MaxPreviewBytes, len(got))
	}
	if !strings.HasPrefix(text, got) {
		t.Fatal("preview should be a prefix of the original text")
	}
}

// 截断点落在多字节字符中间时应回退，保证不产生半个 rune。
func TestTruncatePreviewKeepsRuneBoundary(t *testing.T) {
	// 每个中文字符 3 字节，MaxPreviewBytes 落在某个字符内部
	text := strings.Repeat("中", MaxPreviewBytes)
	got := TruncatePreview(text)
	if len(got) >= MaxPreviewBytes+1 {
		t.Fatalf("preview exceeds limit: %d", len(got))
	}
	if len(got) != MaxPreviewBytes-(MaxPreviewBytes%3) {
		t.Fatalf("unexpected truncated length: %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated preview must stay valid UTF-8")
	}
	if strings.HasSuffix(got, "\ufffd") {
		t.Fatal("truncated preview must not contain replacement chars")
	}
}

func TestTruncatePreviewMultibyteBoundaryExact(t *testing.T) {
	// 4 字节的 emoji 连续排列，截断点应回退到 rune 边界
	text := strings.Repeat("😀", 400) // 1600 字节
	got := TruncatePreview(text)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8 after truncation: %q", got)
	}
	if len(got) > MaxPreviewBytes {
		t.Fatalf("preview exceeds limit: %d", len(got))
	}
	if !strings.HasPrefix(text, got) {
		t.Fatal("preview should be a prefix of the original text")
	}
}

func TestTextFileName(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	name := textFileName(ts)
	if name != "clipboard-20260102-030405.txt" {
		t.Fatalf("unexpected file name: %s", name)
	}
	if !strings.HasSuffix(name, ".txt") {
		t.Fatal("virtual text file must end with .txt so receivers preview it")
	}
}

func TestSha256OfBytes(t *testing.T) {
	// echo -n "abc" | sha256sum
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := sha256OfBytes([]byte("abc")); got != want {
		t.Fatalf("unexpected sha256: %s", got)
	}
}

func TestSendTextRejectsEmpty(t *testing.T) {
	for _, text := range []string{"", "  ", "\n\t"} {
		if err := SendText(text); err != ErrEmptyText {
			t.Fatalf("expected ErrEmptyText for %q, got %v", text, err)
		}
	}
}
