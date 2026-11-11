package discovery

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/meowrain/localsend-go/internal/models"
)

// registerPath 是协议 3.1 / 3.2 的双向发现路由。
const registerPath = "/api/localsend/v2/register"

// probeSchemes 返回 HTTP 探测时依次尝试的协议。
// 先按本机使用的协议（本项目默认 http）试，失败再退回 https：之前写死
// https://<ip>:53317，明文 http 的对端（另一个 localsend-go）在多播不可用的
// 网络里完全发现不到。
func probeSchemes(local string) []string {
	local = strings.ToLower(strings.TrimSpace(local))
	if local == "" || local == "https" {
		return []string{"https"}
	}
	return []string{local, "https"}
}

// newProbeClient 构造探测用的 http.Client。对端多为自签证书，因此跳过校验。
func newProbeClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// probeRegister 向 ip:broadcastPort 依次尝试 probeSchemes，取第一个能连上的协议。
func probeRegister(ctx context.Context, client *http.Client, ip string, payload []byte, localProtocol string) (models.BroadcastMessage, error) {
	return probeRegisterPort(ctx, client, ip, broadcastPort, payload, localProtocol)
}

// probeRegisterPort 同 probeRegister，但探测端口可指定（单测里用）。
//
// 返回解析后的设备信息，其中 Protocol / Port 一定被回填成「本次实际成功的
// scheme 与实际访问的端口」。这一步不能省：官方 v2 的 register 响应体里根本没有
// protocol 字段（协议 3.2 的 response 示例也没有 port），不回填的话发送端
// 会拿空 Protocol 去猜，最终回退成 https，对明文 http 的对端直接发不出去。
func probeRegisterPort(ctx context.Context, client *http.Client, ip string, port int, payload []byte, localProtocol string) (models.BroadcastMessage, error) {
	var lastErr error
	for _, scheme := range probeSchemes(localProtocol) {
		msg, err := probeRegisterOnce(ctx, client, scheme, ip, port, payload)
		if err == nil {
			return msg, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no scheme to probe for %s", ip)
	}
	return models.BroadcastMessage{}, lastErr
}

// probeRegisterOnce 用固定 scheme/端口发一次 POST /api/localsend/v2/register。
func probeRegisterOnce(ctx context.Context, client *http.Client, scheme, ip string, port int, payload []byte) (models.BroadcastMessage, error) {
	url := fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(ip, strconv.Itoa(port)), registerPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return models.BroadcastMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return models.BroadcastMessage{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return models.BroadcastMessage{}, fmt.Errorf("read register response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return models.BroadcastMessage{}, fmt.Errorf("register returned status %d", resp.StatusCode)
	}

	// register 的响应字段是 BroadcastMessage 的子集，直接反序列化即可。
	var msg models.BroadcastMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return models.BroadcastMessage{}, fmt.Errorf("parse register response: %w", err)
	}

	// 回填：响应里没有（或不可信）的字段用本次探测的实际情况补上。
	msg.Protocol = scheme
	if msg.Port <= 0 || msg.Port > 65535 {
		msg.Port = port
	}
	msg.DeviceType = normalizeDeviceType(msg.DeviceType)
	msg.LastSeen = time.Now()

	return msg, nil
}
