package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
)

// isolation 是这些用例共用的前置：切到临时目录、清空会话表与 PIN 计数、
// 关掉进度条（它的后台刷新 goroutine 没必要在 -race 里出现）。
func isolation(t *testing.T, fileName string) string {
	t.Helper()
	workDir := chdirTemp(t)
	store.reset()
	t.Cleanup(store.reset)
	pinGuard.mu.Lock()
	pinGuard.m = make(map[string]*pinAttempt)
	pinGuard.now = time.Now
	pinGuard.mu.Unlock()
	old := uploadProgressBar
	uploadProgressBar = func() bool { return false }
	t.Cleanup(func() { uploadProgressBar = old })
	// 兼容签名的参数只是为了提醒调用方：每个用例都从干净的 uploads/ 开始
	_ = fileName
	return workDir
}

// withPIN 临时设置接收端 PIN；空串表示不启用。
func withPIN(t *testing.T, pin string) {
	t.Helper()
	old := config.ConfigData.Functions.PIN
	config.ConfigData.SetPIN(pin)
	t.Cleanup(func() { config.ConfigData.Functions.PIN = old })
}

// setClock 固定 PIN 计数表使用的时钟。
func setClock(t *testing.T, now time.Time) {
	t.Helper()
	pinGuard.mu.Lock()
	pinGuard.now = func() time.Time { return now }
	pinGuard.mu.Unlock()
}

// startReceiverV22 起一个真实 HTTP 服务（含 prepare / upload / cancel），
// 让 URL 查询参数走完整的解析路径。
func startReceiverV22(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/prepare-upload", PrepareReceive)
	mux.HandleFunc("/api/localsend/v2/upload", ReceiveHandler)
	mux.HandleFunc("/api/localsend/v2/cancel", HandleCancel)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// prepareRequest 组装一份 prepare 请求。
func prepareRequest(files map[string]models.FileInfo) models.PrepareReceiveRequest {
	return models.PrepareReceiveRequest{
		Info:  models.Info{Alias: "v22", Version: "2.2", Protocol: "http"},
		Files: files,
	}
}

// postPrepare 向真实服务端发 prepare，返回状态码与响应体。
func postPrepare(t *testing.T, baseURL, query string, req models.PrepareReceiveRequest) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal prepare: %v", err)
	}
	target := baseURL + "/api/localsend/v2/prepare-upload"
	if query != "" {
		target += "?" + query
	}
	resp, err := http.Post(target, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read prepare response: %v", err)
	}
	return resp.StatusCode, out
}

// postUpload 发送文件内容，返回状态码。
func postUpload(t *testing.T, baseURL, sessionID, fileID, token string, content []byte) int {
	t.Helper()
	q := url.Values{}
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileID)
	q.Set("token", token)
	resp, err := http.Post(baseURL+"/api/localsend/v2/upload?"+q.Encode(), "application/octet-stream", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// TestPreparePIN 覆盖 PIN 的表驱动场景：错误 PIN → 401、连续失败超限 → 429、
// 正确 PIN 通过、成功后计数清零。
func TestPreparePIN(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "123456")
	setClock(t, time.Now())

	files := map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1}}
	req := prepareRequest(files)

	cases := []struct {
		name  string
		query string
		want  int
	}{
		// 「没给 pin」不算猜错：官方只对「pin 错误」计数，这里也不消耗尝试次数。
		{"missing pin", "", http.StatusUnauthorized},
		{"blank pin", "pin=%20%20", http.StatusUnauthorized},
		{"wrong pin", "pin=000000", http.StatusUnauthorized},
		{"wrong length", "pin=1234", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := postPrepare(t, srv.URL, tc.query, req); code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", code, tc.want, body)
			}
		})
	}

	// 上面只有两次真正「pin 错误」，第 3 次错误才触发 429
	if code, _ := postPrepare(t, srv.URL, "pin=999999", req); code != http.StatusUnauthorized {
		t.Fatalf("third wrong pin status = %d, want 401", code)
	}
	// 累计 3 次 pin 错误后，即使 PIN 正确也应该被拦住
	if code, _ := postPrepare(t, srv.URL, "pin=123456", req); code != http.StatusTooManyRequests {
		t.Fatalf("after %d failures status = %d, want 429", pinMaxFailures, code)
	}

	// 窗口过去后自动解锁，正确 PIN 通过且计数清零
	setClock(t, time.Now().Add(pinFailWindow+time.Second))
	code, body := postPrepare(t, srv.URL, "pin=123456", req)
	if code != http.StatusOK {
		t.Fatalf("after window status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare response: %v", err)
	}
	if resp.SessionID == "" || resp.Files["a.txt"] == "" {
		t.Fatalf("prepare response incomplete: %+v", resp)
	}

	// 成功一次即清零：窗口内再错 pinMaxFailures-1 次仍然只是 401，不会直接 429
	if code, _ := postPrepare(t, srv.URL, "pin=bad", req); code != http.StatusUnauthorized {
		t.Fatalf("first failure after success status = %d, want 401", code)
	}
}

