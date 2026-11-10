package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/meowrain/localsend-go/internal/models"
)

// prepare 走一遍真实的 PrepareReceive handler，返回 (sessionID, tokens)。
// workDir 会被切换到临时目录，保证落盘文件不污染仓库。
func prepare(t *testing.T, dir string, files map[string]string) (string, map[string]string) {
	t.Helper()
	req := models.PrepareReceiveRequest{
		Info:  models.Info{Alias: "tester", Protocol: "http", Port: 53317},
		Files: map[string]models.FileInfo{},
	}
	for id, name := range files {
		req.Files[id] = models.FileInfo{ID: id, FileName: name, Size: 1}
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal prepare request: %v", err)
	}

	rec := httptest.NewRecorder()
	PrepareReceive(rec, httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PrepareReceive status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode prepare response: %v", err)
	}
	return resp.SessionID, resp.Files
}

// chdirTemp 把工作目录切到临时目录，结束后还原。
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func uploadRequest(sessionID, fileID, token, content string) *http.Request {
	q := url.Values{}
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileID)
	q.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/upload?"+q.Encode(), strings.NewReader(content))
	req.ContentLength = int64(len(content))
	return req
}

// TestChineseFileIDURLRoundTrip 覆盖「中文 fileId 的 URL 往返」：
// 发送端用 url.Values 编码，接收端用 r.URL.Query() 解码，必须拿回原始中文。
func TestChineseFileIDURLRoundTrip(t *testing.T) {
	workDir := chdirTemp(t)
	const fileName = "我真不想和网瘾少女谈恋爱了！_简体.txt"
	const contents = "hello 中文内容"

	sessionID, tokens := prepare(t, workDir, map[string]string{fileName: fileName})
	if len(tokens) != 1 {
		t.Fatalf("prepare returned %d tokens, want 1", len(tokens))
	}
	token, ok := tokens[fileName]
	if !ok {
		t.Fatalf("token for Chinese fileId missing, got %v", tokens)
	}

	// 与 send.go 完全一致的构造方式
	q := url.Values{}
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileName)
	q.Set("token", token)
	encoded := q.Encode()
	if !strings.Contains(encoded, "%") {
		t.Fatalf("query should be percent-encoded, got %q", encoded)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/upload?"+encoded, strings.NewReader(contents))
	req.ContentLength = int64(len(contents))
	rec := httptest.NewRecorder()
	ReceiveHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ReceiveHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	got, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(got) != contents {
		t.Fatalf("uploaded content = %q, want %q", got, contents)
	}
}

