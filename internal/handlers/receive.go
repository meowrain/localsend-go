package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meowrain/localsend-go/internal/models"

	"github.com/meowrain/localsend-go/internal/utils/clipboard"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/schollz/progressbar/v3"
)

// uploadBaseDir 是接收文件的落盘根目录，实际写入的绝对路径都会校验在它之下。
const uploadBaseDir = "uploads"

// maxPreviewClipboardSize 限制写入剪贴板/日志的预览内容大小。
const maxPreviewClipboardSize = 64 << 10

type receiveSession struct {
	id      string
	tokens  map[string]string // fileID -> token
	names   map[string]string // fileID -> 清理后的文件名
	peerIP  string            // prepare-upload 请求的来源 IP
	created time.Time
}

// receiveStore 保存 prepare 阶段下发的会话信息。
// PrepareReceive 写、ReceiveHandler 读，二者可能来自不同连接（并发），必须加锁。
type receiveStore struct {
	mu       sync.RWMutex
	sessions map[string]*receiveSession
}

var (
	sessionIDCounter atomic.Uint64
	// 仅保留最近若干个会话，避免长期运行后无限增长
	maxSessions = 64
	store       = &receiveStore{sessions: make(map[string]*receiveSession)}
)

func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "session-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("session-%d-%d", sessionIDCounter.Add(1), time.Now().UnixNano())
}

func (s *receiveStore) add(sess *receiveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxSessions {
		var oldest *receiveSession
		for _, v := range s.sessions {
			if oldest == nil || v.created.Before(oldest.created) {
				oldest = v
			}
		}
		if oldest != nil {
			delete(s.sessions, oldest.id)
		}
	}
	s.sessions[sess.id] = sess
}

// lookup 返回会话副本，避免调用方持有锁期间被并发修改。
func (s *receiveStore) lookup(id string) (receiveSession, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return receiveSession{}, false
	}
	cp := receiveSession{
		id:      sess.id,
		peerIP:  sess.peerIP,
		created: sess.created,
		tokens:  make(map[string]string, len(sess.tokens)),
		names:   make(map[string]string, len(sess.names)),
	}
	for k, v := range sess.tokens {
		cp.tokens[k] = v
	}
	for k, v := range sess.names {
		cp.names[k] = v
	}
	return cp, true
}

func (s *receiveStore) reset() {
	s.mu.Lock()
	s.sessions = make(map[string]*receiveSession)
	s.mu.Unlock()
}

