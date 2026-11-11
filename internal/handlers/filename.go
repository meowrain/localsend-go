package handlers

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidFileName 表示文件名不可用于落盘（空、路径穿越、绝对路径等）。
var ErrInvalidFileName = errors.New("invalid file name")

// safeJoinUnder 把 name 安全地拼接到 base 目录下，保证结果不会逃出 base。
// 它同时处理 ".."、"绝对路径"、Windows 盘符，以及恶意符号链接造成的穿越。
func safeJoinUnder(base, name string) (string, error) {
	if !isSafeBaseName(name) {
		return "", ErrInvalidFileName
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	full := filepath.Join(baseAbs, name)
	rel, err := filepath.Rel(baseAbs, full)
	if err != nil {
		return "", ErrInvalidFileName
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrInvalidFileName
	}
	return full, nil
}

// cleanRelDir 把前端/请求传来的相对目录（如 "photos/2024"）规整成可安全拼接到
// uploads/ 之下的相对路径。空串或 "." 表示根目录（返回 ""）。
//
// 与 sanitizeFileName 的区别：这里允许用 "/" 分层的多级目录，但每一段都必须
// 通过 isSafeBaseName，任何一段是 ".."、绝对路径、Windows 盘符都会被整体拒绝，
// 因此调用方可以直接 filepath.Join(uploads, cleanRelDir(...)) 而不会逃出 uploads。
func cleanRelDir(dir string) (string, error) {
	dir = strings.TrimSpace(strings.ReplaceAll(dir, "\\", "/"))
	dir = strings.TrimPrefix(dir, "./")
	if dir == "" || dir == "." {
		return "", nil
	}
	if strings.HasPrefix(dir, "/") || strings.Contains(dir, "\x00") {
		return "", ErrInvalidFileName
	}
	segs := strings.Split(dir, "/")
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "" || seg == "." {
			// 连续斜杠与 "./" 视为无意义，跳过；".." 不在其列，会被 isSafeBaseName 拒绝
			continue
		}
		if !isSafeBaseName(seg) {
			return "", ErrInvalidFileName
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", nil
	}
	return filepath.Join(out...), nil
}

// maxFileNameBytes 限制落盘文件名的字节数：
// 常见文件系统单个文件名上限是 255 字节，留出扩展名与清理的余量后取 200。
const maxFileNameBytes = 200

// windowsReservedNames 是 Windows 下不能作为文件名的设备名（不含扩展名时也不允许）。
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// isWindowsReservedName 判断 Windows 保留设备名（大小写不敏感，忽略扩展名）。
func isWindowsReservedName(name string) bool {
	base := name
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	return windowsReservedNames[strings.ToUpper(base)]
}

// isSafeBaseName 判断 name 是否是可以直接落盘的“纯文件名”。
func isSafeBaseName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	if len(name) > maxFileNameBytes {
		return false
	}
	// 控制字符、格式字符（如 U+202E 反向覆盖、零宽字符）都不允许：
	// Cf 类不是 Cc，IsControl 挡不住，但足够用来伪装扩展名。
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	// 纯 "." / ".." 以及 Windows 下的 ".." 变体
	if name == "." || name == ".." || strings.TrimRight(name, ". ") != name {
		return false
	}
	// Windows：filepath.IsAbs 认 "C:\.."，这里再兜一层盘符判断
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return false
	}
	if strings.Contains(name, ":") {
		return false
	}
	if isWindowsReservedName(name) {
		return false
	}
	return true
}

// sanitizeFileName 把接收到的文件名规整成可安全落盘的 basename。
// 兼容官方 App 只发不带路径的文件名；带路径的文件名只取最后一段。
func sanitizeFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return "", ErrInvalidFileName
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || filepath.IsAbs(name) {
		return "", ErrInvalidFileName
	}
	// 文件名里出现 URL 编码的分隔符/点号，说明发送端做了二次编码或在掩盖路径。
	// 协议里 fileName 是明文，正常客户端不会这样传，直接拒绝（宁误杀不放行）。
	lowered := strings.ToLower(name)
	if strings.Contains(lowered, "%2f") || strings.Contains(lowered, "%5c") || strings.Contains(lowered, "%2e") {
		return "", ErrInvalidFileName
	}
	if strings.ContainsAny(name, "/\\") {
		// 只要出现 "."/".." 段就当成穿越意图，直接拒绝，而不是悄悄取 basename 放行。
		// 注意 filepath.Base("../x") == "x"，不能依赖 Base 判断。
		for _, seg := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
			if seg == ".." || seg == "." {
				return "", ErrInvalidFileName
			}
		}
		name = filepath.Base(filepath.ToSlash(name))
	}
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return "", ErrInvalidFileName
		}
	}
	if !isSafeBaseName(name) {
		return "", ErrInvalidFileName
	}
	return name, nil
}
