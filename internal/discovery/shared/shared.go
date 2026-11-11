package shared

import (
	"sync"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils"
)

// ProtocolVersion 是本机对外声明的 LocalSend 协议版本（major.minor）。
//
// 之所以是 2.2 而不是 3.x：官方 App 的 constants.dart 仍是 protocolVersion = '2.2'，
// http_provider.dart 全部使用 LsHttpClientVersion.v2，服务端 v3 的 register 还标着
// 「TODO: not wired up yet」，协议仓库 v3/ 只有两张 mermaid 图、没有文字规范。
// 在 v3 定稿前按图实现等于对着草稿写生产代码，因此这里只对齐 v2.2。
const ProtocolVersion = "2.2"

// 全局设备记录哈希表和互斥锁,Message信息

var (
	DiscoveredDevices = make(map[string]models.BroadcastMessage)
	DevicesMutex      sync.RWMutex // 只保留一个互斥锁
)

// https://github.com/localsend/protocol?tab=readme-ov-file#71-device-type
//
// Alias 不能在包初始化时取 config.ConfigData.NameOfDevice：配置要到 main 里
// config.Load() 之后才可用，而包级变量初始化早于它，那样会永远拿到空串。
// 因此这里先留空，由 SyncFromConfig（在 config.Load() 之后调用）填充。
var Message = models.BroadcastMessage{
	Version:     ProtocolVersion,
	DeviceModel: utils.CheckOSType(),
	DeviceType:  "headless",      // CLI工具使用headless类型
	Fingerprint: "random-string", // 应该生成一个唯一的指纹
	Port:        53317,
	Protocol:    "http",
	// Download 对应协议的 5.2 / 5.3（prepare-download / download）。
	// 本机没有实现下载 API，所以必须报 false：报 true 等于对外承诺一个会 404 的能力，
	// 对端会据此展示「下载」入口并在点击后失败。等 5.2 / 5.3 真正实现后再改成 true。
	Download: false,
	Announce: true,
}

// SyncFromConfig 把已加载的配置同步到广播消息里，必须在 config.Load() 之后调用。
func SyncFromConfig() {
	Message.Alias = config.ConfigData.NameOfDevice
}

// SetListenPort 同步对外广播的监听端口。
//
// 广播里的 Port 必须与本次实际监听的端口一致，否则接收端会按广播的
// 53317 去连一个跑在别的端口上的实例。
func SetListenPort(port int) {
	if port > 0 {
		Message.Port = port
	}
}
