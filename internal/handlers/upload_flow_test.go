package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/meowrain/localsend-go/internal/models"
)

// startReceiver 起一个真实的 HTTP 服务（不是 httptest.ResponseRecorder），
// 让 r.URL.Query() 走完整的 URL 解析路径，最接近线上行为。
func startReceiver(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/prepare-upload", PrepareReceive)
	mux.HandleFunc("/api/localsend/v2/upload", ReceiveHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doPrepare(t *testing.T, baseURL string, files map[string]string) models.PrepareReceiveResponse {
	t.Helper()
	req := models.PrepareReceiveRequest{
		Info:  models.Info{Alias: "e2e", Protocol: "http"},
		Files: map[string]models.FileInfo{},
	}
	for id, name := range files {
		req.Files[id] = models.FileInfo{ID: id, FileName: name, Size: 4}
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(baseURL+"/api/localsend/v2/prepare-upload", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("prepare status = %d", resp.StatusCode)
	}
	var out models.PrepareReceiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	return out
}

func doUpload(t *testing.T, baseURL, sessionID, fileID, token, content string) int {
	t.Helper()
	q := url.Values{}
	q.Set("sessionId", sessionID)
	q.Set("fileId", fileID)
	q.Set("token", token)
	resp, err := http.Post(baseURL+"/api/localsend/v2/upload?"+q.Encode(), "application/octet-stream", bytes.NewReader([]byte(content)))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestEndToEndChineseNameAndForgedToken 端到端验证 issue 里的两条验收标准。
func TestEndToEndChineseNameAndForgedToken(t *testing.T) {
	workDir := chdirTemp(t)
	srv := startReceiver(t)

	const fileName = "我真不想和网瘾少女谈恋爱了！_简体.txt"
	prep := doPrepare(t, srv.URL, map[string]string{fileName: fileName})
	token := prep.Files[fileName]
	if token == "" {
		t.Fatalf("no token issued for %q", fileName)
	}

	if code := doUpload(t, srv.URL, prep.SessionID, fileName, token, "收"); code != http.StatusOK {
		t.Fatalf("legit upload status = %d, want 200", code)
	}
	if _, err := os.Stat(filepath.Join(workDir, uploadBaseDir, fileName)); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}

	// 伪造 token：必须 403，且不能覆盖已有文件内容
	if code := doUpload(t, srv.URL, prep.SessionID, fileName, "token-伪造", "脏数据"); code != http.StatusForbidden {
		t.Fatalf("forged token status = %d, want 403", code)
	}
	got, err := os.ReadFile(filepath.Join(workDir, uploadBaseDir, fileName))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "收" {
		t.Fatalf("forged upload must not touch existing file, content = %q", got)
	}
}

// TestMatchUploadTarget 覆盖接收端改写 fileId 后的回退匹配。
func TestMatchUploadTarget(t *testing.T) {
	f := localFile{path: "/tmp/dir/a.txt", name: "a.txt", relID: "dir/a.txt"}

	if id, tok, ok := matchUploadTarget(map[string]string{"a.txt": "t1"}, f); !ok || id != "a.txt" || tok != "t1" {
		t.Fatalf("basename match failed: %q %q %v", id, tok, ok)
	}
	if id, _, ok := matchUploadTarget(map[string]string{"dir/a.txt": "t2"}, f); !ok || id != "dir/a.txt" {
		t.Fatalf("relative path match failed: %q %v", id, ok)
	}
	// 官方 App 用自己生成的 id 覆盖，只有一个文件时直接采用
	if id, tok, ok := matchUploadTarget(map[string]string{"uuid-1": "t3"}, f); !ok || id != "uuid-1" || tok != "t3" {
		t.Fatalf("single-token fallback failed: %q %q %v", id, tok, ok)
	}
	if _, _, ok := matchUploadTarget(map[string]string{"x": "1", "y": "2"}, f); ok {
		t.Fatal("ambiguous multi-token map should not match")
	}
}

// TestRelativeFileID 覆盖相对路径计算（单文件与目录两种情况）。
func TestRelativeFileID(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "b.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := relativeFileID(dir, file)
	if err != nil || got != "sub/b.txt" {
		t.Fatalf("relativeFileID(dir, file) = %q, %v", got, err)
	}
	got, err = relativeFileID(file, file)
	if err != nil || got != "b.txt" {
		t.Fatalf("relativeFileID(file, file) = %q, %v", got, err)
	}
}
