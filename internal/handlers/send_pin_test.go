package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
)

// TestSendPreparesWithPeerPINNotReceiverPIN 发送时带的是「对端 PIN」，
// 不能把本机接收端的 PIN 拿去开对端。
func TestSendPreparesWithPeerPINNotReceiverPIN(t *testing.T) {
	var gotPIN []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPIN = append(gotPIN, r.URL.Query().Get("pin"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models.PrepareReceiveResponse{
			SessionID: "session-1",
			Files:     map[string]string{"a.txt": "token-a.txt"},
		})
	}))
	t.Cleanup(srv.Close)

	oldReceiver, oldSend := config.ConfigData.Functions.PIN, config.ConfigData.Functions.SendPIN
	t.Cleanup(func() {
		config.ConfigData.Functions.PIN = oldReceiver
		config.ConfigData.Functions.SendPIN = oldSend
	})

	// 本机接收端要求的 PIN 与对端要求的 PIN 故意设成不同值
	config.ConfigData.Functions.PIN = "111111"
	config.ConfigData.Functions.SendPIN = "222222"

	device := deviceFor(t, srv)
	_, err := sendPrepareRequest(device, prepareRequest(map[string]models.FileInfo{
		"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1},
	}))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(gotPIN) != 1 || gotPIN[0] != "222222" {
		t.Fatalf("prepare sent pin %v, want [222222] (receiver pin 111111 must not leak)", gotPIN)
	}

	// 没配 send pin 时不该凭空带上本机接收端的 pin
	config.ConfigData.Functions.SendPIN = ""
	gotPIN = nil
	if _, err := sendPrepareRequest(device, prepareRequest(map[string]models.FileInfo{
		"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1},
	})); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(gotPIN) != 1 || gotPIN[0] != "" {
		t.Fatalf("prepare sent pin %v, want empty", gotPIN)
	}
}

// TestReceiverPINUnaffectedBySendPIN 给发送端设置对端 PIN 不会改变本机接收端的校验。
func TestReceiverPINUnaffectedBySendPIN(t *testing.T) {
	isolation(t, "")
	srv := startReceiverV22(t)

	oldSend := config.ConfigData.Functions.SendPIN
	t.Cleanup(func() { config.ConfigData.Functions.SendPIN = oldSend })

	withPIN(t, "123456")
	config.ConfigData.SetSendPIN("654321") // 只影响发送端

	req := prepareRequest(map[string]models.FileInfo{"a.txt": {ID: "a.txt", FileName: "a.txt", Size: 1}})

	// 本机接收端仍然只认 123456
	if code, _ := postPrepare(t, srv.URL, "pin=654321", req); code != http.StatusUnauthorized {
		t.Fatalf("peer pin accepted by local receiver: status = %d, want 401", code)
	}
	if code, body := postPrepare(t, srv.URL, "pin=123456", req); code != http.StatusOK {
		t.Fatalf("receiver pin rejected: status = %d, body = %s", code, body)
	}
}
