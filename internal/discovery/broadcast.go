package discovery

import (
	"time"

	"github.com/meowrain/localsend-go/internal/utils/logger"

	"github.com/meowrain/localsend-go/internal/models"
)

const (
	multicastIP   = "224.0.0.167"
	broadcastPort = 53317
	httpTimeout   = 2 * time.Second
	scanInterval  = 2 * time.Second
	// deviceTTL 是设备的生存时间：超过这么久没再收到公告/探测响应，就从设备表里移除。
	// 公告间隔 2s，200s 相当于容忍连续 100 次公告丢失（漫游、休眠、多播被丢）。
	deviceTTL = 200 * time.Second
)

func ListenAndStartBroadcasts(updates chan<- []models.SendModel) {
	logger.Info("Listening for broadcasts...")
	go ListenForUDPBroadcasts(updates)
	go ListenForHttpBroadCast(updates)
	// 清理器是必需的：没有它设备表只增不减，离线设备会一直留在列表里
	// 被当成可发送目标（deviceTTL 曾经定义了却从未被任何代码使用）。
	StartDeviceJanitor(updates)
	logger.Info("Start broadcasts...")
	go StartUDPBroadcast()
}
