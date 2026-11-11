package handlers

import (
	"net/http"
	"sync"

	"github.com/meowrain/localsend-go/internal/utils/logger"
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

	// 本机作为发送端时，cancel 用来中断本地那次上传
	handlersLock.RLock()
	cancelFunc, exists := cancelHandlers[sessionID]
	handlersLock.RUnlock()

	if exists {
		cancelFunc()
	}

	// 本机作为接收端时，cancel 用来释放会话占位（协议 4.1 的 409 依赖它）。
	// 两套状态互不相干，可能只有其中一个命中，任一命中都算处理成功。
	released := store.cancel(sessionID)
	if !exists && !released {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
