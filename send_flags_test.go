package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseSendOptionsMutualExclusion(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string // 期望 err 包含的片段，空表示应当解析成功
	}{
		{name: "file path", args: []string{"a.txt"}},
		{name: "text", args: []string{"--text", "hello"}},
		{name: "clipboard", args: []string{"--clipboard"}},
		{name: "text and clipboard", args: []string{"--text", "hello", "--clipboard"}, wantErr: "互斥"},
		{name: "file and text", args: []string{"--text", "hello", "a.txt"}, wantErr: "互斥"},
		{name: "file and clipboard", args: []string{"--clipboard", "a.txt"}, wantErr: "互斥"},
		{name: "all three", args: []string{"--text", "hello", "--clipboard", "a.txt"}, wantErr: "互斥"},
		{name: "missing source", args: nil, wantErr: "需要指定来源"},
		{name: "empty text", args: []string{"--text", ""}, wantErr: "不能为空"},
		{name: "blank text", args: []string{"--text", "   \n"}, wantErr: "不能为空"},
		{name: "too many paths", args: []string{"a.txt", "b.txt"}, wantErr: "只接受一个文件路径"},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "not defined"},
		{name: "flag needs argument", args: []string{"--text"}, wantErr: "needs an argument"},
		{name: "duplicate text", args: []string{"--text", "a", "--text", "b"}, wantErr: "重复出现"},
		{name: "duplicate text equals form", args: []string{"--text=a", "--text=b"}, wantErr: "重复出现"},
		{name: "duplicate clipboard", args: []string{"--clipboard", "--clipboard"}, wantErr: "重复出现"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSendOptions(tc.args, io.Discard)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestSendOptionsSourceCount(t *testing.T) {
	opts, err := parseSendOptions([]string{"--text", "hi"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.SourceCount() != 1 {
		t.Fatalf("expected 1 source, got %d", opts.SourceCount())
	}
	if !opts.TextSet || opts.Text != "hi" {
		t.Fatalf("unexpected text options: %+v", opts)
	}
}

func TestSendOptionsResolve(t *testing.T) {
	fakeClipboard := func(content string, err error) func() (string, error) {
		return func() (string, error) { return content, err }
	}

	t.Run("explicit text is not trimmed", func(t *testing.T) {
		opts := sendOptions{Text: "  keep spaces  ", TextSet: true}
		text, isText, err := opts.resolve(fakeClipboard("", nil))
		if err != nil || !isText {
			t.Fatalf("unexpected result: text=%q isText=%v err=%v", text, isText, err)
		}
		if text != "  keep spaces  " {
			t.Fatalf("text should be sent verbatim, got %q", text)
		}
	})

	t.Run("clipboard content", func(t *testing.T) {
		opts := sendOptions{Clipboard: true}
		text, isText, err := opts.resolve(fakeClipboard("from clipboard", nil))
		if err != nil || !isText || text != "from clipboard" {
			t.Fatalf("unexpected result: text=%q isText=%v err=%v", text, isText, err)
		}
	})

	t.Run("clipboard read error", func(t *testing.T) {
		opts := sendOptions{Clipboard: true}
		_, _, err := opts.resolve(fakeClipboard("", errors.New("no clipboard")))
		if err == nil || !strings.Contains(err.Error(), "读取剪贴板失败") {
			t.Fatalf("expected clipboard error, got %v", err)
		}
	})

	t.Run("empty clipboard", func(t *testing.T) {
		opts := sendOptions{Clipboard: true}
		_, _, err := opts.resolve(fakeClipboard("  \n", nil))
		if err == nil || !strings.Contains(err.Error(), "剪贴板内容为空") {
			t.Fatalf("expected empty clipboard error, got %v", err)
		}
	})

	t.Run("file path", func(t *testing.T) {
		opts := sendOptions{FilePath: "a.txt"}
		text, isText, err := opts.resolve(fakeClipboard("", nil))
		if err != nil || isText || text != "" {
			t.Fatalf("unexpected result: text=%q isText=%v err=%v", text, isText, err)
		}
	})
}

// TestSendOptionsPeerPIN `send --pin` 是对端 PIN，与来源参数互不影响。
func TestSendOptionsPeerPIN(t *testing.T) {
	opts, err := parseSendOptions([]string{"--pin", " 654321 ", "a.txt"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.PeerPIN != "654321" {
		t.Fatalf("peer pin = %q, want 654321 (should be trimmed)", opts.PeerPIN)
	}
	if opts.FilePath != "a.txt" || opts.SourceCount() != 1 {
		t.Fatalf("peer pin must not affect source parsing: %+v", opts)
	}

	// 不带 --pin 时为空：不会凭空带上本机接收端的 PIN
	opts, err = parseSendOptions([]string{"a.txt"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.PeerPIN != "" {
		t.Fatalf("peer pin = %q, want empty", opts.PeerPIN)
	}

	// 只给 --pin 而不给来源仍然要报错
	if _, err := parseSendOptions([]string{"--pin", "123456"}, io.Discard); err == nil {
		t.Fatal("send --pin alone should fail: missing source")
	}

	// 重复给出直接拒绝
	if _, err := parseSendOptions([]string{"--pin", "1", "--pin", "2", "a.txt"}, io.Discard); err == nil {
		t.Fatal("duplicate --pin should be rejected")
	}
}