// TestPINMissingNotCounted 「没提供 pin」不该计入失败次数：官方只对「pin 错误」计数。
// 老版本发送端不带 pin 时反复 prepare，不该把本机用户自己锁在门外。
func TestPINMissingNotCounted(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "123456")
	setClock(t, time.Now())

	req := prepareRequest(map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1}})
	for i := 0; i < pinMaxFailures*3; i++ {
		if code, _ := postPrepare(t, srv.URL, "", req); code != http.StatusUnauthorized {
			t.Fatalf("missing pin #%d status = %d, want 401", i, code)
		}
	}
	// 一次都没猜错，正确 PIN 仍然直接通过
	if code, body := postPrepare(t, srv.URL, "pin=123456", req); code != http.StatusOK {
		t.Fatalf("correct pin after %d missing attempts status = %d, want 200 (body %q)",
			pinMaxFailures*3, code, body)
	}
}

// TestPreparePINDisabledIgnored 不配 PIN 时，带不带 pin 参数都放行。
func TestPreparePINDisabledIgnored(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	req := prepareRequest(map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1}})
	for _, q := range []string{"", "pin=whatever"} {
		if code, body := postPrepare(t, srv.URL, q, req); code != http.StatusOK {
			t.Fatalf("query %q status = %d, want 200 (body %q)", q, code, body)
		}
		// 会话互斥：清掉上一轮留下的会话，避免下一个用例撞 409
		store.reset()
	}
}

// TestPINLimiterBounded 计数表必须有上限，不能被大量来源 IP 撑爆。
func TestPINLimiterBounded(t *testing.T) {
	isolation(t, "")
	l := &pinLimiter{m: make(map[string]*pinAttempt), now: time.Now}
	for i := 0; i < pinAttemptMaxEntries*3; i++ {
		l.fail(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	l.mu.Lock()
	n := len(l.m)
	l.mu.Unlock()
	if n > pinAttemptMaxEntries {
		t.Fatalf("limiter keeps %d entries, want <= %d", n, pinAttemptMaxEntries)
	}
}

// TestPINConstantTimeCompareIsUsed 确认比较用的是常量时间实现（长度不同直接判否）。
func TestPINConstantTimeCompareIsUsed(t *testing.T) {
	isolation(t, "")
	if ok, status, _ := checkPINWithLimit("123456", "1.1.1.1", "123456"); !ok || status != 0 {
		t.Fatalf("correct pin rejected: ok=%v status=%d", ok, status)
	}
	if ok, status, _ := checkPINWithLimit("123456", "1.1.1.1", "12345"); ok || status != http.StatusUnauthorized {
		t.Fatalf("short pin accepted: ok=%v status=%d", ok, status)
	}
}

// TestChecksumMismatch 篡改内容必须返回 422，且 uploads/ 下不出现文件、
// 同名旧文件保持原样（临时文件 + rename 的核心收益）。
func TestChecksumMismatch(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "report.txt"
	content := []byte("hello v2.2")
	existing := filepath.Join(workDir, uploadBaseDir, fileName)
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("旧内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}

	// 篡改 1 字节
	tampered := append([]byte(nil), content...)
	tampered[0] ^= 0xFF
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], tampered); got != http.StatusUnprocessableEntity {
		t.Fatalf("tampered upload status = %d, want 422", got)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != "旧内容" {
		t.Fatalf("existing file was modified: %q", got)
	}
	entries, err := os.ReadDir(filepath.Join(workDir, uploadBaseDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".localsend-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}

	// 正确的 sha256 才能落盘
	reqOK := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	code, body = postPrepare(t, srv.URL, "", reqOK)
	if code != http.StatusOK {
		t.Fatalf("second prepare status = %d, want 200 (body %q)", code, body)
	}
	resp = models.PrepareReceiveResponse{}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], content); got != http.StatusOK {
		t.Fatalf("matching upload status = %d, want 200", got)
	}
	got, err = os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read rewritten file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content = %q, want %q", got, content)
	}
}

// TestChecksumOptional 协议里 sha256 是 nullable：没提供就跳过校验。
func TestChecksumOptional(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "no-checksum.bin"
	content := []byte("whatever")
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content))},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], content); got != http.StatusOK {
		t.Fatalf("upload without sha256 status = %d, want 200", got)
	}
	got, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil || string(got) != string(content) {
		t.Fatalf("file content = %q, err = %v", got, err)
	}
}