// TestSpecialCharsFileID 覆盖空格、&、#、%、+ 等需要转义的字符。
func TestSpecialCharsFileID(t *testing.T) {
	workDir := chdirTemp(t)
	const fileName = "a b&c#d%e+f.txt"

	sessionID, tokens := prepare(t, workDir, map[string]string{fileName: fileName})
	token := tokens[fileName]

	rec := httptest.NewRecorder()
	ReceiveHandler(rec, uploadRequest(sessionID, fileName, token, "x"))
	if rec.Code != http.StatusOK {
		t.Fatalf("ReceiveHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, uploadBaseDir, fileName)); err != nil {
		t.Fatalf("expected file %q to exist: %v", fileName, err)
	}
}

// TestReceiveHandlerTokenMismatchForbidden 校验 token 与 prepare 阶段下发的不一致时返回 403，
// 且不会在 uploads/ 下创建任何文件。
func TestReceiveHandlerTokenMismatchForbidden(t *testing.T) {
	workDir := chdirTemp(t)
	const fileName = "note.txt"

	sessionID, tokens := prepare(t, workDir, map[string]string{fileName: fileName})
	goodToken := tokens[fileName]

	cases := []struct {
		name    string
		session string
		fileID  string
		token   string
		want    int
	}{
		{"wrong token", sessionID, fileName, "token-forged", http.StatusForbidden},
		{"empty token", sessionID, fileName, "", http.StatusForbidden},
		{"unknown fileId", sessionID, "other.txt", goodToken, http.StatusForbidden},
		{"unknown session", "session-unknown", fileName, goodToken, http.StatusForbidden},
		{"missing session", "", fileName, goodToken, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ReceiveHandler(rec, uploadRequest(tc.session, tc.fileID, tc.token, "x"))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	entries, err := os.ReadDir(filepath.Join(workDir, uploadBaseDir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read uploads dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected uploads must not create files, found %d entries", len(entries))
	}
}

// TestPathTraversalRejected 覆盖各种路径穿越 / 绝对路径文件名。
func TestPathTraversalRejected(t *testing.T) {
	workDir := chdirTemp(t)

	badNames := []string{
		"../escape.txt",
		"../../escape.txt",
		"..%2Fescape.txt", // 已经解码后的形态
		"/etc/passwd",
		`C:\Windows\System32\evil.dll`,
		`..\..\escape.txt`,
		"..",
		".",
		"dir/../../file.txt",
		"a\x00b",
		".. ",
	}

	for _, name := range badNames {
		req := models.PrepareReceiveRequest{
			Info: models.Info{Alias: "tester"},
			Files: map[string]models.FileInfo{
				"id-1": {ID: "id-1", FileName: name, Size: 1},
			},
		}
		body, _ := json.Marshal(req)
		rec := httptest.NewRecorder()
		PrepareReceive(rec, httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PrepareReceive(%q) status = %d, want 400", name, rec.Code)
		}
	}

	// 落盘路径必须始终位于 uploads/ 内
	outside := filepath.Join(workDir, "escape.txt")
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("path traversal created file outside uploads: %s", outside)
	}
}

// TestSanitizeFileName 覆盖文件名规整逻辑。
func TestSanitizeFileName(t *testing.T) {
	ok := map[string]string{
		"a.txt":        "a.txt",
		"中文名.txt":      "中文名.txt",
		"dir/file.txt": "file.txt", // 带目录的只取 basename
		"  sp.txt  ":   "sp.txt",
	}
	for in, want := range ok {
		got, err := sanitizeFileName(in)
		if err != nil {
			t.Fatalf("sanitizeFileName(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Fatalf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}

	for _, in := range []string{"", "..", "../x", "/abs.txt", `C:\x.txt`, "a\x00b"} {
		if _, err := sanitizeFileName(in); err == nil {
			t.Fatalf("sanitizeFileName(%q) should fail", in)
		}
	}
}

// TestSafeJoinUnder 直接验证拼接结果不会逃出 base 目录。
func TestSafeJoinUnder(t *testing.T) {
	base := t.TempDir()
	got, err := safeJoinUnder(base, "ok.txt")
	if err != nil {
		t.Fatalf("safeJoinUnder: %v", err)
	}
	want := filepath.Join(base, "ok.txt")
	if got != want {
		t.Fatalf("safeJoinUnder = %q, want %q", got, want)
	}
	for _, bad := range []string{"../x", "/x", "..", "a/../../b"} {
		if _, err := safeJoinUnder(base, bad); err == nil {
			t.Fatalf("safeJoinUnder(%q) should fail", bad)
		}
	}
}

// TestConcurrentPrepareAndUpload 用 -race 跑并发的 Prepare / Upload，验证 fileNames 一类的共享 map 已加锁。
func TestConcurrentPrepareAndUpload(t *testing.T) {
	chdirTemp(t)

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			fileName := fmt.Sprintf("并发文件-%d.txt", i)
			req := models.PrepareReceiveRequest{
				Info:  models.Info{Alias: "tester"},
				Files: map[string]models.FileInfo{fileName: {ID: fileName, FileName: fileName, Size: 1}},
			}
			body, _ := json.Marshal(req)
			rec := httptest.NewRecorder()
			PrepareReceive(rec, httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body)))
			if rec.Code != http.StatusOK {
				t.Errorf("PrepareReceive status = %d", rec.Code)
				return
			}
			var resp models.PrepareReceiveResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Errorf("decode response: %v", err)
				return
			}
			rec2 := httptest.NewRecorder()
			ReceiveHandler(rec2, uploadRequest(resp.SessionID, fileName, resp.Files[fileName], "data"))
			if rec2.Code != http.StatusOK {
				t.Errorf("ReceiveHandler status = %d, body = %s", rec2.Code, rec2.Body.String())
			}
		}(i)
	}
	wg.Wait()
}

// TestSessionStoreEviction 验证会话数量上限，避免长跑进程内存无限增长。
func TestSessionStoreEviction(t *testing.T) {
	store.reset()
	t.Cleanup(store.reset)

	for i := 0; i < maxSessions+10; i++ {
		store.add(&receiveSession{id: fmt.Sprintf("s-%d", i), tokens: map[string]string{}, names: map[string]string{}})
	}
	store.mu.RLock()
	n := len(store.sessions)
	store.mu.RUnlock()
	if n > maxSessions {
		t.Fatalf("store holds %d sessions, want <= %d", n, maxSessions)
	}
}
