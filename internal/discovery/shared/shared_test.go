package shared

import (
	"encoding/json"
	"testing"

	"github.com/meowrain/localsend-go/internal/models"
)

// TestProtocolVersionIs22 锁死协议版本：本 Issue 只对齐 v2.2（v3 尚未定稿）。
// 有人要改成 3.x 时，这个测试会强制他先读 Issue 里的判断依据。
func TestProtocolVersionIs22(t *testing.T) {
	if ProtocolVersion != "2.2" {
		t.Fatalf("ProtocolVersion = %q, want 2.2", ProtocolVersion)
	}
	if Message.Version != ProtocolVersion {
		t.Fatalf("Message.Version = %q, want %q", Message.Version, ProtocolVersion)
	}
}

// TestDownloadIsDisabledUntilImplemented 广播里的 download 必须与实现一致：
// 下载 API（协议 5.2 / 5.3）未实现，就不能对外报 true。
func TestDownloadIsDisabledUntilImplemented(t *testing.T) {
	if Message.Download {
		t.Fatal("Message.Download = true, but the download API is not implemented")
	}
}

func TestSetListenPort(t *testing.T) {
	old := Message.Port
	t.Cleanup(func() { Message.Port = old })

	SetListenPort(53318)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want 53318", Message.Port)
	}

	// 非法端口不应该覆盖已同步的值
	SetListenPort(0)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want unchanged 53318", Message.Port)
	}
	SetListenPort(-1)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want unchanged 53318", Message.Port)
	}
}

// TestAnnouncementJSONSetsAnnounceTrue 锁死公告载荷里必须出现 announce: true。
//
// 协议 3.1：「A response is only triggered when announce is true」。Message 就是
// UDP 多播公告的发送体（udp_broadcast.go 里直接 json.Marshal(Message)），
// 一旦它变成 false，本机的公告收到的只有沉默——双向发现在两个方向上全断。
// 这条测试断言的是序列化后的实际载荷，而不是变量值，避免将来又把「干净字段」
// 这个理由搬回来。
func TestAnnouncementJSONSetsAnnounceTrue(t *testing.T) {
	payload, err := json.Marshal(Message)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal announcement: %v", err)
	}

	announce, ok := fields["announce"]
	if !ok {
		t.Fatalf("announcement has no announce field: %s", payload)
	}
	if got, isBool := announce.(bool); !isBool || !got {
		t.Fatalf("announcement announce = %v, want true", announce)
	}
}

// TestRegisterBodyOmitsAnnounce register 请求体没有 announce 字段（协议 3.1 / 3.2），
// 这一点由独立类型 models.RegisterRequest 保证，而不是靠把公告的 announce 关掉。
func TestRegisterBodyOmitsAnnounce(t *testing.T) {
	payload, err := json.Marshal(RegisterRequest())
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal register request: %v", err)
	}
	if _, ok := fields["announce"]; ok {
		t.Fatalf("register body must not carry announce: %s", payload)
	}
}

// TestRegisterRequestBodyFields 校验两个载荷共用同一份设备信息，只是字段集合不同。
func TestRegisterRequestBodyFields(t *testing.T) {
	old := Message
	t.Cleanup(func() { Message = old })
	Message = models.BroadcastMessage{
		Alias:       "npc-go",
		Version:     ProtocolVersion,
		DeviceModel: "Linux",
		DeviceType:  "headless",
		Fingerprint: "fp-test",
		Port:        53317,
		Protocol:    "http",
		Download:    false,
		Announce:    true,
	}

	req := RegisterRequest()
	if req.Alias != "npc-go" || req.Version != ProtocolVersion || req.DeviceModel != "Linux" {
		t.Errorf("identity fields not copied: %+v", req)
	}
	if req.DeviceType != "headless" || req.Fingerprint != "fp-test" {
		t.Errorf("device fields not copied: %+v", req)
	}
	if req.Port != 53317 || req.Protocol != "http" || req.Download != false {
		t.Errorf("network fields not copied: %+v", req)
	}
}
