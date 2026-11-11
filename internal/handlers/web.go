package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// Web 模式（浏览器上传站）的公共设施：
//   - 相对路径 -> uploads 下的绝对路径的安全解析；
//   - 写操作的 CSRF token 校验；
//   - JSON 响应与错误回显。

// maxWebBodyBytes 限制 Web JSON API 的请求体大小（只有路径、名字等短字符串）。
const maxWebBodyBytes = 64 << 10

// webError 是回给前端的统一错误结构。
type webError struct {
	Message string `json:"message"`
}

// writeWebError 输出 JSON 错误；状态码沿用 HTTP 语义。
func writeWebError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(webError{Message: msg})
}

// writeWebJSON 输出 JSON 成功响应。
func writeWebJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeWebJSON 解析 JSON 请求体。
func decodeWebJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

// webRelPath 把请求里的相对路径规整为以 "/" 分段的干净相对路径（不带首尾斜杠）。
// 空串表示 uploads 根目录。
func webRelPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || raw == "." {
		return "", nil
	}
	// 绝对路径一律拒绝：去掉前导 "/" 再继续会把 "/etc/passwd" 静默变成
	// uploads/etc/passwd，看起来"安全"，实际掩盖了调用方的错误输入。
	if strings.HasPrefix(raw, "/") {
		return "", ErrInvalidFileName
	}
	segs := strings.Split(raw, "/")
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "" || seg == "." {
			continue
		}
		// isSafeBaseName 会拒绝 ".."、绝对路径、Windows 盘符、控制字符等。
		// 额外拒绝 URL 编码的分隔符/点号：multipart 的 relPath 与查询参数都是
		// 明文，出现 %2f/%2e 只可能是有人在试图绕过校验。
		lowered := strings.ToLower(seg)
		if strings.Contains(lowered, "%2f") || strings.Contains(lowered, "%5c") || strings.Contains(lowered, "%2e") {
			return "", ErrInvalidFileName
		}
		if !isSafeBaseName(seg) {
			return "", ErrInvalidFileName
		}
		out = append(out, seg)
	}
	return strings.Join(out, "/"), nil
}

// webAbsPath 把相对路径解析成 uploads 下的绝对路径，并确认没有逃出 uploads。
func webAbsPath(rel string) (string, error) {
	clean, err := webRelPath(rel)
	if err != nil {
		return "", err
	}
	baseAbs, err := filepath.Abs(uploadBaseDir)
	if err != nil {
		return "", err
	}
	full := baseAbs
	if clean != "" {
		parts := append([]string{baseAbs}, strings.Split(clean, "/")...)
		full = filepath.Join(parts...)
	}
	relToBase, err := filepath.Rel(baseAbs, full)
	if err != nil {
		return "", ErrInvalidFileName
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) || filepath.IsAbs(relToBase) {
		return "", ErrInvalidFileName
	}
	return full, nil
}

// checkWebPathSymlinks 拒绝符号链接穿越：
// 如果路径（或其中一级）是符号链接，且真实目标落在 uploads 之外，返回 ErrInvalidFileName。
//
// 目标路径本身可能还不存在（新建文件/目录），但它的**已存在祖先**仍可能是
// 指向外部的符号链接（uploads/link/new.txt，link -> /etc）。因此这里向上找到
// 第一个存在的祖先再求真实路径，而不是「不存在就放行」。
func checkWebPathSymlinks(abs string) error {
	baseAbs, err := filepath.Abs(uploadBaseDir)
	if err != nil {
		return err
	}
	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		if os.IsNotExist(err) {
			// uploads 还不存在：没有任何已存在的链接可穿越
			return nil
		}
		return err
	}

	// 向上找到第一个存在的祖先
	probe := abs
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			rel, err := filepath.Rel(baseReal, real)
			if err != nil {
				return ErrInvalidFileName
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return ErrInvalidFileName
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			// 到根了仍然不存在：交给上层按 not-exist 处理
			return nil
		}
		probe = parent
	}
}

// webFilePath 是文件操作（读/写/删/改）统一使用的入口：
// 相对路径校验 + 符号链接校验。
func webFilePath(rel string) (string, error) {
	abs, err := webAbsPath(rel)
	if err != nil {
		return "", err
	}
	if err := checkWebPathSymlinks(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// webErrorStatus 把内部错误映射成 HTTP 状态码与前端文案。
func webErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrInvalidFileName):
		return http.StatusBadRequest, "非法路径"
	case os.IsNotExist(err):
		return http.StatusNotFound, "文件不存在"
	case os.IsPermission(err):
		return http.StatusForbidden, "没有权限"
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// logWebError 记录并回显错误。
func logWebError(w http.ResponseWriter, action string, err error) {
	status, msg := webErrorStatus(err)
	if status >= http.StatusInternalServerError {
		logger.Errorf("%s: %v", action, err)
	} else {
		logger.Warnf("%s: %v", action, err)
	}
	writeWebError(w, status, msg)
}

// 写操作（上传/删除/重命名/新建目录/发送/取消）的 CSRF token。
//
// 它不是身份认证：局域网内任何人仍然可以直接调用这些 API（Web 模式默认
// 就是「信任局域网」，与 LocalSend 协议一致；需要保护时请配置 pin）。
// 它的作用只是让用户浏览的第三方网页无法借浏览器发起跨站写请求：
// 跨站请求带不上自定义头 X-Web-Token，也读不到页面里的 token。
var (
	webTokenOnce sync.Once
	webToken     string
)

// WebToken 返回本次进程的 CSRF token（页面渲染时写进 HTML）。
func WebToken() string {
	webTokenOnce.Do(func() {
		webToken = newWebToken()
	})
	return webToken
}

// requireWebToken 校验写请求的 X-Web-Token 头。
func requireWebToken(w http.ResponseWriter, r *http.Request) bool {
	want := WebToken()
	got := r.Header.Get("X-Web-Token")
	if want == "" || len(got) != len(want) ||
		subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeWebError(w, http.StatusForbidden, "缺少或错误的 X-Web-Token")
		return false
	}
	return true
}

// WebGuard 包装 Web 写操作：统一做 token 校验。
func WebGuard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireWebToken(w, r) {
			return
		}
		h(w, r)
	}
}
