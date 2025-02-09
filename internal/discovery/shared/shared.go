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
	DevicesMutex      sync.RWMutex // 只保留一个互斥锁
)

// https://github.com/localsend/protocol?tab=readme-ov-file#71-device-type
var Message = BroadcastMessage{
	Alias:       config.ConfigData.NameOfDevice,
	Version:     "2.0",
	DeviceModel: utils.CheckOSType(),
	DeviceType:  "headless",      // CLI工具使用headless类型
	Fingerprint: "random-string", // 应该生成一个唯一的指纹
	Port:        53317,
	Protocol:    "http",
	Download:    true,
	Announce:    true,
}
