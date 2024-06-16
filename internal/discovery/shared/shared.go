package shared

import (
	"localsend_cli/internal/config"
	. "localsend_cli/internal/models"
	"localsend_cli/internal/utils"
	"sync"
)

// 全局设备记录哈希表和互斥锁,Message信息

var DiscoveredDevices = make(map[string]BroadcastMessage)
var Mu sync.Mutex
var Messsage BroadcastMessage = BroadcastMessage{
	Alias:       config.NameOfDevice,
	Version:     "2.0",
	DeviceModel: utils.CheckOSType(),
	DeviceType:  "desktop",
	Fingerprint: "random-string",
	Port:        53317,
	Protocol:    "http",
	Download:    true,
	Announce:    true,
}
