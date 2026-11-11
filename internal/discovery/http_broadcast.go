package discovery

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

func ListenForHttpBroadCast(updates chan<- []models.SendModel) {
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for range ticker.C {
		// HTTP 探测发的是 register 请求体，与 UDP 公告不是同一个载荷：
		// 协议 3.1 / 3.2 的 register 请求体里没有 announce 字段。
		data, err := json.Marshal(shared.RegisterRequest())
		if err != nil {
			logger.Errorf("Failed to marshal message: %v", err)
			continue
		}

		ips, err := pingScan()
		if err != nil {
			logger.Errorf("Failed to discover devices via ping scan: %v", err)
			continue
		}

		client := newProbeClient()
		var wg sync.WaitGroup
		for _, ip := range ips {
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()

				device, err := probeRegister(context.Background(), client, ip, data, shared.Message.Protocol)
				if err != nil {
					logger.Debugf("HTTP probe to %s failed: %v", ip, err)
					return
				}
				if device.Fingerprint != "" && device.Fingerprint == shared.Message.Fingerprint {
					logger.Debugf("Skip self discovered via HTTP on %s", ip)
					return
				}

				shared.DevicesMutex.Lock()
				shared.DiscoveredDevices[ip] = device
				shared.DevicesMutex.Unlock()
			}(ip)
		}
		wg.Wait()

		devices := snapshotDevices()

		select {
		case updates <- devices:
		default:
			logger.Debug("Updates channel is full, skipping update")
		}
	}
}
