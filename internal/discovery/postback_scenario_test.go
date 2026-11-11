package discovery

import (
	"encoding/json"
	"testing"
	"time"

	shared "github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
)

// TestTwoInstancesWithDifferentFingerprintsPostBack 是本条审查意见要求的验收测试：
// 模拟两台机器，B 的公告指纹与 A 的本机指纹不同 → A 必须向 B 回发 register。
// 反过来，指纹相同（多播环回，等于自己发的包）时不回发。
func TestTwoInstancesWithDifferentFingerprintsPostBack(t *testing.T) {
	machineA := models.BroadcastMessage{
		Alias:       "machine-a",
		Version:     "2.2",
		DeviceType:  "headless",
		Fingerprint: shared.NewFingerprint(),
		Port:        53317,
		Protocol:    "http",
	}
	machineB := models.BroadcastMessage{
		Alias:       "machine-b",
		Version:     "2.2",
		DeviceType:  "headless",
		Fingerprint: shared.NewFingerprint(),
		Port:        53317,
		Protocol:    "http",
	}
	if machineA.Fingerprint == machineB.Fingerprint {
		t.Fatal("two instances generated the same fingerprint")
	}

	// A 视角：本机指纹 = A，收到 B 的公告
	withLocalMessage(t, machineA)
	announcementFromB := machineB
	announcementFromB.Announce = true

	fake := &fakePoster{}
	reply(t, nil, "192.168.1.20", announcementFromB, fake.post)

	if len(fake.calls) != 1 {
		t.Fatalf("poster called %d times, want 1 (two distinct machines must post back)", len(fake.calls))
	}
	if got := fake.calls[0].Fingerprint; got != machineB.Fingerprint {
		t.Errorf("postback target fingerprint = %q, want %q", got, machineB.Fingerprint)
	}

	// 同一个 A 收到「自己」的公告（多播环回）时不回发
	announcementFromA := machineA
	announcementFromA.Announce = true
	fakeSelf := &fakePoster{}
	reply(t, nil, "192.168.1.10", announcementFromA, fakeSelf.post)

	if len(fakeSelf.calls) != 0 {
		t.Fatalf("poster called %d times for own announcement, want 0", len(fakeSelf.calls))
	}
}

// TestAnnouncementWithoutDeviceTypeIsAccepted 校验协议 3.1 的 nullable 语义：
// 只发 alias（没有 deviceType）的公告是合法的，必须接受并回退 desktop。
func TestAnnouncementWithoutDeviceTypeIsAccepted(t *testing.T) {
	raw := []byte(`{"alias":"Nice Orange","version":"2.2","deviceModel":null,"fingerprint":"p","port":53317,"protocol":"http","announce":true}`)

	var msg models.BroadcastMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.DeviceType != "" {
		t.Fatalf("DeviceType = %q, want empty before normalization", msg.DeviceType)
	}
	// 与 udp_broadcast 的处理顺序一致：先校验 alias，再规范化 deviceType
	if msg.Alias == "" {
		t.Fatal("alias must be the only required field")
	}
	if got := normalizeDeviceType(msg.DeviceType); got != "desktop" {
		t.Fatalf("normalizeDeviceType(%q) = %q, want desktop", msg.DeviceType, got)
	}
}

func TestNormalizeDeviceType(t *testing.T) {
	cases := map[string]string{
		"":         "desktop",
		"  ":       "desktop",
		"desktop":  "desktop",
		"Mobile":   "mobile",
		" WEB ":    "web",
		"headless": "headless",
		"server":   "server",
		"toaster":  "desktop",
	}
	for in, want := range cases {
		if got := normalizeDeviceType(in); got != want {
			t.Errorf("normalizeDeviceType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPostbackDedupWithinTTL 同一来源 + 同一指纹在 TTL 内只回发一次。
func TestPostbackDedupWithinTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dedup := freshDeduper()
	withFixedNow(dedup, &now)

	withLocalMessage(t, models.BroadcastMessage{Fingerprint: "self-fp"})
	msg := announcement(true)
	fake := &fakePoster{}

	reply(t, dedup, "192.168.1.10", msg, fake.post)
	reply(t, dedup, "192.168.1.10", msg, fake.post)
	if len(fake.calls) != 1 {
		t.Fatalf("poster called %d times within TTL, want 1", len(fake.calls))
	}

	// 过了 TTL 之后允许再回发
	now = now.Add(postbackTTL + time.Second)
	reply(t, dedup, "192.168.1.10", msg, fake.post)
	if len(fake.calls) != 2 {
		t.Fatalf("poster called %d times after TTL, want 2", len(fake.calls))
	}

	// 换了对端指纹（对端重启）立即回发，不受旧条目影响
	msg2 := announcement(true)
	msg2.Fingerprint = "another-fp"
	reply(t, dedup, "192.168.1.10", msg2, fake.post)
	if len(fake.calls) != 3 {
		t.Fatalf("poster called %d times after peer fingerprint change, want 3", len(fake.calls))
	}
}