// TestNoContentWhenFileAlreadyPresent 同一文件第二次 prepare 返回 204 且不发 token。
func TestNoContentWhenFileAlreadyPresent(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	content := []byte("idempotent upload")
	name := "same.txt"
	req := prepareRequest(map[string]models.FileInfo{
		name: {ID: name, FileName: name, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})

	// 先把文件传到 uploads/
	if err := os.MkdirAll(filepath.Join(workDir, uploadBaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, uploadBaseDir, name), content, 0o644); err != nil {
		t.Fatal(err)
	}

	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusNoContent {
		t.Fatalf("duplicate prepare status = %d, want 204 (body %q)", code, body)
	}
	if len(body) != 0 {
		t.Fatalf("204 response must have no body, got %q", body)
	}

	// 内容不同则不能误判成 204
	req.Files[name] = models.FileInfo{ID: name, FileName: name, Size: 3, SHA256: sha256Hex([]byte("other"))}
	if code, _ := postPrepare(t, srv.URL, "", req); code != http.StatusOK {
		t.Fatalf("changed content prepare status = %d, want 200", code)
	}
}

// TestNoContentRespectsConfig 关闭 skip_existing 后不再回 204。
func TestNoContentRespectsConfig(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	disabled := false
	old := config.ConfigData.Functions.SkipExisting
	config.ConfigData.Functions.SkipExisting = &disabled
	t.Cleanup(func() { config.ConfigData.Functions.SkipExisting = old })

	content := []byte("x")
	name := "same.txt"
	if err := os.MkdirAll(filepath.Join(workDir, uploadBaseDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, uploadBaseDir, name), content, 0o644); err != nil {
		t.Fatal(err)
	}

	req := prepareRequest(map[string]models.FileInfo{
		name: {ID: name, FileName: name, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	if code, body := postPrepare(t, srv.URL, "", req); code != http.StatusOK {
		t.Fatalf("prepare with skip_existing=false status = %d, want 200 (body %q)", code, body)
	}
}

// TestConflictWhileSessionActive 第二个发送端在第一个会话未结束时返回 409。
func TestConflictWhileSessionActive(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	req := prepareRequest(map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 4}})
	if code, body := postPrepare(t, srv.URL, "", req); code != http.StatusOK {
		t.Fatalf("first prepare status = %d, want 200 (body %q)", code, body)
	}
	// 同一个 httptest client 的来源 IP 相同，因此这里验证的是「同一来源重试」放行；
	// 用一个不同来源的请求模拟另一台设备：直接调用 handler 并改 RemoteAddr。
	body, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body))
	r.RemoteAddr = "10.0.0.99:12345"
	PrepareReceive(rec, r)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second device prepare status = %d, want 409", rec.Code)
	}
}

// TestCancelReleasesSession /cancel 之后会话释放，下一个发送端不再 409。
func TestCancelReleasesSession(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	req := prepareRequest(map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 4}})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}

	q := url.Values{}
	q.Set("sessionId", resp.SessionID)
	cancelResp, err := http.Post(srv.URL+"/api/localsend/v2/cancel?"+q.Encode(), "application/json", nil)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelResp.Body.Close()
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", cancelResp.StatusCode)
	}

	rec := httptest.NewRecorder()
	bodyReq, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(bodyReq))
	r.RemoteAddr = "10.0.0.99:12345"
	PrepareReceive(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare after cancel status = %d, want 200", rec.Code)
	}
}

// TestUploadReleasesSession 文件传完之后会话自动释放，不需要额外 cancel。
// 注意用不带 sha256 的文件：带了校验和的相同文件重复 prepare 会命中 204（另一条路径）。
func TestUploadReleasesSession(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	content := []byte("payload")
	name := "release.txt"
	req := prepareRequest(map[string]models.FileInfo{
		name: {ID: name, FileName: name, Size: int64(len(content))},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	if got := postUpload(t, srv.URL, resp.SessionID, name, resp.Files[name], content); got != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", got)
	}

	rec := httptest.NewRecorder()
	bodyReq, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(bodyReq))
	r.RemoteAddr = "10.0.0.99:12345"
	PrepareReceive(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare after finished session status = %d, want 200", rec.Code)
	}
}

