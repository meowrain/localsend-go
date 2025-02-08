package discovery

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"localsend_cli/internal/discovery/shared"
	"localsend_cli/internal/models"
	"localsend_cli/internal/utils/logger"
)

func ListenAndStartBroadcasts(updates chan<- []models.SendModel) {
	logger.Info("Listening for broadcasts...")
	go ListenForUDPBroadcasts(updates)
	go ListenForHttpBroadCast(updates)
	logger.Info("Start broadcasts...")
	go StartUDPBroadcast()
}
func ListenForHttpBroadCast(updates chan<- []models.SendModel) {
	ticker := time.NewTicker(5 * time.Second)
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
		var mu sync.Mutex
		for _, ip := range ips {
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()
				url := fmt.Sprintf("https://%s:53317/api/localsend/v2/register", ip)
				req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
				if err != nil {
					logger.Errorf("Failed to create HTTP request for %s: %v", ip, err)
					return
				}
				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{
					Timeout: 2 * time.Second,
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

				mu.Lock()
				shared.DiscoveredDevices[ip] = response
				mu.Unlock()
			}(ip)
		}

		wg.Wait()

		devices := make([]models.SendModel, 0, len(shared.DiscoveredDevices))
		for ip, device := range shared.DiscoveredDevices {
			devices = append(devices, models.SendModel{
				IP:         ip,
				DeviceName: device.Alias,
			})
		}

		select {
		case updates <- devices:
		default:

		}
	}
}

func ListenForUDPBroadcasts(updates chan<- []models.SendModel) {
	multicastAddr := &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.167"),
		Port: 53317,
	}

	conn, err := net.ListenMulticastUDP("udp", nil, multicastAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	conn.SetReadBuffer(1024)

	for {
		buf := make([]byte, 1024)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			panic(err)
		}

		var message models.BroadcastMessage
		err = json.Unmarshal(buf[:n], &message)
		if err != nil {
			logger.Errorf("Failed to unmarshal broadcast message: %v", err)
			continue
		}

		shared.DiscoveredDevices[remoteAddr.IP.String()] = message

		// Convert allDevices map to a slice of SendModel
		var devices []models.SendModel
		for ip, device := range shared.DiscoveredDevices {
			devices = append(devices, models.SendModel{
				IP:         ip,
				DeviceName: device.Alias,
			})
		}

		// Send the updated devices list to the channel
		updates <- devices
	}
}
