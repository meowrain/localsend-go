package handlers

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// Web 文件管理 API：
//
//	GET  /api/web/files?path=          列出目录
//	GET  /api/web/download?path=       下载文件 / 目录（zip）
//	GET  /api/web/preview?path=        预览（仅安全类型内联）
//	POST /api/web/mkdir                新建目录
//	POST /api/web/rename               重命名 / 移动
//	POST /api/web/delete               删除（目录可递归）
//	POST /api/web/upload?path=         上传（multipart，保留相对目录结构）
//
// 所有路径都相对 uploads/，统一经过 webFilePath 校验（逐段白名单 + 符号链接检查）。

// webListEntry 是文件列表里的一项。
type webListEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // 相对 uploads/，统一用 "/"
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // RFC3339
	Mime    string `json:"mime,omitempty"`
}

// WebListFiles 返回某个目录下的文件列表（不递归）。
func WebListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rel := r.URL.Query().Get("path")
	abs, err := webFilePath(rel)
	if err != nil {
		logWebError(w, "list files", err)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		logWebError(w, "list files", err)
		return
	}
	if !info.IsDir() {
		writeWebError(w, http.StatusBadRequest, "不是目录")
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		logWebError(w, "list files", err)
		return
	}

	cleanRel, _ := webRelPath(rel)
	out := make([]webListEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// 隐藏文件（.localsend-*.part 等）不展示，避免用户误操作正在写入的临时文件
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(abs, name)
		st, err := os.Lstat(full)
		if err != nil {
			continue
		}
		// 符号链接只展示指向 uploads 内部的，避免把 /etc 引进来
		if st.Mode()&os.ModeSymlink != 0 {
			if err := checkWebPathSymlinks(full); err != nil {
				continue
			}
			if resolved, err := os.Stat(full); err == nil {
				st = resolved
			} else {
				continue
			}
		}
		relPath := path.Join(cleanRel, name)
		item := webListEntry{
			Name:    name,
			Path:    relPath,
			IsDir:   st.IsDir(),
			Size:    st.Size(),
			ModTime: st.ModTime().UTC().Format(time.RFC3339),
		}
		if !st.IsDir() {
			item.Mime = mimeTypeForPath(name)
		}
		out = append(out, item)
	}

	// 目录在前、同类按名称排序（前端还可以再按大小/时间排）
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	parent := ""
	if cleanRel != "" {
		parent = path.Dir(cleanRel)
		if parent == "." {
			parent = ""
		}
	}

	writeWebJSON(w, http.StatusOK, map[string]any{
		"path":    cleanRel,
		"parent":  parent,
		"entries": out,
	})
}

// inlinePreviewMimes 是允许浏览器内联显示的 MIME：
// 只有这些类型不会在页面同源上下文里执行脚本，其余一律强制下载。
func inlinePreviewMimes(mime string) bool {
	switch {
	case strings.HasPrefix(mime, "image/"),
		strings.HasPrefix(mime, "video/"),
		strings.HasPrefix(mime, "audio/"),
		mime == "text/plain",
		mime == "application/pdf":
		return true
	}
	return false
}

