package discovery

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

		var wg sync.WaitGroup
		for _, ip := range ips {
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()
				url := fmt.Sprintf("https://%s:%d/api/localsend/v2/register", ip, broadcastPort)
				req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
				if err != nil {
					logger.Errorf("Failed to create HTTP request for %s: %v", ip, err)
					return
				}
				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{
					Timeout: httpTimeout,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}

				resp, err := client.Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					logger.Errorf("Failed to read HTTP response body from %s: %v", ip, err)
					return
				}

				var response models.BroadcastMessage
				if err := json.Unmarshal(body, &response); err != nil {
					logger.Errorf("Failed to parse HTTP response from %s: %v", ip, err)
					return
				}

				response.LastSeen = time.Now()

				shared.DevicesMutex.Lock()
				shared.DiscoveredDevices[ip] = response
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
