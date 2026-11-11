package discovery

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
)

// fakePoster 记录回发调用，替代默认的 HTTP 实现：单测里绝不真的发包。
type fakePoster struct {
	calls []registerPeer
	err   error
}

func (f *fakePoster) post(peer registerPeer) error {
	f.calls = append(f.calls, peer)
	return f.err
}

func announcement(announce bool) models.BroadcastMessage {
	return models.BroadcastMessage{
		Alias:       "Nice Orange",
		Version:     "2.2",
		DeviceModel: "Samsung",
		DeviceType:  "mobile",
		Fingerprint: "peer-fingerprint",
		Port:        53317,
		Protocol:    "http",
		Download:    false,
		Announce:    announce,
	}
}

// withLocalMessage 临时改本机广播信息（主要为了避开「指纹等于自己」的判断），
// 结束后恢复，避免影响同包其他测试。
func withLocalMessage(t *testing.T, msg models.BroadcastMessage) {
	t.Helper()
	old := shared.Message
	shared.Message = msg
	t.Cleanup(func() { shared.Message = old })
}

// freshDeduper 返回一个空的去重表，避免测试之间互相踩（同一 IP + 指纹只回发一次）。
func freshDeduper() *postbackDeduper {
	return &postbackDeduper{last: map[string]time.Time{}}
}

// withFixedNow 把去重表的时钟固定住，让 TTL 行为可断言。
func withFixedNow(d *postbackDeduper, now *time.Time) {
	d.now = func() time.Time { return *now }
	d.ttl = postbackTTL
}

// reply 是同包里触发一次回发的统一入口，统一注入新的去重表。
func reply(t *testing.T, dedup *postbackDeduper, ip string, msg models.BroadcastMessage, poster RegisterPoster) {
	t.Helper()
	if dedup == nil {
		dedup = freshDeduper()
	}
	replyToAnnouncementDedup(dedup, ip, msg, poster)
}

// TestReplyToAnnouncementPostsWhenAnnounceTrue 是验收标准里「announce == true 触发回发」。
func TestReplyToAnnouncementPostsWhenAnnounceTrue(t *testing.T) {
	fake := &fakePoster{}

	reply(t, nil, "192.168.1.10", announcement(true), fake.post)

	if len(fake.calls) != 1 {
		t.Fatalf("poster called %d times, want 1", len(fake.calls))
	}
	got := fake.calls[0]
	if got.IP != "192.168.1.10" {
		t.Errorf("IP = %q, want 192.168.1.10", got.IP)
	}
	if got.Port != 53317 {
		t.Errorf("Port = %d, want 53317", got.Port)
	}
	if got.Protocol != "http" {
		t.Errorf("Protocol = %q, want http", got.Protocol)
	}
	if got.Source != "192.168.1.10:53317" {
		t.Errorf("Source = %q, want 192.168.1.10:53317", got.Source)
	}
	if !got.Announce {
		t.Error("Announce should be true in the peer description")
	}
}

// TestReplyToAnnouncementSkipsWhenAnnounceFalse 对应「announce == false 只记录不回发」：
// 那份消息本身就是别人对本机的回复，再回会无限对轰。
func TestReplyToAnnouncementSkipsWhenAnnounceFalse(t *testing.T) {
	fake := &fakePoster{}

	reply(t, nil, "192.168.1.10", announcement(false), fake.post)

	if len(fake.calls) != 0 {
		t.Fatalf("poster called %d times, want 0", len(fake.calls))
	}
}

// TestReplyToAnnouncementSkipsSelf 指纹相同说明是自己发出去的公告（多播环回），不能回发。
func TestReplyToAnnouncementSkipsSelf(t *testing.T) {
	withLocalMessage(t, models.BroadcastMessage{Fingerprint: "self-fp"})

	msg := announcement(true)
	msg.Fingerprint = "self-fp"
	fake := &fakePoster{}

	reply(t, nil, "192.168.1.10", msg, fake.post)

	if len(fake.calls) != 0 {
		t.Fatalf("poster called %d times, want 0", len(fake.calls))
	}
}

// TestReplyToAnnouncementSkipsInvalidIP 来源地址不是合法 IP 时不发请求。
func TestReplyToAnnouncementSkipsInvalidIP(t *testing.T) {
	fake := &fakePoster{}

	reply(t, nil, "not-an-ip", announcement(true), fake.post)

	if len(fake.calls) != 0 {
		t.Fatalf("poster called %d times, want 0", len(fake.calls))
	}
}

// TestReplyToAnnouncementSwallowsErrors 回发失败只记日志，不能 panic 或向上传播，
// 否则会打断发现主循环。
func TestReplyToAnnouncementSwallowsErrors(t *testing.T) {
	fake := &fakePoster{err: errors.New("connection refused")}

	reply(t, nil, "192.168.1.10", announcement(true), fake.post)

	if len(fake.calls) != 1 {
		t.Fatalf("poster called %d times, want 1", len(fake.calls))
	}
}