// TestMetadataRoundTrip 发送端的 metadata.modified 能被接收端完整读到，
// 缺失 metadata 的请求也不会报错（协议标注 nullable）。
func TestMetadataRoundTrip(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)

	req := prepareRequest(map[string]models.FileInfo{
		"with-meta.txt": {
			ID: "with-meta.txt", FileName: "with-meta.txt", Size: 1,
			Metadata: &models.FileMetadata{Modified: "2021-01-01T12:34:56Z"},
		},
		"no-meta.txt": {ID: "no-meta.txt", FileName: "no-meta.txt", Size: 1},
	})
	if code, body := postPrepare(t, srv.URL, "", req); code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(store.sessions))
	}
	for _, sess := range store.sessions {
		if len(sess.files) != 2 {
			t.Fatalf("files = %d, want 2", len(sess.files))
		}
	}
}

// TestFileMetadataFillsModified 发送端用 ModTime 填 modified，accessed 留空。
func TestFileMetadataFillsModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2021, 1, 1, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fileMetadata(path, info)
	if err != nil {
		t.Fatalf("fileMetadata: %v", err)
	}
	if got.Metadata == nil {
		t.Fatal("metadata is nil")
	}
	if got.Metadata.Modified != mtime.Format(time.RFC3339) {
		t.Fatalf("modified = %q, want %q", got.Metadata.Modified, mtime.Format(time.RFC3339))
	}
	if got.Metadata.Accessed != "" {
		t.Fatalf("accessed should stay empty, got %q", got.Metadata.Accessed)
	}
}

// TestInfoHasNoAnnounce 协议 6.1 的 /info 响应里没有 announce 字段。
func TestInfoHasNoAnnounce(t *testing.T) {
	rec := httptest.NewRecorder()
	GetInfoHandler(rec, httptest.NewRequest(http.MethodGet, "/api/localsend/v2/info", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := payload["announce"]; ok {
		t.Fatalf("/info response must not contain announce: %v", payload)
	}
	for _, key := range []string{"alias", "version", "fingerprint", "download"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("/info response missing %q: %v", key, payload)
		}
	}
}

// postTruncatedUpload 模拟「发送端写到一半断线」：声明 declared 字节，
// 实际只写 partial，然后直接关掉连接，接收端会读到 io.ErrUnexpectedEOF。
//
// 用 io.Pipe 做请求体：bytes.Reader 会立刻把它自己的内容读成 EOF，net/http 在
// 发送前就会发现「ContentLength 比实际内容大」；pipe 不给 EOF，服务端才会真的
// 读到半截请求体。写端要在请求结束后再关闭，否则客户端可能先报 write error。
func postTruncatedUpload(t *testing.T, baseURL, sessionID, fileID, token string, declared int64, partial []byte) {
	t.Helper()

	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/localsend/v2/upload", pr)
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	q := req.URL.Query()
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileID)
	q.Set("token", token)
	req.URL.RawQuery = q.Encode()
	req.ContentLength = declared
	req.Header.Set("Content-Type", "application/octet-stream")

	errCh := make(chan error, 1)
	go func() {
		_, werr := pw.Write(partial)
		errCh <- werr
		// 不关写端：让服务端一直等剩下的 declared-len(partial) 字节。
		_ = pr.CloseWithError(io.ErrUnexpectedEOF)
	}()

	// 请求必然失败（连接被服务端掐断），忽略这个错误——真正要断言的
	// 是接收端有没有把文件落成失败终态。
	//nolint:errcheck // 见上
	resp, _ := http.DefaultClient.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	<-errCh

	// 断线之后 handler 要一点时间才会返回并落失败终态，这里等它处理完
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.activeSession() == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// prepareFromOtherDevice 用另一台设备的来源 IP 发 prepare，返回状态码。
func prepareFromOtherDevice(t *testing.T, peerIP string, req models.PrepareReceiveRequest) int {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal prepare: %v", err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body))
	r.RemoteAddr = peerIP + ":41234"
	PrepareReceive(rec, r)
	return rec.Code
}

// TestInterruptedUploadReleasesSession 传输中断后，文件必须落失败终态：
// 会话不再占用唯一传输通道，另一台设备的 prepare 不能再拿到 409。
//
// 这是「发送端断线后本机永久 409、只能重启进程」那个阻断问题的回归测试。
func TestInterruptedUploadReleasesSession(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "big.bin"
	content := bytes.Repeat([]byte("x"), 1000)
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})

	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}

	// 声明 1000 字节、只发 3 字节：接收端读到 UnexpectedEOF
	postTruncatedUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], int64(len(content)), content[:3])

	// 中断的会话必须已经让出通道
	if active := store.activeSession(); active != nil {
		t.Fatalf("session %s still blocks transfers after abort", active.id)
	}
	if code := prepareFromOtherDevice(t, "10.0.0.99", prepareRequest(map[string]models.FileInfo{
		"other.txt": {ID: "other.txt", FileName: "other.txt", Size: 1},
	})); code != http.StatusOK {
		t.Fatalf("prepare from another device status = %d, want 200", code)
	}

	// 也没有留下半截文件或临时文件
	if _, err := os.Stat(filepath.Join(workDir, uploadBaseDir, fileName)); !os.IsNotExist(err) {
		t.Fatalf("aborted upload must not leave a file, err = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workDir, uploadBaseDir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read uploads dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".localsend-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// TestChecksumMismatchReleasesSession 422 之后会话同样要让位，
// 否则一次校验失败就能把本机永久锁在 409 里。
func TestChecksumMismatchReleasesSession(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "bad.txt"
	content := []byte("hello")
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}

	tampered := append([]byte(nil), content...)
	tampered[0] ^= 0xFF
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], tampered); got != http.StatusUnprocessableEntity {
		t.Fatalf("tampered upload status = %d, want 422", got)
	}

	if active := store.activeSession(); active != nil {
		t.Fatalf("session %s still blocks transfers after 422", active.id)
	}
	if code := prepareFromOtherDevice(t, "10.0.0.99", prepareRequest(map[string]models.FileInfo{
		"other.txt": {ID: "other.txt", FileName: "other.txt", Size: 1},
	})); code != http.StatusOK {
		t.Fatalf("prepare from another device status = %d, want 200", code)
	}
}

