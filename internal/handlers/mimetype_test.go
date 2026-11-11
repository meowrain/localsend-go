package handlers

import (
	"strings"
	"testing"
)

func TestMimeTypeForPathKnownExtensions(t *testing.T) {
	cases := map[string]string{
		"photo.png":        "image/png",
		"photo.JPG":        "image/jpeg",
		"a/b/c.jpeg":       "image/jpeg",
		"clip.mp4":         "video/mp4",
		"song.mp3":         "audio/mpeg",
		"notes.txt":        "text/plain",
		"doc.pdf":          "application/pdf",
		"archive.tar":      "application/x-tar",
		"archive.tar.gz":   "application/gzip", // 只看最后一段扩展名
		"report.final.PDF": "application/pdf",
		"app.apk":          "application/vnd.android.package-archive",
		"data.json":        "application/json",
	}
	for path, want := range cases {
		if got := mimeTypeForPath(path); got != want {
			t.Errorf("mimeTypeForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestMimeTypeForPathFallsBack(t *testing.T) {
	cases := []string{
		"weird.zzz",
		"noextension",
		"",
		"trailingdot.",
		".hidden",
		"file.CustomExt",
	}
	for _, path := range cases {
		if got := mimeTypeForPath(path); got != fallbackMimeType {
			t.Errorf("mimeTypeForPath(%q) = %q, want %q", path, got, fallbackMimeType)
		}
	}
}

// TestMimeTypeTableHasNoNonMimeValues 防止表里混进扩展名或空值——
// 这正是本 Issue 要修的病根。
func TestMimeTypeTableHasNoNonMimeValues(t *testing.T) {
	for ext, mime := range mimeTypeByExt {
		if mime == "" {
			t.Errorf("extension %q maps to empty mime", ext)
		}
		if mime[0] == '.' {
			t.Errorf("extension %q maps to %q, looks like an extension not a MIME", ext, mime)
		}
		if ext[0] != '.' {
			t.Errorf("key %q should start with a dot", ext)
		}
		if ext != strings.ToLower(ext) {
			t.Errorf("key %q should be lowercase", ext)
		}
	}
}
