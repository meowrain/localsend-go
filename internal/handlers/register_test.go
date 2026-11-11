package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
)

// withSharedMessage 临时替换本机广播信息，测试结束后恢复，避免测试之间互相污染。
func withSharedMessage(t *testing.T, msg models.BroadcastMessage) {
	t.Helper()
	old := shared.Message
	shared.Message = msg
	t.Cleanup(func() { shared.Message = old })
}

func testBroadcastMessage() models.BroadcastMessage {
	return models.BroadcastMessage{
		Alias:       "npc-go",
		Version:     shared.ProtocolVersion,
		DeviceModel: "Linux",
		DeviceType:  "headless",
		Fingerprint: "fp-test",
		Port:        53317,
		Protocol:    "http",
		Download:    false,
		Announce:    true,
	}
}

// TestRegisterRejectsNonPost 校验只有 POST 被接受：协议里 register 是 POST 语义，
// GET 落到这里说明对端用错了方法，应该给出 405 而不是把它当成 /info。
func TestRegisterRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	RegisterHandler(rec, httptest.NewRequest(http.MethodGet, "/api/localsend/v2/register", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow header = %q, want %q", allow, http.MethodPost)
	}
}

// TestRegisterParsesRequestAndReturnsDeviceInfo 覆盖验收标准里「请求解析 + 响应字段」。
func TestRegisterParsesRequestAndReturnsDeviceInfo(t *testing.T) {
	withSharedMessage(t, testBroadcastMessage())

	body, err := json.Marshal(models.RegisterRequest{
		Alias:       "Nice Orange",
		Version:     "2.2",
		DeviceModel: "Samsung",
		DeviceType:  "mobile",
		Fingerprint: "peer-fp",
		Port:        53318,
		Protocol:    "https",
		Download:    true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(RegisterHandler))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/localsend/v2/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var got models.RegisterResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal response %q: %v", raw, err)
	}

	want := models.RegisterResponse{
		Alias:       "npc-go",
		Version:     shared.ProtocolVersion,
		DeviceModel: "Linux",
		DeviceType:  "headless",
		Fingerprint: "fp-test",
		Port:        53317,
		Protocol:    "http",
		Download:    false,
	}
	if got != want {
		t.Fatalf("response = %+v, want %+v", got, want)
	}

	// 响应必须能被发送侧解析成 BroadcastMessage（HTTP 探测就是这么用的）
	var asBroadcast models.BroadcastMessage
	if err := json.Unmarshal(raw, &asBroadcast); err != nil {
		t.Fatalf("response should be parsable as BroadcastMessage: %v", err)
	}
	if asBroadcast.Alias != got.Alias || asBroadcast.Port != got.Port {
		t.Fatalf("broadcast view = %+v, response = %+v", asBroadcast, got)
	}
}

// TestRegisterToleratesEmptyBody 对端不发请求体时仍然要返回本机信息（退化成 /info），
// 这样旧版客户端至少能发现本机，而不是拿到 400。
func TestRegisterToleratesEmptyBody(t *testing.T) {
	withSharedMessage(t, testBroadcastMessage())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/localsend/v2/register", nil)
	RegisterHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got models.RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != shared.ProtocolVersion {
		t.Fatalf("version = %q, want %q", got.Version, shared.ProtocolVersion)
	}
}
