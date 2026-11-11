package shared

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// ProtocolVersion 是本机对外声明的 LocalSend 协议版本（major.minor）。
//
// 之所以是 2.2 而不是 3.x：官方 App 的 constants.dart 仍是 protocolVersion = '2.2'，
// http_provider.dart 全部使用 LsHttpClientVersion.v2，服务端 v3 的 register 还标着
// 「TODO: not wired up yet」，协议仓库 v3/ 只有两张 mermaid 图、没有文字规范。
// 在 v3 定稿前按图实现等于对着草稿写生产代码，因此这里只对齐 v2.2。
const ProtocolVersion = "2.2"

// DefaultDeviceType 是协议 7.1 规定的兜底设备类型。
// 该字段只影响对端 UI 图标，官方实现对未知/缺失值一律回退 desktop。
const DefaultDeviceType = "desktop"

// fingerprintBytes 是设备指纹的随机字节数。16 字节（128 bit）足够避免碰撞，
// 又能保持 hex 后 32 字符的可读长度。
const fingerprintBytes = 16

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
	DeviceType:  "headless", // CLI工具使用headless类型
	// 指纹必须每个进程唯一：协议 3.1 里它唯一的作用就是「避免发现自己」。
	// 曾经写死成 "random-string"，结果两台 localsend-go 互判为同一台设备，
	// 收到对端公告后直接跳过 register 回发，双向发现在真实双机场景下完全失效。
	Fingerprint: NewFingerprint(),
	Port:        53317,
	Protocol:    "http",
	// Download 对应协议的 5.2 / 5.3（prepare-download / download）。
	// 本机没有实现下载 API，所以必须报 false：报 true 等于对外承诺一个会 404 的能力，
	// 对端会据此展示「下载」入口并在点击后失败。等 5.2 / 5.3 真正实现后再改成 true。
	Download: false,
	// 公告里不带 announce：它只属于多播公告（协议 3.1），POST /v2/register 的
	// 请求体与响应体都没有这个字段。用 omitempty 避免把无效字段发给对端。
	// 注意：收到 UDP 公告后的回发由 udp_broadcast 单独处理，不依赖本字段。
	Announce: false,
}

// NewFingerprint 生成一个随机的设备指纹（32 位十六进制串）。
//
// 必须是随机的、且每个进程不同。这里不落盘持久化：重启后指纹改变只会让对端
// 把本机当作「新设备」重新发现，不会产生错误行为（对端只按指纹排除自己）。
// 用 crypto/rand 而不是 math/rand：指纹用于区分身份，不该是可预测的低熵值。
func NewFingerprint() string {
	buf := make([]byte, fingerprintBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 在受支持平台上不会失败；真失败时退化为空指纹，
		// 会让 Fingerprint=="" 的对端判断退化成「不排除」，比 panic 更可用。
		logger.Errorf("Failed to generate device fingerprint: %v", err)
		return ""
	}
	return hex.EncodeToString(buf)
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
