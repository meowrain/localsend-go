package discovery

import (
	"time"

	"localsend_cli/internal/models"
	"localsend_cli/internal/utils/logger"
)

const (
	multicastIP   = "224.0.0.167"
	broadcastPort = 53317
	httpTimeout   = 2 * time.Second
	scanInterval  = 2 * time.Second
	deviceTTL     = 200 * time.Second // 设备的生存时间
)

func ListenAndStartBroadcasts(updates chan<- []models.SendModel) {
	logger.Info("Listening for broadcasts...")
	go ListenForUDPBroadcasts(updates)
	go ListenForHttpBroadCast(updates)
	logger.Info("Start broadcasts...")
	go StartUDPBroadcast()
}
