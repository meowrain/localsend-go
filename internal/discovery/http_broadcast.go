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

				// 探测目标地址：先按本机使用的协议（本项目默认 http）试，失败再退回 https。
				// 之前写死 https://<ip>:53317，导致明文 http 的对端（例如另一个 localsend-go）
				// 在多播不可用的网络里完全发现不到。
				schemes := []string{shared.Message.Protocol, "https"}
				if schemes[0] == "" || schemes[0] == "https" {
					schemes = []string{"https"}
				}
				client := &http.Client{
					Timeout: httpTimeout,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
					},
				}

				var resp *http.Response
				var err error
				for _, scheme := range schemes {
					url := fmt.Sprintf("%s://%s:%d/api/localsend/v2/register", scheme, ip, broadcastPort)
					req, reqErr := http.NewRequest("POST", url, bytes.NewBuffer(data))
					if reqErr != nil {
						logger.Errorf("Failed to create HTTP request for %s: %v", ip, reqErr)
						return
					}
					req.Header.Set("Content-Type", "application/json")
					resp, err = client.Do(req)
					if err == nil {
						break
					}
				}
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
