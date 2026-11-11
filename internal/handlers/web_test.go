package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
)

// webTestServer 起一个带 Web 路由的测试服务（未启用 PIN）。
func webTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	if err := os.MkdirAll(uploadBaseDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", WebAuth(IndexFileHandler))
	mux.HandleFunc("/uploads/", WebAuth(FileServerHandler))
	mux.HandleFunc("/api/web/files", WebAuth(WebListFiles))
	mux.HandleFunc("/api/web/download", WebAuth(WebDownload))
	mux.HandleFunc("/api/web/preview", WebAuth(WebPreview))
	mux.HandleFunc("/api/web/thumb", WebAuth(WebThumb))
	mux.HandleFunc("/api/web/mkdir", WebAuth(WebGuard(WebMkdir)))
	mux.HandleFunc("/api/web/rename", WebAuth(WebGuard(WebRename)))
	mux.HandleFunc("/api/web/delete", WebAuth(WebGuard(WebDelete)))
	mux.HandleFunc("/api/web/upload", WebAuth(WebGuard(WebUpload)))
	mux.HandleFunc("/api/web/upload/chunk", WebAuth(WebGuard(WebChunkUpload)))
	mux.HandleFunc("/api/web/upload/chunk/status", WebAuth(WebChunkStatus))
	mux.HandleFunc("/api/web/history", WebAuth(WebHistory))
	mux.HandleFunc("/api/web/qr", WebAuth(WebQRCode))
	mux.HandleFunc("/api/web/share", WebAuth(WebShareInfo))
	mux.HandleFunc("/api/web/send", WebAuth(WebGuard(WebSend)))
	mux.HandleFunc("/api/web/send/status", WebAuth(WebSendStatus))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// webPost 发送带 CSRF token 的 JSON 写请求。
func webPost(t *testing.T, url string, payload any) (*http.Response, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Web-Token", WebToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, out
}

func webGet(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, out
}

// TestWebPathValidation 覆盖 webRelPath 的穿越场景。
func TestWebPathValidation(t *testing.T) {
	bad := []string{
		"../secret",
		"a/../../b",
		"/etc/passwd",
		"C:/Windows",
		"a/%2e%2e/b",
		"a\x00b",
	}
	for _, p := range bad {
		if got, err := webRelPath(p); err == nil {
			t.Errorf("webRelPath(%q) = %q, want error", p, got)
		}
	}
	good := map[string]string{
		"":          "",
		".":         "",
		"photos":    "photos",
		"photos/":   "photos",
		"a/b/c.txt": "a/b/c.txt",
		"a//b":      "a/b",
		"中文 文件.txt": "中文 文件.txt",
	}
	for in, want := range good {
		got, err := webRelPath(in)
		if err != nil {
			t.Errorf("webRelPath(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("webRelPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWebUploadKeepsStructure 验证多级目录被保留、冲突自动改名。
func TestWebUploadKeepsStructure(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("conflict", "rename")
	// relPath 与 file 按出现顺序一一对应，这里必须保持同样的顺序
	for _, item := range []struct{ rel, name, content string }{
		{"photos/2024/a.txt", "a.txt", "alpha"},
		{"photos/b.txt", "b.txt", "bravo"},
	} {
		_ = mw.WriteField("relPath", item.rel)
		fw, err := mw.CreateFormFile("file", item.name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		_, _ = fw.Write([]byte(item.content))
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/web/upload", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Web-Token", WebToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, body = %s", resp.StatusCode, out)
	}

	for rel, want := range map[string]string{
		"photos/2024/a.txt": "alpha",
		"photos/b.txt":      "bravo",
	} {
		got, err := os.ReadFile(filepath.Join(uploadBaseDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}

	// 再传一次同名文件：rename 策略应生成 "a (1).txt"
	result := uploadOneWebFile(t, srv.URL, "photos/2024", "a.txt", "second", "rename")
	if len(result.Renamed) != 1 {
		t.Fatalf("expected one renamed file, got %+v", result)
	}
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "photos", "2024", "a (1).txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

// uploadOneWebFile 是上面用例的辅助：单文件上传并解析响应。
func uploadOneWebFile(t *testing.T, baseURL, dir, name, content, conflict string) webUploadResult {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("relPath", name)
	_ = mw.WriteField("conflict", conflict)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/web/upload?path="+dir, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Web-Token", WebToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d, body = %s", resp.StatusCode, out)
	}
	var result webUploadResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return result
}

// TestWebUploadConflictStrategies 覆盖 skip / overwrite。
func TestWebUploadConflictStrategies(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.MkdirAll(filepath.Join(uploadBaseDir, "d"), 0o755)
	path := filepath.Join(uploadBaseDir, "d", "x.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	skipped := uploadOneWebFile(t, srv.URL, "d", "x.txt", "new", "skip")
	if len(skipped.Skipped) != 1 || len(skipped.Uploaded) != 0 {
		t.Fatalf("skip result = %+v", skipped)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("skip overwrote file: %q", got)
	}

	uploadOneWebFile(t, srv.URL, "d", "x.txt", "new", "overwrite")
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("overwrite did not replace file: %q", got)
	}
}

// TestWebMkdirRenameDelete 覆盖写操作的正常路径与路径校验。
func TestWebMkdirRenameDelete(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)

	if resp, out := webPost(t, srv.URL+"/api/web/mkdir", map[string]string{"path": "docs"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mkdir = %d %s", resp.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "docs")); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(uploadBaseDir, "docs", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp, out := webPost(t, srv.URL+"/api/web/rename", map[string]string{"from": "docs/a.txt", "to": "docs/b.txt"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename = %d %s", resp.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "docs", "b.txt")); err != nil {
		t.Fatalf("rename target: %v", err)
	}

	// 目标已存在 -> 409
	if resp, _ := webPost(t, srv.URL+"/api/web/rename", map[string]string{"from": "docs/b.txt", "to": "docs/b.txt"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("rename onto existing = %d, want 409", resp.StatusCode)
	}

	// 非递归删除非空目录 -> 409
	if resp, _ := webPost(t, srv.URL+"/api/web/delete", map[string]any{"paths": []string{"docs"}}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete non-empty = %d, want 409", resp.StatusCode)
	}
	// 递归删除 -> 200
	if resp, out := webPost(t, srv.URL+"/api/web/delete", map[string]any{"paths": []string{"docs"}, "recursive": true}); resp.StatusCode != http.StatusOK {
		t.Fatalf("recursive delete = %d %s", resp.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "docs")); !os.IsNotExist(err) {
		t.Fatalf("docs still exists")
	}

	// 删除 uploads 根目录 / 穿越路径必须被拒绝
	for _, p := range []string{"", "/", "../", "a/../../b"} {
		if resp, _ := webPost(t, srv.URL+"/api/web/delete", map[string]any{"paths": []string{p}, "recursive": true}); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("delete %q = %d, want 400", p, resp.StatusCode)
		}
	}
}

// TestWebTokenRequired 验证写接口必须带 X-Web-Token。
func TestWebTokenRequired(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)

	resp, err := http.Post(srv.URL+"/api/web/mkdir", "application/json", strings.NewReader(`{"path":"x"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("mkdir without token = %d, want 403", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "x")); !os.IsNotExist(err) {
		t.Fatalf("directory created without token")
	}
}

// TestWebListAndDownload 覆盖列表、单文件下载与目录 zip。
func TestWebListAndDownload(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.MkdirAll(filepath.Join(uploadBaseDir, "dir"), 0o755)
	_ = os.WriteFile(filepath.Join(uploadBaseDir, "hello.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(uploadBaseDir, "dir", "inner.txt"), []byte("inner"), 0o644)

	resp, out := webGet(t, srv.URL+"/api/web/files?path=")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d %s", resp.StatusCode, out)
	}
	var list struct {
		Entries []webListEntry `json:"entries"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Entries) != 2 {
		t.Fatalf("entries = %+v", list.Entries)
	}
	if !list.Entries[0].IsDir {
		t.Errorf("directories should sort first: %+v", list.Entries)
	}

	resp, out = webGet(t, srv.URL+"/api/web/download?path=hello.txt")
	if resp.StatusCode != http.StatusOK || string(out) != "hello" {
		t.Fatalf("download = %d %q", resp.StatusCode, out)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("download disposition = %q", cd)
	}

	resp, out = webGet(t, srv.URL+"/api/web/download?path=dir")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("zip download = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("zip content-type = %q", ct)
	}
	if len(out) == 0 || !bytes.HasPrefix(out, []byte("PK")) {
		t.Errorf("zip payload looks wrong (%d bytes)", len(out))
	}
}

// TestWebPreviewForcesDownloadForDangerousTypes 验证 HTML 不会被内联渲染。
func TestWebPreviewForcesDownloadForDangerousTypes(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.WriteFile(filepath.Join(uploadBaseDir, "evil.html"), []byte("<script>alert(1)</script>"), 0o644)

	resp, _ := webGet(t, srv.URL+"/api/web/preview?path=evil.html")
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("html preview disposition = %q, want attachment", cd)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("content-type = %q (nosniff is what makes this safe)", ct)
	}

	_ = os.WriteFile(filepath.Join(uploadBaseDir, "pic.txt"), []byte("hi"), 0o644)
	resp, _ = webGet(t, srv.URL+"/api/web/preview?path=pic.txt")
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Fatalf("text preview disposition = %q, want inline", cd)
	}
}

// TestWebPINGate 验证配置 PIN 后页面/API 都要求认证，且 ?pin= 能换取 Cookie。
func TestWebPINGate(t *testing.T) {
	isolation(t, "")
	withPIN(t, "123456")
	srv := webTestServer(t)

	// 无凭证访问 API -> 401
	if resp, _ := webGet(t, srv.URL+"/api/web/files"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api without pin = %d, want 401", resp.StatusCode)
	}
	// 无凭证访问页面 -> 返回登录页（200 + 表单）
	resp, out := webGet(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK || !bytes.Contains(out, []byte(`name="pin"`)) {
		t.Fatalf("page without pin = %d, body has form: %v", resp.StatusCode, bytes.Contains(out, []byte(`name="pin"`)))
	}

	// 错误 PIN -> 401 + 登录页（不设 Cookie）
	resp, out = webGet(t, srv.URL+"/?pin=000000")
	if resp.StatusCode != http.StatusOK || len(resp.Cookies()) != 0 {
		t.Fatalf("wrong pin = %d, cookies = %v", resp.StatusCode, resp.Cookies())
	}
	if !bytes.Contains(out, []byte("PIN 错误")) {
		t.Errorf("wrong pin page missing error hint")
	}

	// 正确 PIN -> 303 + Cookie，之后不带 pin 也能访问
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/?pin=123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "pin=") {
		t.Fatalf("redirect leaks pin: %s", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != webPINCookie {
		t.Fatalf("login cookies = %v", cookies)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/web/files", nil)
	req.AddCookie(cookies[0])
	authed, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authed get: %v", err)
	}
	defer authed.Body.Close()
	if authed.StatusCode != http.StatusOK {
		t.Fatalf("authed api = %d, want 200", authed.StatusCode)
	}
}

// TestWebUploadLimit 验证上传大小上限。
func TestWebUploadLimit(t *testing.T) {
	isolation(t, "")
	old := config.ConfigData.Functions.WebUploadLimitMB
	limit := 1
	config.ConfigData.Functions.WebUploadLimitMB = &limit
	t.Cleanup(func() { config.ConfigData.Functions.WebUploadLimitMB = old })

	srv := webTestServer(t)
	big := bytes.Repeat([]byte("a"), 2<<20) // 2 MiB，超过 1 MiB 上限

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormFile("file", "big.bin")
	_, _ = fw.Write(big)
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/web/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Web-Token", WebToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload = %d, want 413", resp.StatusCode)
	}
}

// TestWebPathTraversalThroughSymlink 验证符号链接无法把 uploads 之外的文件暴露出来。
func TestWebPathTraversalThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permission on Windows requires elevation")
	}
	workDir := isolation(t, "")
	outside := filepath.Join(workDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(uploadBaseDir, 0o755)
	if err := os.Symlink(outside, filepath.Join(uploadBaseDir, "link.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	srv := webTestServer(t)
	if resp, _ := webGet(t, srv.URL+"/api/web/download?path=link.txt"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("symlink download = %d, want 400", resp.StatusCode)
	}
	// 列表里也不应出现这个链接
	_, out := webGet(t, srv.URL+"/api/web/files?path=")
	if bytes.Contains(out, []byte("link.txt")) {
		t.Errorf("escaping symlink listed: %s", out)
	}
}

// TestWebSendToReceiver 端到端：Web 任务把 uploads 下的文件发给一个真实的
// LocalSend 接收端（直接构造请求，不依赖 mDNS 发现）。
func TestWebSendToReceiver(t *testing.T) {
	isolation(t, "")
	if err := os.MkdirAll(filepath.Join(uploadBaseDir, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello from web")
	if err := os.WriteFile(filepath.Join(uploadBaseDir, "dir", "a.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	// 接收端复用真实的 PrepareReceive / ReceiveHandler，与线上走同一套代码。
	recvMux := http.NewServeMux()
	recvMux.HandleFunc("/api/localsend/v2/prepare-upload", PrepareReceive)
	recvMux.HandleFunc("/api/localsend/v2/upload", ReceiveHandler)
	recv := httptest.NewServer(recvMux)
	t.Cleanup(recv.Close)

	spec, err := specForUploadPaths([]string{"dir/a.txt"})
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	// 关闭终端进度条：测试里它只会往 stderr 刷控制字符。
	spec.progress = func(string, int64, int64) {}
	device := models.SendModel{IP: "127.0.0.1", Port: portOf(t, recv.URL), Protocol: "http"}

	if err := prepareAndUpload(spec, &device, nil); err != nil {
		t.Fatalf("send: %v", err)
	}

	// prepare 用的 fileId 是 "dir/a.txt"，但接收端按 fileName 落盘，最终在 uploads/a.txt
	got, err := os.ReadFile(filepath.Join(uploadBaseDir, "a.txt"))
	if err != nil {
		t.Fatalf("received file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("received %q, want %q", got, content)
	}
}

// portOf 从 httptest URL 里取端口。
func portOf(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return p
}

// TestWebSymlinkAncestorBlocked 验证「目标文件尚不存在、但祖先目录是指向外部的符号链接」也会被拒绝。
func TestWebSymlinkAncestorBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permission on Windows requires elevation")
	}
	workDir := isolation(t, "")
	outsideDir := filepath.Join(workDir, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(uploadBaseDir, 0o755)
	if err := os.Symlink(outsideDir, filepath.Join(uploadBaseDir, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	srv := webTestServer(t)
	// 新建目录：目标尚不存在，但父级 link 指向 uploads 之外
	if resp, out := webPost(t, srv.URL+"/api/web/mkdir", map[string]string{"path": "link/new"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mkdir under escaping symlink = %d %s", resp.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new")); !os.IsNotExist(err) {
		t.Fatalf("directory created outside uploads")
	}
}

// TestWebQRCode 验证二维码只接受 http/https。
func TestWebQRCode(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)

	resp, out := webGet(t, srv.URL+"/api/web/qr?url="+url.QueryEscape("http://192.168.1.2:53317/"))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("qr = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if len(out) < 8 || !bytes.HasPrefix(out, []byte("\x89PNG")) {
		t.Fatalf("qr payload is not a PNG (%d bytes)", len(out))
	}

	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
	} {
		if resp, _ := webGet(t, srv.URL+"/api/web/qr?url="+url.QueryEscape(bad)); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("qr(%q) = %d, want 400", bad, resp.StatusCode)
		}
	}
}

// TestWebHistoryRecordsReceive 验证接收成功后会写一条历史记录。
func TestWebHistoryRecordsReceive(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)

	req := prepareRequest(map[string]models.FileInfo{
		"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 5},
	})
	code, body := postPrepare(t, srv.URL, "", req)
	if code != http.StatusOK {
		t.Fatalf("prepare = %d %s", code, body)
	}
	var resp models.PrepareReceiveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	content := []byte("hello")
	if code := postUpload(t, srv.URL, resp.SessionID, "a.txt", resp.Files["a.txt"], content); code != http.StatusOK {
		t.Fatalf("upload = %d", code)
	}

	entries := snapshotHistory()
	if len(entries) != 1 {
		t.Fatalf("history = %+v", entries)
	}
	if entries[0].Direction != "receive" || entries[0].FileName != "a.txt" || entries[0].Size != int64(len(content)) {
		t.Fatalf("history entry = %+v", entries[0])
	}
	if !entries[0].OK || entries[0].SavedPath != "a.txt" {
		t.Fatalf("history entry = %+v", entries[0])
	}
}

// TestWebShareInfo 验证分享信息至少包含一个局域网地址。
func TestWebShareInfo(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	resp, out := webGet(t, srv.URL+"/api/web/share")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("share = %d %s", resp.StatusCode, out)
	}
	var info webShareInfo
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(info.URLs) == 0 {
		t.Skip("no LAN address on this machine")
	}
	if !strings.HasPrefix(info.URLs[0], "http://") {
		t.Fatalf("share url = %q", info.URLs[0])
	}
	if !strings.HasPrefix(info.QR, "data:image/png;base64,") {
		t.Errorf("share qr missing")
	}
}

// TestWebSendJobLifecycle 走完整的 HTTP 路径：提交任务 -> 轮询 -> 看到完成。
func TestWebSendJobLifecycle(t *testing.T) {
	isolation(t, "")
	if err := os.MkdirAll(uploadBaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadBaseDir, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	recvMux := http.NewServeMux()
	recvMux.HandleFunc("/api/localsend/v2/prepare-upload", PrepareReceive)
	recvMux.HandleFunc("/api/localsend/v2/upload", ReceiveHandler)
	recv := httptest.NewServer(recvMux)
	t.Cleanup(recv.Close)

	// 发送端与接收端共享同一个临时 uploads/，同名同内容会命中 204 跳过；
	// 这里关掉 skip_existing，才能真正验证上传进度。
	oldSkip := config.ConfigData.Functions.SkipExisting
	disabled := false
	config.ConfigData.Functions.SkipExisting = &disabled
	t.Cleanup(func() { config.ConfigData.Functions.SkipExisting = oldSkip })

	srv := webTestServer(t)
	payload := map[string]any{
		"device": map[string]any{
			"alias": "peer", "ip": "127.0.0.1", "port": portOf(t, recv.URL), "protocol": "http",
		},
		"paths": []string{"a.txt"},
	}
	resp, out := webPost(t, srv.URL+"/api/web/send", payload)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("send = %d %s", resp.StatusCode, out)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &created); err != nil || created.ID == "" {
		t.Fatalf("decode job: %v %s", err, out)
	}

	var status map[string]any
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		_, body := webGet(t, srv.URL+"/api/web/send/status?id="+created.ID)
		if err := json.Unmarshal(body, &status); err != nil {
			t.Fatalf("decode status: %v %s", err, body)
		}
		if status["state"] == "done" || status["state"] == "error" {
			break
		}
	}
	if status["state"] != "done" {
		t.Fatalf("job state = %v (message %v)", status["state"], status["message"])
	}
	if got, _ := status["sent"].(float64); got != 7 {
		t.Fatalf("sent = %v, want 7", status["sent"])
	}

	// 接收端按 fileName 落盘，这里是 uploads/a.txt（发送端自己也有同名文件，会被覆盖成同样的内容）
	if got, err := os.ReadFile(filepath.Join(uploadBaseDir, "a.txt")); err != nil || string(got) != "payload" {
		t.Fatalf("received = %q, %v", got, err)
	}

	// 历史里应有一条发送记录
	var foundSend bool
	for _, e := range snapshotHistory() {
		if e.Direction == "send" && e.FileName == "a.txt" {
			foundSend = true
		}
	}
	if !foundSend {
		t.Fatalf("send history missing: %+v", snapshotHistory())
	}
}

// TestWebUploadRejectsEncodedTraversal 验证 relPath 里的编码分隔符不会被放行。
func TestWebUploadRejectsEncodedTraversal(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	for _, rel := range []string{"../evil.txt", "a/%2e%2e/x.txt", "..%2fevil.txt", "a\\..\\evil.txt"} {
		body := &bytes.Buffer{}
		mw := multipart.NewWriter(body)
		_ = mw.WriteField("relPath", rel)
		fw, _ := mw.CreateFormFile("file", "a.txt")
		_, _ = fw.Write([]byte("x"))
		_ = mw.Close()

		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/web/upload", body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Web-Token", WebToken())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("relPath %q = %d %s, want 400", rel, resp.StatusCode, out)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(uploadBaseDir), "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("file escaped uploads")
	}
}

// TestWebChunkedUpload 覆盖分片上传：分批写入 -> 续传查询 -> 收齐落盘。
func TestWebChunkedUpload(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	_ = os.MkdirAll(uploadBaseDir, 0o755)

	content := bytes.Repeat([]byte("chunked-data-"), 1000) // ~13 KB
	total := len(content)
	uploadID := strings.Repeat("ab", 16)

	post := func(offset, end int) *http.Response {
		req, _ := http.NewRequest(http.MethodPost,
			fmt.Sprintf("%s/api/web/upload/chunk?uploadId=%s&path=big&relPath=big/file.bin&offset=%d&total=%d",
				srv.URL, uploadID, offset, total),
			bytes.NewReader(content[offset:end]))
		req.Header.Set("X-Web-Token", WebToken())
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chunk post: %v", err)
		}
		return resp
	}

	// 第一片
	resp := post(0, 5000)
	var first map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&first)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || first["done"] == true {
		t.Fatalf("first chunk = %d %+v", resp.StatusCode, first)
	}

	// 查询断点
	_, out := webGet(t, srv.URL+"/api/web/upload/chunk/status?uploadId="+uploadID+"&path=big")
	var status struct{ Offset int64 }
	if err := json.Unmarshal(out, &status); err != nil || status.Offset != 5000 {
		t.Fatalf("status = %s (offset %d)", out, status.Offset)
	}

	// offset 错位 -> 409 且带服务器真实 offset
	resp = post(4000, 6000)
	var mismatch map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&mismatch)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || mismatch["offset"].(float64) != 5000 {
		t.Fatalf("mismatch = %d %+v", resp.StatusCode, mismatch)
	}

	// 续传到结束
	resp = post(5000, total)
	var last map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&last)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || last["done"] != true {
		t.Fatalf("last chunk = %d %+v", resp.StatusCode, last)
	}
	if last["path"] != "big/big/file.bin" {
		t.Fatalf("saved path = %v", last["path"])
	}

	got, err := os.ReadFile(filepath.Join(uploadBaseDir, "big", "big", "file.bin"))
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %d bytes", len(got))
	}
	// part 文件必须已被 rename 掉
	if _, err := os.Stat(filepath.Join(uploadBaseDir, "big", ".upload-"+uploadID+".part")); !os.IsNotExist(err) {
		t.Fatalf("part file left behind")
	}
}

