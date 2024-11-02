package shared

import (
	"sync"

	"localsend_cli/internal/config"
	. "localsend_cli/internal/models"
	"localsend_cli/internal/utils"
)

// 全局设备记录哈希表和互斥锁,Message信息

var (
	DiscoveredDevices = make(map[string]BroadcastMessage)
	Mu                sync.Mutex
)

// https://github.com/localsend/protocol?tab=readme-ov-file#71-device-type
var Message BroadcastMessage = BroadcastMessage{
	Alias:       config.ConfigData.NameOfDevice,
	Version:     "2.0",
	DeviceModel: utils.CheckOSType(),
	DeviceType:  "headless", // 表示是没有gui的情况下运行
	Fingerprint: "random-string",
	Port:        53317,
	Protocol:    "http",
	Download:    true,
	Announce:    true,
}
