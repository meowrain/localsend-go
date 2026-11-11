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
		data, err := json.Marshal(shared.Message)
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

		shared.DevicesMutex.RLock()
		devices := make([]models.SendModel, 0, len(shared.DiscoveredDevices))
		for ip, device := range shared.DiscoveredDevices {
			devices = append(devices, models.SendModel{
				IP:         ip,
				DeviceName: device.Alias,
				Port:       device.Port,
				Protocol:   device.Protocol,
			})
		}
		shared.DevicesMutex.RUnlock()

		select {
		case updates <- devices:
		default:
			logger.Debug("Updates channel is full, skipping update")
		}
	}
}
