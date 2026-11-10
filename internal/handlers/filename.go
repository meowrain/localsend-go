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

// isSafeBaseName 判断 name 是否是可以直接落盘的“纯文件名”。
func isSafeBaseName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	// 控制字符、路径分隔符都不允许出现在文件名里
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) {
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
	// 文件名里出现 URL 编码的分隔符/点号，说明发送端做了二次编码或在掩盖路径，
	// localsend 协议里 fileName 是明文，正常客户端不会这样传，直接拒绝。
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
