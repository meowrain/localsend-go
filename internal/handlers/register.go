package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// RegisterHandler 实现协议 3.1 / 3.2 的双向发现：POST /api/localsend/v2/register。
//
// 对端（官方 App、其他 localsend-go）在收到本机的多播公告、或做 HTTP 探测时都会请求这里，
// 期望拿到本机的设备信息作为响应。缺这条路由时请求会落到 404，本机永远进不了对方的设备列表。
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 协议 3.1 要求请求体是完整的设备信息；但历史版本 / 部分实现可能不发请求体，
	// 那种情况下仍然返回本机信息（相当于退化为 /info），只记一条日志，不拒绝请求。
	var info models.RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&info); err != nil {
		logger.Debugf("Register request from %s has no parsable body: %v", clientIP(r), err)
	}

	logger.Debugf("Received register request from %s (%s, fingerprint=%q)",
		clientIP(r), info.Alias, info.Fingerprint)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(registerResponse()); err != nil {
		logger.Errorf("Failed to write register response: %v", err)
	}
}

// registerResponse 用本机的广播信息拼出 register 响应体。
// 响应字段与请求同构（协议 3.1 / 3.2 的响应示例），额外带上 port / protocol，
// 让对方不依赖端口假设也能连上本机。
func registerResponse() models.RegisterResponse {
	msg := shared.Message
	return models.RegisterResponse{
		Alias:       msg.Alias,
		Version:     msg.Version,
		DeviceModel: msg.DeviceModel,
		DeviceType:  msg.DeviceType,
		Fingerprint: msg.Fingerprint,
		Port:        msg.Port,
		Protocol:    msg.Protocol,
		Download:    msg.Download,
	}
}
