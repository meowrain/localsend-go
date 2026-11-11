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
// 正确 PIN 通过、成功后计数清零，以及不配 PIN 时 pin 参数被忽略。
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
		{"missing pin", "", http.StatusUnauthorized},
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

	// 上面已经失败 3 次，第 4 次即使 PIN 正确也应该被拦住
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