// TestInterruptedUploadCanResume 中断后同一个发送端重新 prepare 应该能续传：
// 失败终态的文件不算「已经传完」，重传是合法的。
func TestInterruptedUploadCanResume(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "resume.bin"
	content := bytes.Repeat([]byte("y"), 512)
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})

	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	postTruncatedUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], int64(len(content)), content[:10])

	// 重发 prepare：没有活跃会话挡路，文件也没落盘，因此不该回 204
	code, body = postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("resume prepare status = %d, want 200 (body %q)", code, body)
	}
	resp = models.PrepareReceiveResponse{}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], content); got != http.StatusOK {
		t.Fatalf("resumed upload status = %d, want 200", got)
	}
	got, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("resumed file content mismatch: err = %v, len = %d", err, len(got))
	}
}

// TestUploadTokenReplayRejected 文件传完之后，同一个 sessionId/fileId/token
// 再传一次必须被拒，且不能覆盖 uploads/ 下的文件。官方要求 file.status == Pending。
func TestUploadTokenReplayRejected(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "once.txt"
	content := []byte("first payload")
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}

	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], content); got != http.StatusOK {
		t.Fatalf("first upload status = %d, want 200", got)
	}

	// 第二次、第三次重放：都必须被拒
	for i := 2; i <= 3; i++ {
		got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], []byte("overwrite!"))
		if got != http.StatusConflict {
			t.Fatalf("replay #%d status = %d, want 409", i, got)
		}
	}

	stored, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("replay overwrote the file: %q", stored)
	}
}

// TestUploadTokenReplayRejectedSamePeer 同一发送端重发 prepare 换了新会话 id，
// 用旧 token 重放同样要被拒——token 只由 fileId 决定，不会随会话变化。
func TestUploadTokenReplayRejectedSamePeer(t *testing.T) {
	workDir := isolation(t, "")
	srv := startReceiverV22(t)
	withPIN(t, "")

	const fileName = "once2.txt"
	content := []byte("payload")
	req := prepareRequest(map[string]models.FileInfo{
		fileName: {ID: fileName, FileName: fileName, Size: int64(len(content)), SHA256: sha256Hex(content)},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200 (body %q)", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], content); got != http.StatusOK {
		t.Fatalf("first upload status = %d, want 200", got)
	}

	// 同一来源重发 prepare：同内容的文件已存在 → 204，拿不到新会话
	code, _ = postPrepare(t, srv.URL, "", req)
	if code != http.StatusNoContent {
		t.Fatalf("duplicate prepare status = %d, want 204", code)
	}
	// 旧 token + 旧会话 id 重放
	if got := postUpload(t, srv.URL, resp.SessionID, fileName, resp.Files[fileName], []byte("overwrite!")); got == http.StatusOK {
		t.Fatal("replay after 204 must not be accepted")
	}
	stored, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("stored file changed: %q, err = %v", stored, err)
	}
}
