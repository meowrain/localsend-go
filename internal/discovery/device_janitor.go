package discovery

import (
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

// 发现层只负责「把看到的设备记下来」，没有任何删除路径时，设备表只会单调增长：
// 对端关机、断网、换 IP 之后条目永远留在 shared.DiscoveredDevices 里，Web 端和
// TUI 会把幽灵设备当成可发送目标，用户点下去只能等到 60 秒 prepare 超时。
//
// deviceTTL 就是为这件事准备的：超过这个时间没有再次收到公告/探测响应，就认为
// 设备已离线。清理由独立 goroutine 定时执行，避免在收包热路径里做全表扫描。

// deviceJanitorInterval 是过期设备的扫描间隔。取 10s 是折中：
// 公告间隔 2s，正常在线的设备会被不断刷新 LastSeen，扫描再频繁也没有额外开销；
// 而设备真正下线后，最多 TTL+10s 就会从列表里消失。
const deviceJanitorInterval = 10 * time.Second

// StartDeviceJanitor 启动设备过期清理。updates 为 nil 时只清理内存中的设备表，
// 不推送更新（接收模式 / Web 模式直接读 shared.DiscoveredDevices，不需要频道）。
func StartDeviceJanitor(updates chan<- []models.SendModel) {
	go runDeviceJanitor(updates, deviceTTL, deviceJanitorInterval, time.Now)
}

// runDeviceJanitor 是清理循环，参数可注入以便单测。
//
// now 注入的是时钟而不是 ticker：单测里调用一次 janitorTick 就能覆盖判定逻辑，
// 不必真的等 10 秒。
func runDeviceJanitor(updates chan<- []models.SendModel, ttl, interval time.Duration, now func() time.Time) {
	if interval <= 0 {
		interval = deviceJanitorInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		janitorTick(updates, ttl, now)
	}
}

// janitorTick 执行一次清理，并在真的删掉设备时推送一份新的设备列表。
// 没有设备过期时静默返回，避免每秒都往 updates 里灌一份完全相同的快照。
func janitorTick(updates chan<- []models.SendModel, ttl time.Duration, now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	removed := pruneExpiredDevices(now(), ttl)
	if len(removed) == 0 {
		return
	}

	logger.Infof("Removed %d offline device(s) not seen for %s: %v", len(removed), ttl, removed)

	// updates 为 nil 时 select 会直接走 default，不会阻塞。
	select {
	case updates <- snapshotDevices():
	default:
		logger.Debug("Updates channel is full, skipping device expiry update")
	}
}

// pruneExpiredDevices 删除 LastSeen 超过 ttl 的设备，返回被删除的 IP。
//
// 判定用严格大于：正好等于 TTL 时保留，避免边界值让「刚续期又刚好卡点」的设备
// 抖动。LastSeen 为零值的条目一并删除——正常链路（UDP 公告、HTTP 探测）写入前
// 都会盖上时间戳，零值只可能来自「写了一半/忘了盖章」的异常记录，留着就是幽灵。
func pruneExpiredDevices(now time.Time, ttl time.Duration) []string {
	if ttl <= 0 {
		ttl = deviceTTL
	}

	shared.DevicesMutex.Lock()
	defer shared.DevicesMutex.Unlock()

	var removed []string
	for ip, device := range shared.DiscoveredDevices {
		if device.LastSeen.IsZero() || now.Sub(device.LastSeen) > ttl {
			delete(shared.DiscoveredDevices, ip)
			removed = append(removed, ip)
		}
	}
	return removed
}

// snapshotDevices 把设备表复制成发送端使用的列表。
// 调用方不要持有 DevicesMutex，否则会和这里重复加锁。
func snapshotDevices() []models.SendModel {
	shared.DevicesMutex.RLock()
	defer shared.DevicesMutex.RUnlock()

	devices := make([]models.SendModel, 0, len(shared.DiscoveredDevices))
	for ip, device := range shared.DiscoveredDevices {
		devices = append(devices, models.SendModel{
			IP:         ip,
			DeviceName: device.Alias,
			Port:       device.Port,
			Protocol:   device.Protocol,
		})
	}
	return devices
}
