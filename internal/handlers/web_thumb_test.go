package handlers

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestPNG 在 uploads 下生成一张 w×h 的 PNG。
func writeTestPNG(t *testing.T, rel string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	abs := filepath.Join(uploadBaseDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

// TestWebThumbScalesImage 验证缩略图按长边等比缩小，且接口返回可解码的 PNG。
func TestWebThumbScalesImage(t *testing.T) {
	isolation(t, "")
	thumbCacheRoot = t.TempDir()
	t.Cleanup(func() { thumbCacheRoot = "" })
	srv := webTestServer(t)

	writeTestPNG(t, "pics/wide.png", 300, 200)

	resp, out := webGet(t, srv.URL+"/api/web/thumb?path=pics/wide.png")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumb status = %d, body = %s", resp.StatusCode, out)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("cache-control = %q, want private cache", cc)
	}

	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != thumbLongSide || b.Dy() != thumbLongSide*2/3 {
		t.Errorf("thumb size = %dx%d, want %dx%d", b.Dx(), b.Dy(), thumbLongSide, thumbLongSide*2/3)
	}

	// 第二次请求应命中缓存：内容一致，且缓存目录里确实落了文件
	_, again := webGet(t, srv.URL+"/api/web/thumb?path=pics/wide.png")
	if !bytes.Equal(out, again) {
		t.Errorf("cached thumb differs from first response")
	}
	files, err := os.ReadDir(thumbCacheRoot)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("cache dir has %d files, want 1", len(files))
	}
}

// TestWebThumbRejectsNonImage 验证非图片与危险类型不会被当成缩略图处理。
func TestWebThumbRejectsNonImage(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.WriteFile(filepath.Join(uploadBaseDir, "notes.txt"), []byte("hello"), 0o644)

	resp, _ := webGet(t, srv.URL+"/api/web/thumb?path=notes.txt")
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text thumb status = %d, want 415", resp.StatusCode)
	}

	// 路径穿越同样要被挡住
	resp, _ = webGet(t, srv.URL+"/api/web/thumb?path=../secret.png")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("traversal thumb status = %d, want 400", resp.StatusCode)
	}
}

// TestWebThumbFallsBackForUndecodableImage 验证服务端解不了的图片类型
// （SVG / WebP 等）直接重定向到预览接口，让浏览器自己缩放。
func TestWebThumbFallsBackForUndecodableImage(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.WriteFile(filepath.Join(uploadBaseDir, "logo.svg"),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`), 0o644)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/api/web/thumb?path=logo.svg")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("svg thumb status = %d, want 307", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/api/web/preview?path=") {
		t.Errorf("redirect location = %q, want preview fallback", loc)
	}
}
