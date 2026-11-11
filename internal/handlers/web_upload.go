package handlers

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// maxWebUploadFiles 限制单次 Web 上传的文件数量，避免一个请求塞进几万个文件。
const maxWebUploadFiles = 2000

// uploadConflictError 表示落盘时同名文件已存在且请求要求「不覆盖」。
type uploadConflictError struct{ name string }

func (e *uploadConflictError) Error() string { return "file exists: " + e.name }

// webUploadResult 是 WebUpload 的成功响应。
type webUploadResult struct {
	Uploaded []string `json:"uploaded"` // 落盘的相对路径
	Skipped  []string `json:"skipped"`  // 按冲突策略跳过的相对路径
	Renamed  []string `json:"renamed"`  // 自动改名后落盘的相对路径
}

// WebUpload 处理 Web 页面的上传。
//
// 与 LocalSend 协议的 /send 相比：
//   - 显式限制总大小（config.functions.web_upload_limit_mb）；
//   - 保留相对目录结构（前端把每个文件的 relPath 一起提交）；
//   - 冲突策略：overwrite / skip / rename（默认 rename，避免静默覆盖）。
func WebUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	targetDir := r.URL.Query().Get("path")
	cleanDir, err := webRelPath(targetDir)
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法目录")
		return
	}
	if _, err := webFilePath(cleanDir); err != nil {
		logWebError(w, "upload target", err)
		return
	}

	// MaxBytesReader 让超限请求在读取阶段就失败，而不是先写满磁盘
	if limit := config.ConfigData.WebUploadLimitBytes(); limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}

	// 32 MiB 内存阈值，超出部分落系统临时文件（不是 uploads/）
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			msg := fmt.Sprintf("上传内容超过上限 %d MiB，可在 config.yaml 里调整 web_upload_limit_mb", maxErr.Limit>>20)
			writeWebError(w, http.StatusRequestEntityTooLarge, msg)
			return
		}
		writeWebError(w, http.StatusBadRequest, "表单解析失败: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeWebError(w, http.StatusBadRequest, "没有收到文件")
		return
	}
	if len(files) > maxWebUploadFiles {
		writeWebError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("一次最多上传 %d 个文件", maxWebUploadFiles))
		return
	}

	// 冲突策略：overwrite / skip / rename；缺省 rename
	strategy := uploadConflictStrategy(r)

	// 前端提交的相对路径（与 file 字段一一对应、顺序相同）。
	// 数量对不上时宁可整体忽略：错位映射会把 A 的内容写进 B 的路径，
	// 比丢掉目录结构危险得多。此时退回每个文件自己的文件名。
	relPaths := r.MultipartForm.Value["relPath"]
	if len(relPaths) != len(files) {
		relPaths = nil
	}
	totalSize := int64(0)

	baseAbs, err := filepath.Abs(uploadBaseDir)
	if err != nil {
		logWebError(w, "upload", err)
		return
	}
	destAbs := baseAbs
	if cleanDir != "" {
		destAbs = filepath.Join(append([]string{baseAbs}, strings.Split(cleanDir, "/")...)...)
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		logWebError(w, "upload mkdir", err)
		return
	}

	var result webUploadResult
	for i, header := range files {
		// relPath 缺失或非法时退化为原始文件名
		rel := ""
		if i < len(relPaths) {
			rel = relPaths[i]
		}
		rel = filepath.ToSlash(rel)
		// 只取文件名部分：目录结构由前端提供的 relPath 决定，
		// 但 relPath 本身可能带 ..（恶意构造），下面逐段校验。
		segments, err := splitUploadRelPath(rel, header.Filename)
		if err != nil {
			writeWebError(w, http.StatusBadRequest, "非法文件名或路径: "+rel)
			return
		}

		// 目标：uploads/<targetDir>/<segments...>
		parts := append([]string{destAbs}, segments...)
		dstPath := filepath.Join(parts...)
		// 再确认一次没有逃出 uploads
		if relToBase, err := filepath.Rel(baseAbs, dstPath); err != nil ||
			relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) || filepath.IsAbs(relToBase) {
			writeWebError(w, http.StatusBadRequest, "非法路径")
			return
		}

		finalPath, renamed, err := resolveUploadConflict(dstPath, strategy)
		if err != nil {
			if errors.Is(err, errSkipExisting) {
				result.Skipped = append(result.Skipped, relForResult(cleanDir, segments))
				continue
			}
			writeWebError(w, http.StatusInternalServerError, "检查目标文件失败: "+err.Error())
			return
		}
		// relPath 可能带多级目录（"a/b/c.txt"），父级可能是刚创建的。
		// 每一级都要再校验一次符号链接：攻击者可以先传 "link/x"（link 指向外部）
		// 让 MkdirAll 在外部建目录，后续文件就写到 uploads 之外了。
		if err := checkWebPathSymlinks(filepath.Dir(finalPath)); err != nil {
			logWebError(w, "upload path", err)
			return
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			logWebError(w, "upload mkdir", err)
			return
		}
		written, err := saveMultipartFile(header, finalPath)
		if err != nil {
			logWebError(w, "upload save", err)
			return
		}
		totalSize += written
		relSaved, _ := filepath.Rel(baseAbs, finalPath)
		relSaved = filepath.ToSlash(relSaved)
		result.Uploaded = append(result.Uploaded, relSaved)
		if renamed {
			result.Renamed = append(result.Renamed, relSaved)
		}
	}

	logger.Infof("Web upload: %d file(s), %d bytes -> %s", len(result.Uploaded), totalSize, cleanDir)
	writeWebJSON(w, http.StatusCreated, result)
}

