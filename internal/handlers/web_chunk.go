package handlers

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// 分片上传 / 断点续传：
//
//	GET  /api/web/upload/chunk?uploadId=..&path=<dir>&relPath=..  查询已接收字节数
//	POST /api/web/upload/chunk?uploadId=..&path=<dir>&relPath=..&offset=..&total=..[&conflict=..]
//	DELETE /api/web/upload/chunk?uploadId=..&path=<dir>           放弃本次上传
//
// 每个分片是原始字节（application/octet-stream），按 offset 顺序追加到
// uploads/<dir>/.upload-<uploadId>.part。part 文件是隐藏文件，不会出现在列表里。
// 收到最后一个字节后按冲突策略 rename 成最终文件——与普通上传共用同一套校验。
//
// 断点续传的「断点」就是 part 文件的当前大小：刷新页面后前端用同一个
// uploadId 查询 offset，从断点继续。uploadId 由前端生成并放在 localStorage 里。

// maxChunkBytes 限制单个分片大小：4 MiB 是前端默认值的两倍，留出余量。
const maxChunkBytes = 8 << 20

// chunkUploadIDLen 是 uploadId 的十六进制长度（16 字节）。
const chunkUploadIDLen = 32

// validateUploadID 校验 uploadId：只允许十六进制，避免它被拼进路径时带出 ".."。
func validateUploadID(id string) error {
	if len(id) != chunkUploadIDLen {
		return ErrInvalidFileName
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ErrInvalidFileName
		}
	}
	return nil
}

// chunkPartPath 返回分片临时文件路径（隐藏文件，位于目标目录内，保证 rename 不跨文件系统）。
func chunkPartPath(destDir, uploadID string) (string, error) {
	if err := validateUploadID(uploadID); err != nil {
		return "", err
	}
	if err := checkWebPathSymlinks(destDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(destDir, ".upload-"+uploadID+".part"), nil
}

// chunkTarget 解析分片上传的目标目录与最终路径。
func chunkTarget(r *http.Request) (destDir, relClean string, segments []string, err error) {
	dirClean, err := webRelPath(r.URL.Query().Get("path"))
	if err != nil {
		return "", "", nil, err
	}
	destDir, err = webAbsPath(dirClean)
	if err != nil {
		return "", "", nil, err
	}
	// 分片接口的元数据一律走查询参数，body 只放原始字节。
	// 不能用 FormValue：如果客户端把 Content-Type 误写成表单类型，
	// ParseForm 会把 body 当表单读掉，后面的 io.Copy 只能读到 0 字节。
	segments, err = splitUploadRelPath(r.URL.Query().Get("relPath"), r.URL.Query().Get("name"))
	if err != nil {
		return "", "", nil, err
	}
	return destDir, dirClean, segments, nil
}

// WebChunkStatus 返回已收到的字节数（没有 part 文件时为 0）。
func WebChunkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	dirClean, err := webRelPath(q.Get("path"))
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法目录")
		return
	}
	destDir, err := webAbsPath(dirClean)
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法目录")
		return
	}
	part, err := chunkPartPath(destDir, q.Get("uploadId"))
	if err != nil {
		logWebError(w, "chunk status", err)
		return
	}
	info, err := os.Stat(part)
	if err != nil {
		if os.IsNotExist(err) {
			writeWebJSON(w, http.StatusOK, map[string]int64{"offset": 0})
			return
		}
		logWebError(w, "chunk status", err)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]int64{"offset": info.Size()})
}

