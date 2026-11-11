package handlers

import (
	"fmt"
	"strings"

	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

const (
	// defaultSendProtocol / defaultSendPort 是 LocalSend 协议的默认值，
	// 只在设备信息缺失时作为兜底（见 resolveSendTarget）。
	defaultSendProtocol = "https"
	defaultSendPort     = 53317
)

// resolveSendTarget 根据对端广播的设备信息推导发送请求要用的协议与端口。
// 设备信息里 protocol 为空时回退为 https（LocalSend 协议默认值），
// port <= 0 时回退为 53317；回退时打一条日志，方便排查对端信息不全的问题。
func resolveSendTarget(device models.SendModel) (scheme string, port int) {
	// 缺协议信息时回退 https：LocalSend 协议默认走 HTTPS。
	// 注意：如果对端其实是明文 http 而广播里又没带 protocol，这里仍会失败，
	// 这是协议本身的歧义，只能靠对端正确广播来消除。
	scheme = strings.ToLower(strings.TrimSpace(device.Protocol))
	if scheme != "http" && scheme != "https" {
		if scheme != "" {
			logger.Warnf("Unknown protocol %q from %s, falling back to %s", device.Protocol, device.IP, defaultSendProtocol)
		}
		scheme = defaultSendProtocol
	}

	port = device.Port
	if port <= 0 || port > 65535 {
		port = defaultSendPort
	}
	return scheme, port
}

// buildSendURL 拼接发送链路的 API 地址，例如
// http://192.168.1.2:53317/api/localsend/v2/prepare-upload
func buildSendURL(device models.SendModel, path string) string {
	scheme, port := resolveSendTarget(device)
	return fmt.Sprintf("%s://%s:%d%s", scheme, device.IP, port, path)
}
