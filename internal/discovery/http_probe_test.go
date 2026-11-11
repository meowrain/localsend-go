package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// splitServer 把 httptest 的 URL 拆成 IP 与端口。
func splitServer(t *testing.T, srv *httptest.Server) (string, int) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return u.Hostname(), port
}

// serverWithoutProtocol 模拟官方 v2 的 register 响应：没有 protocol、没有 port。
func serverWithoutProtocol(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(registerPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"alias":       "Nice Orange",
			"version":     "2.0",
			"deviceModel": "Samsung",
			"deviceType":  "mobile",
			"download":    false,
		})
	})
	return httptest.NewServer(mux)
}

// TestProbeRegisterFillsProtocolAndPort 是第三条审查意见的验收测试：
// 官方 v2 响应里没有 protocol / port，探测成功后必须把实际成功的 scheme
// 与端口回填进去，否则发送端会回退 https，对明文 http 的对端发不出去。
func TestProbeRegisterFillsProtocolAndPort(t *testing.T) {
	srv := serverWithoutProtocol(t)
	defer srv.Close()
	ip, port := splitServer(t, srv)

	msg, err := probeRegisterOnce(context.Background(), newProbeClient(), "http", ip, port, []byte(`{}`))
	if err != nil {
		t.Fatalf("probeRegisterOnce: %v", err)
	}
	if msg.Protocol != "http" {
		t.Errorf("Protocol = %q, want http (scheme actually used)", msg.Protocol)
	}
	if msg.Port != port {
		t.Errorf("Port = %d, want %d (port actually probed)", msg.Port, port)
	}
	if msg.Alias != "Nice Orange" {
		t.Errorf("Alias = %q, want Nice Orange", msg.Alias)
	}
	// deviceType 缺失/未知同样走协议 7.1 的兜底
	if msg.DeviceType != "mobile" {
		t.Errorf("DeviceType = %q, want mobile", msg.DeviceType)
	}
	if msg.LastSeen.IsZero() {
		t.Error("LastSeen should be set")
	}
}

// TestProbeRegisterKeepsAdvertisedPort 响应里自报了 port 时以响应为准，
// 只有缺失或非法时才回填探测端口。
func TestProbeRegisterKeepsAdvertisedPort(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(registerPath, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"alias": "x", "port": 53318, "protocol": "https"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ip, port := splitServer(t, srv)

	msg, err := probeRegisterOnce(context.Background(), newProbeClient(), "http", ip, port, []byte(`{}`))
	if err != nil {
		t.Fatalf("probeRegisterOnce: %v", err)
	}
	if msg.Port != 53318 {
		t.Errorf("Port = %d, want advertised 53318", msg.Port)
	}
	// protocol 必须反映本次实际用的 scheme，而不是响应里自称的值
	if msg.Protocol != "http" {
		t.Errorf("Protocol = %q, want http (actual scheme)", msg.Protocol)
	}
}

// TestProbeRegisterNon200IsError 非 200 不能当成发现成功。
func TestProbeRegisterNon200IsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(registerPath, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ip, port := splitServer(t, srv)

	if _, err := probeRegisterOnce(context.Background(), newProbeClient(), "http", ip, port, []byte(`{}`)); err == nil {
		t.Fatal("want error for 403, got nil")
	}
}

// TestProbeRegisterFallsBackToHTTPS 明文 http 探测失败后会退回 https。
// 用 httptest.NewTLSServer + InsecureSkipVerify，验证回填的是真正成功的 "https"。
func TestProbeRegisterFallsBackToHTTPS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(registerPath, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"alias": "TLS peer", "deviceType": "desktop"})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	ip, port := splitServer(t, srv)

	// 同一端口上只有 TLS：探测 http:// 会因握手失败而退化到 https://
	msg, err := probeRegisterPort(context.Background(), newProbeClient(), ip, port, []byte(`{}`), "http")
	if err != nil {
		t.Fatalf("probeRegister: %v", err)
	}
	if msg.Protocol != "https" {
		t.Errorf("Protocol = %q, want https", msg.Protocol)
	}
	if msg.Port != port {
		t.Errorf("Port = %d, want %d", msg.Port, port)
	}
}

func TestProbeSchemes(t *testing.T) {
	cases := map[string][]string{
		"":        {"https"},
		"https":   {"https"},
		"HTTP":    {"http", "https"},
		" http  ": {"http", "https"},
	}
	for in, want := range cases {
		got := probeSchemes(in)
		if len(got) != len(want) {
			t.Fatalf("probeSchemes(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("probeSchemes(%q) = %v, want %v", in, got, want)
			}
		}
	}
}