// fileToken 生成 prepare 阶段下发的 token。
// 协议只要求它是不透明字符串；保留 "token-<fileID>" 前缀以兼容旧版 localsend-go 发送端。
func fileToken(fileID string) string {
	return "token-" + fileID
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func PrepareReceive(w http.ResponseWriter, r *http.Request) {
	var req models.PrepareReceiveRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("Received request from %s,device is %s", req.Info.Alias, req.Info.DeviceModel)

	sess := &receiveSession{
		id:      newSessionID(),
		tokens:  make(map[string]string, len(req.Files)),
		names:   make(map[string]string, len(req.Files)),
		peerIP:  clientIP(r),
		created: time.Now(),
	}

	for fileID, fileInfo := range req.Files {
		// 文件名必须能安全落盘，非法文件名直接在 prepare 阶段拒绝，避免后续写到目录外
		name, err := sanitizeFileName(fileInfo.FileName)
		if err != nil {
			logger.Errorf("Reject prepare request: invalid file name %q (fileId %q)", fileInfo.FileName, fileID)
			http.Error(w, "Invalid file name", http.StatusBadRequest)
			return
		}
		sess.tokens[fileID] = fileToken(fileID)
		sess.names[fileID] = name

		if strings.HasSuffix(name, ".txt") && len(fileInfo.Preview) > 0 {
			preview := fileInfo.Preview
			if len(preview) > maxPreviewClipboardSize {
				preview = preview[:maxPreviewClipboardSize]
			}
			logger.Success("TXT file content preview:", string(preview))
			clipboard.WriteToClipBoard(preview)
		}
	}
	store.add(sess)

	resp := models.PrepareReceiveResponse{
		SessionID: sess.id,
		Files:     sess.tokens,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// validateUpload 校验上传请求的 sessionId/fileId/token/来源 IP。
// 返回的 error 只用于日志，响应内容保持协议常量。
func validateUpload(r *http.Request) (receiveSession, string, string, error) {
	sessionID := r.URL.Query().Get("sessionId")
	fileID := r.URL.Query().Get("fileId")
	token := r.URL.Query().Get("token")

	if sessionID == "" || fileID == "" || token == "" {
		return receiveSession{}, "", "", fmt.Errorf("missing parameters")
	}

	sess, ok := store.lookup(sessionID)
	if !ok {
		return receiveSession{}, "", "", fmt.Errorf("unknown session %q", sessionID)
	}

	wantToken, ok := sess.tokens[fileID]
	if !ok {
		return receiveSession{}, "", "", fmt.Errorf("unknown fileId %q in session %q", fileID, sessionID)
	}
	// 长度不一致时直接比较不等，避免同一长度假设
	if len(token) != len(wantToken) || subtleCompare(token, wantToken) != 1 {
		return receiveSession{}, "", "", fmt.Errorf("token mismatch for fileId %q", fileID)
	}

	// 只拒绝明显的跨主机请求；同网段 NAT / 容器环境下的地址变化放行，避免破坏兼容性
	if sess.peerIP != "" {
		peer := net.ParseIP(sess.peerIP)
		cur := net.ParseIP(clientIP(r))
		if peer != nil && cur != nil && !sameHost(peer, cur) {
			return receiveSession{}, "", "", fmt.Errorf("ip mismatch: prepared from %s, upload from %s", sess.peerIP, clientIP(r))
		}
	}

	fileName, ok := sess.names[fileID]
	if !ok {
		return receiveSession{}, "", "", fmt.Errorf("no file name for fileId %q", fileID)
	}
	return sess, fileID, fileName, nil
}

// subtleCompare 常量时间比较，避免 token 校验被计时侧信道利用。
func subtleCompare(a, b string) int {
	diff := 0
	for i := 0; i < len(a); i++ {
		diff |= int(a[i] ^ b[i])
	}
	return 1 & ((diff - 1) >> 8)
}

func sameHost(a, b net.IP) bool {
	if a.Equal(b) {
		return true
	}
	return a.IsLoopback() && b.IsLoopback()
}

func ReceiveHandler(w http.ResponseWriter, r *http.Request) {
	sess, fileID, fileName, err := validateUpload(r)
	if err != nil {
		// 协议里 token 不匹配固定返回 403，且此时不会创建任何文件
		logger.Errorf("Rejected upload: %v", err)
		http.Error(w, "Invalid token or IP address", http.StatusForbidden)
		return
	}

	if err := os.MkdirAll(uploadBaseDir, 0o755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory: %v", err)
		return
	}

	// 再次校验落盘路径没跑出 uploads/
	filePath, err := safeJoinUnder(uploadBaseDir, fileName)
	if err != nil {
		logger.Errorf("Rejected upload: unsafe file name %q: %v", fileName, err)
		http.Error(w, "Invalid file name", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory: %v", err)
		return
	}

	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		logger.Errorf("Error creating file: %v", err)
		return
	}
	defer file.Close()

	// 创建一个 context 来处理请求取消
	ctx := r.Context()

	// 创建文件后，获取文件大小
	contentLength := r.ContentLength

	// 创建进度条
	bar := progressbar.NewOptions64(
		contentLength,
		progressbar.OptionSetDescription(fmt.Sprintf("下载 %s", fileName)),
		progressbar.OptionSetWidth(15),
		progressbar.OptionShowBytes(true),
		progressbar.OptionThrottle(time.Second), // 降低刷新频率，减少闪烁
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(), // 完成时清除进度条
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(true), // 预测剩余时间
		progressbar.OptionFullWidth(),          // 使用全宽显示
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█", // 使用实心方块
			SaucerHead:    "█",
			SaucerPadding: "░", // 使用灰色方块作为背景
			BarStart:      "|",
			BarEnd:        "|",
		}),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
	)

	buffer := make([]byte, 2*1024*1024) // 2MB 缓冲区

	// 使用 channel 来处理传输完成或取消
	done := make(chan error, 1)

	go func() {
		for {
			n, err := r.Body.Read(buffer)
			if err != nil && err != io.EOF {
				done <- fmt.Errorf("Read file failed: %w", err)
				return
			}
			if n == 0 {
				done <- nil
				return
			}

			_, err = file.Write(buffer[:n])
			if err != nil {
				done <- fmt.Errorf("Write file failed: %w", err)
				return
			}

			bar.Add(n)
		}
	}()

	// 等待传输完成或取消
	select {
	case err := <-done:
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			logger.Errorf("Transfer error: %v", err)
			// 删除未完成的文件
			os.Remove(filePath)
			return
		}
	case <-ctx.Done():
		// 请求被取消
		logger.Info("Transfer canceled by client")
		// 删除未完成的文件
		os.Remove(filePath)
		// 关闭连接
		if conn, ok := w.(http.CloseNotifier); ok {
			conn.CloseNotify()
		}
		return
	}

	logger.Success("File saved to:", filePath)
	logger.Debugf("Upload finished: session=%s fileId=%s", sess.id, fileID)
	w.WriteHeader(http.StatusOK)
}
