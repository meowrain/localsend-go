package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// Web 端缩略图：
//
//	GET /api/web/thumb?path=     返回图片文件的缩略图（PNG）
//
// 只对能安全解码的位图（PNG / JPEG / GIF）在服务端缩放；其余图片类型
// （webp / svg / avif…）返回 302 指向预览接口，让浏览器自己缩小原图。
// 生成结果按「路径 + 大小 + 修改时间」缓存在用户缓存目录，避免重复解码。

// thumbLongSide 是缩略图长边像素：列表里按 40 CSS px 显示，留出 2x 屏的余量。
const thumbLongSide = 96

// maxThumbPixels 限制源图像素总数：解码一张图会占用 4 字节/像素，
// 2400 万像素约 96 MiB，再大就退回原图，避免把服务端内存打爆。
const maxThumbPixels = 24_000_000

// maxThumbSourceBytes 限制参与解码的源文件大小：超大文件解码既慢又占内存。
const maxThumbSourceBytes = 64 << 20

// thumbSem 限制同时解码的图片数量（解码是 CPU / 内存密集操作）。
var thumbSem = make(chan struct{}, 4)

// errThumbSkip 表示无法（或不值得）在服务端生成缩略图，
// 调用方应退回原图预览，而不是报错。
var errThumbSkip = errors.New("thumb: fall back to original")

// thumbCacheRoot 为空时使用 os.UserCacheDir()/localsend-go/thumbs。
// 测试可以覆盖它，避免污染真实缓存目录。
var thumbCacheRoot string

// thumbCacheState 缓存「已创建的目录」与「已告警过」状态，避免每次请求都 MkdirAll / 刷日志。
var thumbCacheState struct {
	sync.Mutex
	created string
	failed  bool
}

// thumbCacheDir 返回（并创建）缩略图缓存目录；不可用时返回空串（不缓存）。
func thumbCacheDir() string {
	root := thumbCacheRoot
	if root == "" {
		if base, err := os.UserCacheDir(); err == nil && base != "" {
			root = filepath.Join(base, "localsend-go", "thumbs")
		} else {
			root = filepath.Join(os.TempDir(), "localsend-go-thumbs")
		}
	}

	thumbCacheState.Lock()
	defer thumbCacheState.Unlock()
	if thumbCacheState.created == root {
		return root
	}
	if thumbCacheState.failed && thumbCacheState.created == "" {
		return ""
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		logger.Warnf("thumb cache dir unavailable, thumbnails will not be cached: %v", err)
		thumbCacheState.failed = true
		return ""
	}
	thumbCacheState.created = root
	thumbCacheState.failed = false
	return root
}

// thumbDecodable 判断 MIME 是否有服务端解码器（webp / svg 等交给浏览器）。
func thumbDecodable(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}
	return false
}

// WebThumb 输出缩略图。
func WebThumb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw := r.URL.Query().Get("path")
	clean, err := webRelPath(raw)
	if err != nil {
		logWebError(w, "thumb", err)
		return
	}
	abs, err := webFilePath(clean)
	if err != nil {
		logWebError(w, "thumb", err)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		logWebError(w, "thumb", err)
		return
	}
	if info.IsDir() {
		writeWebError(w, http.StatusBadRequest, "是目录")
		return
	}
	mime := mimeTypeForPath(info.Name())
	if !strings.HasPrefix(mime, "image/") {
		writeWebError(w, http.StatusUnsupportedMediaType, "不是图片")
		return
	}
	if !thumbDecodable(mime) {
		redirectToPreview(w, r, clean)
		return
	}

	cachePath := ""
	if dir := thumbCacheDir(); dir != "" {
		cachePath = filepath.Join(dir, thumbCacheKey(clean, info))
		if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
			writeThumbPNG(w, r, data, info)
			return
		}
	}

	data, err := buildThumbnail(abs, info)
	if err != nil {
		if errors.Is(err, errThumbSkip) {
			// 解码不了 / 图太大：让浏览器用原图当缩略图
			redirectToPreview(w, r, clean)
			return
		}
		logWebError(w, "thumb", err)
		return
	}

	if cachePath != "" {
		if err := writeFileAtomic(cachePath, data); err != nil {
			// 缓存失败不影响本次响应，只是下次还要重新解码
			logger.Debugf("write thumb cache: %v", err)
		}
	}
	writeThumbPNG(w, r, data, info)
}

// redirectToPreview 把缩略图请求交给预览接口（浏览器负责缩放）。
func redirectToPreview(w http.ResponseWriter, r *http.Request, cleanRel string) {
	http.Redirect(w, r, "/api/web/preview?path="+url.QueryEscape(cleanRel), http.StatusTemporaryRedirect)
}

// thumbCacheKey 用「相对路径 + 大小 + 修改时间 + 目标尺寸」做缓存键，
// 源文件一变键就变，无需额外的失效逻辑。
func thumbCacheKey(rel string, info os.FileInfo) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d", rel, info.Size(), info.ModTime().UnixNano(), thumbLongSide)
	return hex.EncodeToString(h.Sum(nil)[:16]) + ".png"
}

// buildThumbnail 解码源图并缩小成 PNG。
func buildThumbnail(abs string, info os.FileInfo) ([]byte, error) {
	if info.Size() > maxThumbSourceBytes {
		return nil, errThumbSkip
	}
	thumbSem <- struct{}{}
	defer func() { <-thumbSem }()

	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("%w: decode config: %v", errThumbSkip, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errThumbSkip
	}
	// 用除法比较维度，避免构造出的超大尺寸让乘法溢出、绕过上限
	if cfg.Width > maxThumbPixels/cfg.Height {
		return nil, errThumbSkip
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%w: decode: %v", errThumbSkip, err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaleDownImage(src, thumbLongSide)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scaleDownImage 按面积平均把图片缩到长边 <= longSide。
// 面积平均比最近邻慢一些，但缩小后不会出现明显锯齿，适合固定尺寸缩略图。
// 已经足够小时原样返回。
func scaleDownImage(src image.Image, longSide int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= longSide && sh <= longSide {
		return src
	}
	scale := math.Min(float64(longSide)/float64(sw), float64(longSide)/float64(sh))
	dw := int(math.Round(float64(sw) * scale))
	dh := int(math.Round(float64(sh) * scale))
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		sy0 := b.Min.Y + dy*sh/dh
		sy1 := b.Min.Y + (dy+1)*sh/dh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			sx0 := b.Min.X + dx*sw/dw
			sx1 := b.Min.X + (dx+1)*sw/dw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sr, sg, sb, sa uint64
			var n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			// image.RGBA 与 Color.RGBA() 都是 alpha 预乘格式，可直接写回平均值
			i := dst.PixOffset(dx, dy)
			dst.Pix[i] = uint8(sr / n >> 8)
			dst.Pix[i+1] = uint8(sg / n >> 8)
			dst.Pix[i+2] = uint8(sb / n >> 8)
			dst.Pix[i+3] = uint8(sa / n >> 8)
		}
	}
	return dst
}

// writeThumbPNG 输出缩略图，交给 ServeContent 处理条件请求与 Range。
func writeThumbPNG(w http.ResponseWriter, r *http.Request, data []byte, srcInfo os.FileInfo) {
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("X-Content-Type-Options", "nosniff")
	// 只给浏览器私有缓存：图片是登录态（可能配了 PIN）下的内容，不进共享缓存
	h.Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", srcInfo.ModTime(), bytes.NewReader(data))
}

// writeFileAtomic 先写临时文件再 rename，避免并发请求读到半截缓存。
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".thumb-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