// serveUploadFile 用安全的方式把一个 uploads 下的文件发给浏览器。
// inline=true 时只对白名单类型内联，其余强制 attachment。
func serveUploadFile(w http.ResponseWriter, r *http.Request, abs string, inline bool) {
	f, err := os.Open(abs)
	if err != nil {
		logWebError(w, "open file", err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		logWebError(w, "stat file", err)
		return
	}
	if info.IsDir() {
		writeWebError(w, http.StatusBadRequest, "是目录")
		return
	}

	mime := mimeTypeForPath(info.Name())
	disposition := "attachment"
	if inline && inlinePreviewMimes(mime) {
		disposition = "inline"
		// 内联内容统一沙箱化：即使 MIME 判断失误，脚本也无法在本源执行
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disposition, urlPathEscape(info.Name())))
	// ServeContent 负责 Range / If-Modified-Since，视频拖动进度条依赖它
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// urlPathEscape 按 RFC 5987 转义文件名（保留 "/" 之外的所有内容）。
func urlPathEscape(name string) string {
	var b strings.Builder
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// WebDownload 下载单个文件，或把目录 / 多选打包成 zip。
func WebDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// 单个文件：直接流式返回（支持 Range）
	if rel := r.URL.Query().Get("path"); rel != "" {
		abs, err := webFilePath(rel)
		if err != nil {
			logWebError(w, "download", err)
			return
		}
		info, err := os.Stat(abs)
		if err != nil {
			logWebError(w, "download", err)
			return
		}
		if !info.IsDir() {
			serveUploadFile(w, r, abs, false)
			return
		}
		// 目录也走 zip
		writeZip(w, []string{rel})
		return
	}

	// 多选：?paths=a&paths=b
	paths := r.URL.Query()["paths"]
	if len(paths) == 0 {
		writeWebError(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	writeZip(w, paths)
}

// WebPreview 预览：只对安全类型内联，其余仍走下载。
func WebPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	abs, err := webFilePath(r.URL.Query().Get("path"))
	if err != nil {
		logWebError(w, "preview", err)
		return
	}
	serveUploadFile(w, r, abs, true)
}

// zipEntry 是待打包的一个（目录下的）文件。
type zipEntry struct {
	arcName string // zip 内路径
	absPath string
}

// collectZipEntries 递归收集目录，跳过符号链接逃逸与隐藏临时文件。
func collectZipEntries(rootRel string) ([]zipEntry, error) {
	abs, err := webFilePath(rootRel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	baseName := info.Name()
	if !info.IsDir() {
		return []zipEntry{{arcName: baseName, absPath: abs}}, nil
	}

	var out []zipEntry
	err = filepath.Walk(abs, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == abs {
			return nil
		}
		if strings.HasPrefix(fi.Name(), ".") {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := checkWebPathSymlinks(p); err != nil {
				return nil // 指向 uploads 之外的符号链接直接跳过
			}
			if resolved, err := os.Stat(p); err != nil || resolved.IsDir() {
				return nil
			}
			fi, _ = os.Stat(p)
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return nil
		}
		out = append(out, zipEntry{
			arcName: path.Join(baseName, filepath.ToSlash(rel)),
			absPath: p,
		})
		return nil
	})
	return out, err
}

// writeZip 把若干路径打包成 zip 直接写到响应。
// 中途出错时响应已经开始了，只能记录日志并中断。
func writeZip(w http.ResponseWriter, paths []string) {
	var entries []zipEntry
	for _, rel := range paths {
		clean, err := webRelPath(rel)
		if err != nil || clean == "" {
			writeWebError(w, http.StatusBadRequest, "非法路径")
			return
		}
		got, err := collectZipEntries(clean)
		if err != nil {
			logWebError(w, "zip", err)
			return
		}
		entries = append(entries, got...)
	}
	if len(entries) == 0 {
		writeWebError(w, http.StatusNotFound, "没有可打包的文件")
		return
	}

	name := fmt.Sprintf("localsend-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	zw := zip.NewWriter(w)
	for _, entry := range entries {
		src, err := os.Open(entry.absPath)
		if err != nil {
			logger.Warnf("zip: skip %s: %v", entry.absPath, err)
			continue
		}
		info, err := src.Stat()
		if err != nil {
			src.Close()
			continue
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			src.Close()
			continue
		}
		hdr.Name = entry.arcName
		hdr.Method = zip.Deflate
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			src.Close()
			logger.Errorf("zip: create %s: %v", entry.arcName, err)
			return
		}
		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			logger.Errorf("zip: copy %s: %v", entry.arcName, err)
			return
		}
		src.Close()
	}
	if err := zw.Close(); err != nil {
		logger.Errorf("zip: close: %v", err)
	}
}

// webMkdirRequest 是新建目录的请求体。
type webMkdirRequest struct {
	Path string `json:"path"`
}

// WebMkdir 新建目录（不递归：父目录必须已存在）。
func WebMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req webMkdirRequest
	if err := decodeWebJSON(w, r, &req); err != nil {
		writeWebError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	clean, err := webRelPath(req.Path)
	if err != nil || clean == "" {
		writeWebError(w, http.StatusBadRequest, "非法路径")
		return
	}
	abs, err := webFilePath(clean)
	if err != nil {
		logWebError(w, "mkdir", err)
		return
	}
	// 允许一次创建多级目录：前端只有一个输入框，用户很可能直接写 "a/b/c"。
	if err := os.MkdirAll(abs, 0o755); err != nil {
		if os.IsExist(err) {
			writeWebError(w, http.StatusConflict, "目录已存在")
			return
		}
		logWebError(w, "mkdir", err)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]string{"path": clean})
}