// TestWebChunkedUploadRejectsBadInput 覆盖 uploadId/offset/total 的校验。
func TestWebChunkedUploadRejectsBadInput(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)

	post := func(query string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/web/upload/chunk?"+query, bytes.NewReader([]byte("x")))
		req.Header.Set("X-Web-Token", WebToken())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("chunk post: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	cases := map[string]struct {
		query string
		want  int
	}{
		"bad id":       {"uploadId=../../etc&path=&relPath=a&offset=0&total=1", http.StatusBadRequest},
		"bad offset":   {"uploadId=" + strings.Repeat("ab", 16) + "&path=&relPath=a&offset=-1&total=1", http.StatusBadRequest},
		"bad total":    {"uploadId=" + strings.Repeat("ab", 16) + "&path=&relPath=a&offset=0&total=0", http.StatusBadRequest},
		"traversal":    {"uploadId=" + strings.Repeat("ab", 16) + "&path=../&relPath=a&offset=0&total=1", http.StatusBadRequest},
		"missing name": {"uploadId=" + strings.Repeat("ab", 16) + "&path=&offset=0&total=1", http.StatusBadRequest},
	}
	for name, c := range cases {
		if got := post(c.query); got != c.want {
			t.Errorf("%s: status = %d, want %d", name, got, c.want)
		}
	}
}

