package handlers

import (
	"localsend_cli/internal/utils/logger"
	"net/http"
	"sync"
)

var (
	cancelHandlers = make(map[string]func())
	handlersLock   sync.RWMutex
)

// RegisterCancelHandler 注册取消处理函数
func RegisterCancelHandler(sessionID string, cancelFunc func()) {
	handlersLock.Lock()
	defer handlersLock.Unlock()
	cancelHandlers[sessionID] = cancelFunc
}

// UnregisterCancelHandler 注销取消处理函数
func UnregisterCancelHandler(sessionID string) {
	handlersLock.Lock()
	defer handlersLock.Unlock()
	delete(cancelHandlers, sessionID)
}

// HandleCancel 处理取消请求
func HandleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	logger.Debugf("Received cancel request for session: %s", sessionID)
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	handlersLock.RLock()
	cancelFunc, exists := cancelHandlers[sessionID]
	handlersLock.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	cancelFunc()
	w.WriteHeader(http.StatusOK)
}
