package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
)

// 传输历史：
//
//	GET /api/web/history   返回最近的接收/发送记录
//
// 记录只存在内存里（重启即清空），最多保留 maxHistory 条。
// 不落盘是刻意的：这些是「谁在什么时候传了什么东西」的隐私信息，
// 而且文件本身已经在 uploads/ 里，历史只是给人看的便捷视图。

// maxHistory 是内存里保留的记录条数。
const maxHistory = 200

// historyEntry 是一条传输记录。
type historyEntry struct {
	// Direction: "receive"（别人传给我）或 "send"（我通过 Web 发出去）
	Direction string `json:"direction"`
	// FileName / Size / Peer 是展示用的基本信息
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	Peer     string `json:"peer"`
	// SavedPath 是接收文件在 uploads 下的相对路径（发送记录里是对端文件名）
	SavedPath string `json:"savedPath,omitempty"`
	// OK 表示成功；Error 是失败原因
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	At    string `json:"at"` // RFC3339
}

// transferHistory 是环形缓冲；用互斥锁保护，避免并发接收时数据竞争。
type transferLog struct {
	mu      sync.Mutex
	entries []historyEntry
}

var webHistory = &transferLog{entries: make([]historyEntry, 0, maxHistory)}

// recordHistory 追加一条记录（超出上限时丢弃最旧的）。
func recordHistory(e historyEntry) {
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339)
	}
	webHistory.mu.Lock()
	defer webHistory.mu.Unlock()
	webHistory.entries = append(webHistory.entries, e)
	if len(webHistory.entries) > maxHistory {
		webHistory.entries = webHistory.entries[len(webHistory.entries)-maxHistory:]
	}
}

// snapshotHistory 返回按时间倒序的记录副本。
func snapshotHistory() []historyEntry {
	webHistory.mu.Lock()
	defer webHistory.mu.Unlock()
	out := make([]historyEntry, len(webHistory.entries))
	for i, e := range webHistory.entries {
		out[len(webHistory.entries)-1-i] = e
	}
	return out
}

// WebHistory 返回传输历史。
func WebHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]any{
		"entries": snapshotHistory(),
		"device":  shared.Message.Alias,
	})
}
