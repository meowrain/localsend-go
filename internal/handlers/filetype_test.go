package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileMetadataUsesMimeType 覆盖验收标准里的「fileType 改为 MIME」：
// 发送链路的 prepare 声明必须是 image/png 这类 MIME，而不是 ".png"。
func TestFileMetadataUsesMimeType(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"picture.png": "image/png",
		"clip.MP4":    "video/mp4",
		"notes.txt":   "text/plain",
		"mystery.qqq": fallbackMimeType,
		"noextension": fallbackMimeType,
	}
	for name, want := range cases {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		got, err := fileMetadata(path, info)
		if err != nil {
			t.Fatalf("fileMetadata(%s): %v", name, err)
		}
		if got.FileType != want {
			t.Errorf("%s: FileType = %q, want %q", name, got.FileType, want)
		}
		if got.ID != name || got.FileName != name {
			t.Errorf("%s: ID/FileName = %q/%q", name, got.ID, got.FileName)
		}
	}
}
