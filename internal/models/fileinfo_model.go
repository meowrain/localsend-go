package models

// FileMetadata 是协议 4.1 里每个文件的附加时间戳（nullable）。
// 字段一律用 RFC3339 字符串：发送端可能来自任意语言/平台，接收端不应假设格式更严格。
type FileMetadata struct {
	Modified string `json:"modified,omitempty"`
	Accessed string `json:"accessed,omitempty"`
}

type FileInfo struct {
	ID       string        `json:"id"`
	FileName string        `json:"fileName"`
	Size     int64         `json:"size"`
	FileType string        `json:"fileType"`
	SHA256   string        `json:"sha256,omitempty"`
	Preview  string        `json:"preview,omitempty"`
	Metadata *FileMetadata `json:"metadata,omitempty"`
}
