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
//
// 注意：utf8.RuneStart 只说明「该字节不是 continuation byte」，被截断的
// 多字节序列的 leader byte 也满足它。所以先按 RuneStart 回退，再确认前缀
// 确实是合法 UTF-8；若不合法（输入本身含非法字节）就继续回退，保证不会
// 把半个 rune / 非法字节发给对端。
func TruncatePreview(text string) string {
	if len(text) <= MaxPreviewBytes {
		return text
	}
	cut := MaxPreviewBytes
	for cut > 0 {
		if utf8.RuneStart(text[cut]) && utf8.ValidString(text[:cut]) {
			break
		}
		cut--
	}
	return text[:cut]
}
