package shared

import (
	. "localsend_cli/internal/models"
	"sync"
)

// 全局设备记录哈希表和互斥锁
var DiscoveredDevices = make(map[string]BroadcastMessage)
var Mu sync.Mutex
