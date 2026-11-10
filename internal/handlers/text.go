package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// ErrEmptyText 表示待发送的文本为空。
var ErrEmptyText = errors.New("text to send is empty")

// MaxPreviewBytes 是 prepare 阶段 preview 字段的截断上限。
//
// 依据：preview 随 prepare 请求体一次性发送，官方实现只取文本开头用于对端展示/剪贴板，
// 官方 App 与客户端普遍按 KB 级别处理预览；这里取 1 KiB，既能把验证码、短链接、
// 一两行文本完整带到对端，又避免大段文本撑大 prepare 请求体（部分实现对请求体有大小限制）。
// 完整文本始终通过 upload 阶段写入 .txt 文件，因此截断不会造成内容丢失。
const MaxPreviewBytes = 1024

// textFileName 生成虚拟文本文件的文件名，形如 clipboard-20260101-120000.txt。
func textFileName(t time.Time) string {
	return fmt.Sprintf("clipboard-%s.txt", t.Format("20060102-150405"))
}

// sha256OfBytes 计算内存数据的 SHA-256，供 prepare 阶段声明文件使用。
func sha256OfBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// TruncatePreview 生成 prepare 阶段携带的文本预览：
// 超过 MaxPreviewBytes 的部分被截断，且不会切断 UTF-8 字符。
func TruncatePreview(text string) string {
	if len(text) <= MaxPreviewBytes {
		return text
	}
	cut := MaxPreviewBytes
	// 回退到前一个合法字符边界，避免产生半个 rune 的乱码。
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