// WebChunkUpload 接收一个分片。
func WebChunkUpload(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleChunkPost(w, r)
	case http.MethodDelete:
		handleChunkDelete(w, r)
	default:
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleChunkPost(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// relPath 允许走查询参数（DELETE/GET 没法带 body 时用），POST 优先读表单
	destDir, cleanDir, segments, err := chunkTarget(r)
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法文件名或路径")
		return
	}

	offset, err := strconv.ParseInt(q.Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		writeWebError(w, http.StatusBadRequest, "缺少或错误的 offset")
		return
	}
	total, err := strconv.ParseInt(q.Get("total"), 10, 64)
	if err != nil || total <= 0 {
		writeWebError(w, http.StatusBadRequest, "缺少或错误的 total")
		return
	}
	if offset > total {
		writeWebError(w, http.StatusBadRequest, "offset 超过 total")
		return
	}
	need := total - offset
	if need > maxChunkBytes {
		need = maxChunkBytes
	}

	part, err := chunkPartPath(destDir, q.Get("uploadId"))
	if err != nil {
		logWebError(w, "chunk upload", err)
		return
	}

	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		logWebError(w, "chunk open", err)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		logWebError(w, "chunk stat", err)
		return
	}
	// 断点必须与服务器上的 part 长度对齐：错位说明前端状态过期，
	// 直接告诉它当前 offset，让它重新同步（而不是写出一段重复内容）。
	if st.Size() != offset {
		writeWebJSON(w, http.StatusConflict, map[string]any{
			"message": "offset mismatch",
			"offset":  st.Size(),
		})
		return
	}

	// 限制本次读取：多读一个字节用来判断请求体是否超长
	body := http.MaxBytesReader(w, r.Body, need+1)
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		logWebError(w, "chunk seek", err)
		return
	}
	written, err := io.Copy(f, body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeWebError(w, http.StatusRequestEntityTooLarge, "分片超过上限")
			return
		}
		logWebError(w, "chunk write", err)
		return
	}
	if written > need {
		writeWebError(w, http.StatusRequestEntityTooLarge, "分片超过上限")
		return
	}
	if err := f.Sync(); err != nil {
		logger.Warnf("chunk sync %s: %v", part, err)
	}

	newSize := offset + written
	if newSize < total {
		writeWebJSON(w, http.StatusOK, map[string]any{"offset": newSize, "done": false})
		return
	}
	if newSize > total {
		writeWebError(w, http.StatusBadRequest, "内容超过声明的 total")
		return
	}
	if err := f.Close(); err != nil {
		logWebError(w, "chunk close", err)
		return
	}

	// 内容收齐：按冲突策略落盘（与 multipart 上传共用 resolver）
	finalPath, renamed, err := resolveUploadConflict(filepath.Join(append([]string{destDir}, segments...)...), chunkConflictStrategy(r))
	if err != nil {
		_ = os.Remove(part)
		if errors.Is(err, errSkipExisting) {
			writeWebJSON(w, http.StatusOK, map[string]any{
				"done": true, "skipped": true, "path": relForResult(cleanDir, segments),
			})
			return
		}
		writeWebError(w, http.StatusInternalServerError, "落盘失败: "+err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		_ = os.Remove(part)
		logWebError(w, "chunk mkdir", err)
		return
	}
	if err := os.Rename(part, finalPath); err != nil {
		_ = os.Remove(part)
		logWebError(w, "chunk rename", err)
		return
	}

	baseAbs, _ := filepath.Abs(uploadBaseDir)
	relSaved, _ := filepath.Rel(baseAbs, finalPath)
	logger.Infof("Chunked upload finished: %s (%d bytes)", filepath.ToSlash(relSaved), total)
	writeWebJSON(w, http.StatusOK, map[string]any{
		"done":    true,
		"path":    filepath.ToSlash(relSaved),
		"renamed": renamed,
	})
}

func handleChunkDelete(w http.ResponseWriter, r *http.Request) {
	dirClean, err := webRelPath(r.URL.Query().Get("path"))
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法目录")
		return
	}
	destDir, err := webAbsPath(dirClean)
	if err != nil {
		writeWebError(w, http.StatusBadRequest, "非法目录")
		return
	}
	part, err := chunkPartPath(destDir, r.URL.Query().Get("uploadId"))
	if err != nil {
		logWebError(w, "chunk cancel", err)
		return
	}
	if err := os.Remove(part); err != nil && !os.IsNotExist(err) {
		logWebError(w, "chunk cancel", err)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// uploadConflictStrategy 解析 multipart 上传的冲突策略（表单字段）。
func uploadConflictStrategy(r *http.Request) string {
	return normalizeConflictStrategy(r.FormValue("conflict"))
}

// chunkConflictStrategy 解析分片上传的冲突策略（查询参数，避免触碰 body）。
func chunkConflictStrategy(r *http.Request) string {
	return normalizeConflictStrategy(r.URL.Query().Get("conflict"))
}

func normalizeConflictStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "overwrite", "skip", "rename":
		return strings.ToLower(strings.TrimSpace(strategy))
	}
	return "rename"
}
