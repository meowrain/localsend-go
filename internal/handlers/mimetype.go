package handlers

import (
	"path/filepath"
	"strings"
)

// fallbackMimeType 是协议允许的兜底类型：未知扩展名一律报二进制流，
// 让对端按「不预览、直接保存」处理，而不是靠猜。
const fallbackMimeType = "application/octet-stream"

// defaultTextMimeType 用于文本发送（虚拟 .txt 文件）。
const defaultTextMimeType = "text/plain"

// mimeTypeByExt 是协议里 fileType 字段使用的映射表：扩展名 -> MIME。
//
// 协议要求 fileType 是 MIME（如 image/png、text/plain），官方 App 按它决定预览
// 与保存策略；之前这里填的是 filepath.Ext 的结果（".png"），对端拿不到可靠信息。
// 单独放在一个文件里，方便增补与测试。
//
// 键统一用小写、带前导点。只列常见类型，未命中的回退 fallbackMimeType。
var mimeTypeByExt = map[string]string{
	// 图片
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".heic": "image/heic",
	".ico":  "image/x-icon",
	".tif":  "image/tiff",
	".tiff": "image/tiff",

	// 文本 / 文档
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".log":  "text/plain",
	".html": "text/html",
	".htm":  "text/html",
	".css":  "text/css",
	".js":   "text/javascript",
	".json": "application/json",
	".xml":  "application/xml",
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".toml": "application/toml",
	".pdf":  "application/pdf",
	".rtf":  "application/rtf",
	".epub": "application/epub+zip",

	// 压缩包
	".zip": "application/zip",
	".gz":  "application/gzip",
	".tar": "application/x-tar",
	".7z":  "application/x-7z-compressed",
	".rar": "application/vnd.rar",
	".bz2": "application/x-bzip2",
	".xz":  "application/x-xz",

	// 音视频
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".mp4":  "video/mp4",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",

	// 可执行 / 安装包
	".apk": "application/vnd.android.package-archive",
	".exe": "application/vnd.microsoft.portable-executable",
	".msi": "application/x-msi",
	".dmg": "application/x-apple-diskimage",
	".deb": "application/vnd.debian.binary-package",
	".rpm": "application/x-rpm",
	".pkg": "application/x-newton-compatible-pkg",
}

// mimeTypeForPath 按文件路径的扩展名查 MIME，未命中回退 application/octet-stream。
// 扩展名大小写不敏感（.PNG 与 .png 等价）。
func mimeTypeForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return fallbackMimeType
	}
	if mime, ok := mimeTypeByExt[ext]; ok {
		return mime
	}
	return fallbackMimeType
}
