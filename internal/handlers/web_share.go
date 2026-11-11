package handlers

import (
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery"
	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/skip2/go-qrcode"
)

// Web 端分享与二维码：
//
//	GET /api/web/qr?url=        把给定 URL 渲染成 PNG 二维码
//	GET /api/web/share?lan=1    返回本机可分享的地址（含局域网 IP）
//
// QR 只在服务端渲染，页面里不引入第三方 JS 库；URL 从请求里取时做白名单，
// 只允许 http/https，防止把 javascript: 之类的内容编码进二维码。

// maxQRURLLen 限制二维码内容长度：超过 2 KiB 的 URL 既没人会扫，也容易撑爆内存。
const maxQRURLLen = 2048

// WebQRCode 返回 PNG 格式的二维码。
func WebQRCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw := r.URL.Query().Get("url")
	if raw == "" {
		writeWebError(w, http.StatusBadRequest, "缺少 url 参数")
		return
	}
	if len(raw) > maxQRURLLen {
		writeWebError(w, http.StatusBadRequest, "url 过长")
		return
	}
	if !isHTTPURL(raw) {
		writeWebError(w, http.StatusBadRequest, "只支持 http/https 链接")
		return
	}

	png, err := qrcode.Encode(raw, qrcode.Medium, 320)
	if err != nil {
		logger.Errorf("generate QR: %v", err)
		writeWebError(w, http.StatusInternalServerError, "生成二维码失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(png)
}

// isHTTPURL 做最简的 scheme 校验（不需要完整解析）。
func isHTTPURL(raw string) bool {
	lower := raw
	n := len(lower)
	if n > 7 && equalFoldASCII(lower[:7], "http://") {
		return true
	}
	if n > 8 && equalFoldASCII(lower[:8], "https://") {
		return true
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// webShareInfo 是分享信息响应。
type webShareInfo struct {
	// URLs 是本机对外可访问的地址（每个局域网 IP 一个）
	URLs []string `json:"urls"`
	// QR 是第一个地址的二维码（PNG base64），失败时为空
	QR string `json:"qr,omitempty"`
	// PINEnabled 提示对方需要 PIN
	PINEnabled bool `json:"pinEnabled"`
	// Device 是本机设备名
	Device string `json:"device"`
}

// WebShareInfo 返回本机的分享地址与二维码。
func WebShareInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	urls := localShareURLs(r)
	info := webShareInfo{
		URLs:       urls,
		PINEnabled: webPINEnabled(),
		Device:     shared.Message.Alias,
	}
	if len(urls) > 0 {
		if png, err := qrcode.Encode(urls[0], qrcode.Medium, 320); err == nil {
			info.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		} else {
			logger.Warnf("generate share QR: %v", err)
		}
	}
	writeWebJSON(w, http.StatusOK, info)
}

// localShareURLs 枚举本机的局域网地址，拼成 Web 页面地址。
//
// 结果缓存 30 秒：GetLocalIP 会枚举网卡，对每个页面加载都做一次没必要。
// 缓存只保存 IP 列表，端口与协议在请求时拼接（Host 可能因反向代理而不同）。
var (
	shareIPsMu   sync.Mutex
	shareIPs     []string
	shareIPsTime time.Time
)

const shareIPsTTL = 30 * time.Second

func localShareURLs(r *http.Request) []string {
	shareIPsMu.Lock()
	if time.Since(shareIPsTime) > shareIPsTTL || shareIPs == nil {
		ips, err := localIPs()
		if err != nil {
			logger.Warnf("enumerate local IPs: %v", err)
		} else {
			shareIPs = ips
		}
		shareIPsTime = time.Now()
	}
	ips := append([]string(nil), shareIPs...)
	shareIPsMu.Unlock()

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	port := ""
	if host := r.Host; host != "" {
		if i := lastIndexByte(host, ':'); i >= 0 {
			port = host[i:]
		}
	}
	urls := make([]string, 0, len(ips))
	for _, ip := range ips {
		urls = append(urls, scheme+"://"+ip+port+"/")
	}
	return urls
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// localIPs 返回本机的可分享 IPv4 地址（不含回环）。
func localIPs() ([]string, error) {
	ips, err := discovery.GetLocalIP()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil || v4.IsLoopback() {
			continue
		}
		out = append(out, v4.String())
	}
	return out, nil
}

// ensure net is used on all platforms where GetLocalIP returns net.IP
var _ net.IP