// TestReplyToAnnouncementNilPosterUsesDefault 校验 nil 时会退回默认实现。
// 这里通过把默认实现换成假实现来观察，避免真的发包。
func TestReplyToAnnouncementNilPosterUsesDefault(t *testing.T) {
	fake := &fakePoster{}
	old := postbackRegister
	postbackRegister = fake.post
	t.Cleanup(func() { postbackRegister = old })

	reply(t, nil, "192.168.1.10", announcement(true), nil)

	if len(fake.calls) != 1 {
		t.Fatalf("poster called %d times, want 1", len(fake.calls))
	}
}

func TestBuildRegisterPeerDefaults(t *testing.T) {
	msg := models.BroadcastMessage{Alias: "x", Announce: true}

	peer := buildRegisterPeer("10.0.0.2", msg)
	if peer.Port != broadcastPort {
		t.Errorf("Port = %d, want %d", peer.Port, broadcastPort)
	}
	if peer.Protocol != "https" {
		t.Errorf("Protocol = %q, want https", peer.Protocol)
	}
	if peer.Source != "10.0.0.2:53317" {
		t.Errorf("Source = %q, want 10.0.0.2:53317", peer.Source)
	}
}

func TestBuildRegisterPeerKeepsAnnouncedValues(t *testing.T) {
	msg := models.BroadcastMessage{Port: 8080, Protocol: "HTTP", Announce: true}

	peer := buildRegisterPeer("10.0.0.2", msg)
	if peer.Port != 8080 {
		t.Errorf("Port = %d, want 8080", peer.Port)
	}
	if peer.Protocol != "http" {
		t.Errorf("Protocol = %q, want http (normalized)", peer.Protocol)
	}
}

func TestBuildRegisterPeerRejectsOutOfRangePort(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		peer := buildRegisterPeer("10.0.0.2", models.BroadcastMessage{Port: port, Protocol: "http"})
		if peer.Port != broadcastPort {
			t.Errorf("port %d -> %d, want fallback %d", port, peer.Port, broadcastPort)
		}
	}
}

func TestShouldPostRegister(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		msg  models.BroadcastMessage
		want bool
	}{
		{"announce true", "10.0.0.2", models.BroadcastMessage{Announce: true, Fingerprint: "p"}, true},
		{"announce false", "10.0.0.2", models.BroadcastMessage{Announce: false, Fingerprint: "p"}, false},
		{"bad ip", "oops", models.BroadcastMessage{Announce: true, Fingerprint: "p"}, false},
		{"empty ip", "", models.BroadcastMessage{Announce: true, Fingerprint: "p"}, false},
		{"ipv6", "fe80::1", models.BroadcastMessage{Announce: true, Fingerprint: "p"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPostRegister(tc.ip, tc.msg); got != tc.want {
				t.Fatalf("shouldPostRegister = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPostbackRegisterDefaultDoesNotPanicOnUnreachable 说明默认实现失败时是返回 error 而不是 panic。
// 用回环地址的未监听端口，请求会立刻被拒绝，不会真的打到局域网设备。
func TestPostbackRegisterDefaultDoesNotPanic(t *testing.T) {
	peer := registerPeer{IP: "127.0.0.1", Port: 1, Protocol: "http"}
	// 不断言错误内容：不同平台对 connection refused 的文案不一致。
	_ = postRegister(peer)
}

// TestPostRegisterBodyOmitsAnnounce 抓一次真实的 HTTP 回发，检查请求体：
// 必须带完整设备信息（含 version / port / protocol），且不带 announce。
// 协议 3.1 / 3.2 的 register 请求体没有 announce；早先为了「让请求体干净」
// 而把 shared.Message.Announce 改成 false，顺手把 UDP 公告也弄哑了——
// 这里锁住请求体侧的正确形态，公告侧由 shared.TestAnnouncementJSONSetsAnnounceTrue 锁住。
func TestPostRegisterBodyOmitsAnnounce(t *testing.T) {
	withLocalMessage(t, models.BroadcastMessage{
		Alias:       "npc-go",
		Version:     shared.ProtocolVersion,
		DeviceModel: "Linux",
		DeviceType:  "headless",
		Fingerprint: "fp-self",
		Port:        53317,
		Protocol:    "http",
		Download:    false,
		Announce:    true, // 公告必须是 true，回发请求体仍不该带它
	})

	var (
		mu     sync.Mutex
		bodies [][]byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != registerPath {
			t.Errorf("path = %q, want %q", r.URL.Path, registerPath)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"alias":"peer","version":"2.2","fingerprint":"fp-peer"}`))
	}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split server addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	if err := postRegister(registerPeer{IP: host, Port: port, Protocol: "http"}); err != nil {
		t.Fatalf("postRegister: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("server received %d requests, want 1", len(bodies))
	}

	var fields map[string]any
	if err := json.Unmarshal(bodies[0], &fields); err != nil {
		t.Fatalf("unmarshal register body %q: %v", bodies[0], err)
	}
	if _, ok := fields["announce"]; ok {
		t.Fatalf("register body must not carry announce: %s", bodies[0])
	}
	for _, key := range []string{"alias", "version", "deviceModel", "deviceType", "fingerprint", "port", "protocol", "download"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("register body missing %q: %s", key, bodies[0])
		}
	}
}