// errSkipExisting 表示冲突策略为 skip 且目标已存在。
var errSkipExisting = errors.New("skip existing")

// relForResult 把「目标目录 + 分段」拼成给前端看的相对路径。
func relForResult(dir string, segments []string) string {
	return path.Join(append([]string{dir}, segments...)...)
}

// splitUploadRelPath 把前端提交的 relPath 拆成安全的路径分段。
//
// relPath 形如 "photos/2024/a.jpg"。任何一段是 ".."、绝对路径、Windows 盘符、
// 控制字符都直接拒绝；为空时退回 multipart 提供的文件名（已被 Go 取过 basename）。
func splitUploadRelPath(rel, fallbackName string) ([]string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		name, err := sanitizeFileName(fallbackName)
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "/")
	segs := strings.Split(rel, "/")
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "" || seg == "." {
			continue
		}
		// 与 webRelPath 保持一致：明文路径里出现 URL 编码的分隔符/点号只可能是
		// 在试图绕过校验（某些反向代理会二次解码），直接拒绝。
		lowered := strings.ToLower(seg)
		if strings.Contains(lowered, "%2f") || strings.Contains(lowered, "%5c") || strings.Contains(lowered, "%2e") {
			return nil, ErrInvalidFileName
		}
		if !isSafeBaseName(seg) {
			return nil, ErrInvalidFileName
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return nil, ErrInvalidFileName
	}
	return out, nil
}

// resolveUploadConflict 根据策略处理同名文件：
//   - overwrite：直接用目标路径；
//   - skip：返回 errSkipExisting；
//   - rename：追加 " (1)"、"(2)"… 直到不冲突。
//
// renamed 表示最终路径与请求路径不同。
func resolveUploadConflict(dst string, strategy string) (final string, renamed bool, err error) {
	_, statErr := os.Lstat(dst)
	exists := statErr == nil
	if !exists {
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", false, statErr
		}
		return dst, false, nil
	}
	switch strategy {
	case "overwrite":
		return dst, false, nil
	case "skip":
		return "", false, errSkipExisting
	default:
		ext := filepath.Ext(dst)
		base := strings.TrimSuffix(dst, ext)
		for i := 1; i <= 1000; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if _, err := os.Lstat(candidate); os.IsNotExist(err) {
				return candidate, true, nil
			}
		}
		return "", false, errors.New("too many files with the same name")
	}
}

// saveMultipartFile 把 multipart 文件写到 dst，先写同目录临时文件再 rename，
// 保证失败时不会留下半截文件，也不会破坏同名旧文件。
func saveMultipartFile(header *multipart.FileHeader, dst string) (int64, error) {
	src, err := header.Open()
	if err != nil {
		return 0, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*.part")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.Copy(tmp, src)
	if err != nil {
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		logger.Warnf("Web upload: sync %s failed: %v", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return 0, err
	}
	committed = true
	return written, nil
}