// TestWebPageHasViewer 验证页面里包含图片查看器的骨架与静态资源引用。
func TestWebPageHasViewer(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	resp, out := webGet(t, srv.URL+"/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page = %d", resp.StatusCode)
	}
	html := string(out)
	for _, want := range []string{
		`id="imageViewer"`,
		`id="viewerStage"`,
		`id="viewerImg"`,
		`data-v="zoom-in"`,
		`data-v="zoom-out"`,
		`data-v="rotate-right"`,
		`data-v="fullscreen"`,
		`/static/js/web.js`,
		`/static/css/web.css`,
		// 外观主题：按钮 + 面板骨架 + 首屏防闪白内联脚本
		`id="btnTheme"`,
		`id="themeMenu"`,
		`id="themeList"`,
		`data-mode="system"`,
		`localsendgo-theme`,
		`setAttribute('data-mode'`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %s", want)
		}
	}
	// 页面不应再依赖外部 CDN（离线可用）
	for _, bad := range []string{"unpkg.com", "fonts.googleapis.com", "cdn.jsdelivr.net"} {
		if strings.Contains(html, bad) {
			t.Errorf("page still references a CDN: %s", bad)
		}
	}
}

// TestWebThemesCSS 验证多套主题的令牌都在样式表里，且每套都有浅色与深色两版。
func TestWebThemesCSS(t *testing.T) {
	isolation(t, "")
	srv := webTestServer(t)
	resp, out := webGet(t, srv.URL+"/static/css/web.css")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("css = %d", resp.StatusCode)
	}
	css := string(out)
	for _, theme := range []string{"indigo", "forest", "ember", "tide", "rose", "synth"} {
		for _, mode := range []string{"light", "dark"} {
			sel := `[data-theme="` + theme + `"][data-mode="` + mode + `"]`
			if !strings.Contains(css, sel) {
				t.Errorf("css is missing theme block %s", sel)
			}
		}
	}
	// 旧的 data-theme="dark" 单段式选择器已不再使用
	if strings.Contains(css, `:root[data-theme="dark"]`) {
		t.Errorf("css still uses the legacy single-axis data-theme="dark" selector")
	}
	// 明暗由 JS 解析后写到 data-mode，样式表里不再需要明暗媒体查询
	if strings.Contains(css, "@media (prefers-color-scheme") {
		t.Errorf("css still branches on prefers-color-scheme; resolved mode should be in data-mode")
	}
	// 每套主题都必须覆盖按钮前景色，避免主色变浅后白字失读
	if n := strings.Count(css, "--on-primary:"); n < 13 {
		t.Errorf("--on-primary defined %d times, want >= 13 (root + 6 themes x 2 modes)", n)
	}
}