// webRenameRequest 是重命名 / 移动的请求体。
type webRenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// WebRename 重命名或移动文件 / 目录。
func WebRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req webRenameRequest
	if err := decodeWebJSON(w, r, &req); err != nil {
		writeWebError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	fromClean, err := webRelPath(req.From)
	if err != nil || fromClean == "" {
		writeWebError(w, http.StatusBadRequest, "非法源路径")
		return
	}
	toClean, err := webRelPath(req.To)
	if err != nil || toClean == "" {
		writeWebError(w, http.StatusBadRequest, "非法目标路径")
		return
	}
	fromAbs, err := webFilePath(fromClean)
	if err != nil {
		logWebError(w, "rename", err)
		return
	}
	toAbs, err := webFilePath(toClean)
	if err != nil {
		logWebError(w, "rename", err)
		return
	}
	if _, err := os.Lstat(fromAbs); err != nil {
		logWebError(w, "rename", err)
		return
	}
	if _, err := os.Lstat(toAbs); err == nil {
		writeWebError(w, http.StatusConflict, "目标已存在")
		return
	}
	if err := os.Rename(fromAbs, toAbs); err != nil {
		logWebError(w, "rename", err)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]string{"path": toClean})
}

// webDeleteRequest 是删除请求体。
type webDeleteRequest struct {
	Paths     []string `json:"paths"`
	Recursive bool     `json:"recursive"`
}

// WebDelete 删除文件或目录；非空目录必须显式 recursive。
func WebDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req webDeleteRequest
	if err := decodeWebJSON(w, r, &req); err != nil {
		writeWebError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if len(req.Paths) == 0 {
		writeWebError(w, http.StatusBadRequest, "缺少路径")
		return
	}

	deleted := make([]string, 0, len(req.Paths))
	for _, raw := range req.Paths {
		clean, err := webRelPath(raw)
		if err != nil || clean == "" {
			// 删除 uploads 根目录是绝对禁止的
			writeWebError(w, http.StatusBadRequest, "非法路径")
			return
		}
		abs, err := webFilePath(clean)
		if err != nil {
			logWebError(w, "delete", err)
			return
		}
		info, err := os.Lstat(abs)
		if err != nil {
			logWebError(w, "delete", err)
			return
		}
		if info.IsDir() {
			if !req.Recursive {
				entries, err := os.ReadDir(abs)
				if err != nil {
					logWebError(w, "delete", err)
					return
				}
				if len(entries) > 0 {
					writeWebError(w, http.StatusConflict, fmt.Sprintf("目录 %s 非空", clean))
					return
				}
			}
			err = os.RemoveAll(abs)
		} else {
			err = os.Remove(abs)
		}
		if err != nil {
			logWebError(w, "delete", err)
			return
		}
		deleted = append(deleted, clean)
	}
	writeWebJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// newWebToken 生成一个随机的 CSRF token。
//
// Web 页面由本服务自己渲染，token 直接写进页面；所有写操作要求带
// X-Web-Token 头。它不是身份认证（局域网内任何人都能直接调 API），
// 只是防止用户浏览的恶意网页借浏览器发起跨站表单/请求。
func newWebToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
