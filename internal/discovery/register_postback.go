package discovery

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// RegisterPoster 向来源设备回发一次 register 请求。
// 抽成函数类型是为了让单测可以替换成只记录调用的假实现，不必真的发包。
type RegisterPoster func(peer registerPeer) error

// registerPeer 描述一次「回发 register」的目标与依据。
type registerPeer struct {
	IP          string // 对方公告来源 IP
	Port        int    // 对方公告里声明的 HTTP(S) 监听端口
	Protocol    string // 对方公告里声明的协议：http / https
	Message     models.BroadcastMessage
	Announce    bool // 对方公告是否要求回复（announce == true）
	Source      string
	Fingerprint string // 对方指纹，用于避免回复自己
}

// postbackRegister 是本包默认使用的回发实现。
var postbackRegister RegisterPoster = postRegister

// shouldPostRegister 判断是否需要为这条公告回发 register。
//
// 协议 3.1：只有 announce == true 的公告才需要回复；announce == false 的那份是
// 别人对本机的回复（fallback 形式），记录即可，再回就会无限对轰。
// 另外跳过空 IP / 自己的指纹，避免把包发给自己。
func shouldPostRegister(ip string, message models.BroadcastMessage) bool {
	if !message.Announce {
		return false
	}
	if net.ParseIP(ip) == nil {
		return false
	}
	if message.Fingerprint != "" && message.Fingerprint == shared.Message.Fingerprint {
		return false
	}
	return true
}

// buildRegisterPeer 把一条已解析的公告转换成回发目标。
// 端口 / 协议缺失时按协议默认值兜底（53317 / https），与发送链路保持一致。
func buildRegisterPeer(ip string, message models.BroadcastMessage) registerPeer {
	port := message.Port
	if port <= 0 || port > 65535 {
		port = broadcastPort
	}
	protocol := strings.ToLower(strings.TrimSpace(message.Protocol))
	if protocol != "http" && protocol != "https" {
		protocol = "https"
	}
	return registerPeer{
		IP:          ip,
		Port:        port,
		Protocol:    protocol,
		Message:     message,
		Announce:    message.Announce,
		Source:      net.JoinHostPort(ip, strconv.Itoa(port)),
		Fingerprint: message.Fingerprint,
	}
}

// postRegister 真正发出 POST /api/localsend/v2/register，请求体是本机的设备信息。
// 失败只返回 error，由调用方决定是否记日志——回发不应该影响发现主循环。
//
// 请求体必须用 shared.RegisterRequest 而不是 shared.Message：后者是 UDP 多播
// 公告的载荷，带着 announce: true，而协议 3.1 的 register 请求体没有这个字段。
func postRegister(peer registerPeer) error {
	payload, err := json.Marshal(shared.RegisterRequest())
	if err != nil {
		return fmt.Errorf("marshal register body: %w", err)
	}

	// 与 http_broadcast 一致：先按对方声明的协议试，失败再回退到另一种。
	schemes := []string{peer.Protocol}
	if peer.Protocol == "http" {
		schemes = append(schemes, "https")
	} else {
		schemes = append(schemes, "http")
	}

	client := &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	var lastErr error
	for _, scheme := range schemes {
		url := fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(peer.IP, strconv.Itoa(peer.Port)), registerPath)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		// 只要对端能接上并返回响应就算回发成功；响应体里的设备信息在
		// HTTP 探测链路上会被解析，UDP 链路本来就已经拿到了对方的公告。
		resp.Body.Close()
		return nil
	}
	return lastErr
}

// replyToAnnouncement 在收到 UDP 公告后决定是否回发 register。
// poster 为 nil 时使用默认实现；回发失败只记录日志，不影响调用方的发现主循环。
// 同一条公告（来源 IP + 指纹）在 postbackTTL 内只会回发一次。
func replyToAnnouncement(ip string, message models.BroadcastMessage, poster RegisterPoster) {
	replyToAnnouncementDedup(&lastPostback, ip, message, poster)
}

// replyToAnnouncementDedup 是 replyToAnnouncement 的实现，去重表可注入以便单测。
func replyToAnnouncementDedup(dedup *postbackDeduper, ip string, message models.BroadcastMessage, poster RegisterPoster) {
	if !shouldPostRegister(ip, message) {
		logger.Debugf("Skip register postback to %s (announce=%v)", ip, message.Announce)
		return
	}
	if dedup != nil && !dedup.allow(postbackDedupKey(ip, message.Fingerprint)) {
		logger.Debugf("Skip duplicate register postback to %s (fingerprint=%q)", ip, message.Fingerprint)
		return
	}
	if poster == nil {
		poster = postbackRegister
	}
	peer := buildRegisterPeer(ip, message)
	if err := poster(peer); err != nil {
		logger.Warnf("Failed to send register to %s: %v", peer.Source, err)
		return
	}
	logger.Debugf("Sent register to %s after receiving announcement", peer.Source)
}
