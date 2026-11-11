package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/meowrain/localsend-go/internal/utils/logger"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
)

// GetInfoHandler 实现协议 6.1 的 GET /api/localsend/v2/info。
//
// 响应体与 register 的请求/响应同构：协议 6.1 的示例里没有 announce 字段，
// 而 announce 只属于 UDP 多播公告（3.1）。之前直接 Marshal(shared.Message)
// 会把这个公告专用字段塞进 /info 响应，这里改用 shared.RegisterRequest()。
func GetInfoHandler(w http.ResponseWriter, r *http.Request) {
	res, err := json.Marshal(shared.RegisterRequest())
	if err != nil {
		logger.Errorf("json convert failed: %v", err)
		http.Error(w, "json convert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(res)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		logger.Errorf("Error writing file: %v", err)
		return
	}
}
