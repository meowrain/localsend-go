package discovery

import (
	"encoding/json"
	"fmt"
	"localsend_cli/internal/discovery/shared"
	"localsend_cli/internal/models"
	"localsend_cli/internal/utils/logger"
	"net"
	"time"
)

func ListenForUDPBroadcasts(updates chan<- []models.SendModel) {
	multicastAddr := &net.UDPAddr{
		IP:   net.ParseIP(multicastIP),
		Port: broadcastPort,
	}

	conn, err := net.ListenMulticastUDP("udp", nil, multicastAddr)
	if err != nil {
		logger.Errorf("Failed to listen for UDP broadcasts: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadBuffer(4096)

	logger.Info("Started listening for UDP broadcasts on ", multicastAddr.String())

	for {
		buf := make([]byte, 4096)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logger.Errorf("Error reading UDP broadcast: %v", err)
			continue
		}

		logger.Debugf("Received UDP broadcast from %s, size: %d bytes", remoteAddr.String(), n)

		// 打印原始消息内容以便调试
		logger.Debugf("Raw message: %s", string(buf[:n]))

		var message models.BroadcastMessage
		if err = json.Unmarshal(buf[:n], &message); err != nil {
			logger.Errorf("Failed to unmarshal broadcast message from %s: %v", remoteAddr.IP.String(), err)
			continue
		}

		// 验证必要字段
		if message.Alias == "" || message.DeviceType == "" {
			logger.Errorf("Invalid broadcast message from %s: missing required fields", remoteAddr.IP.String())
			continue
		}

		message.LastSeen = time.Now()

		logger.Debugf("Parsed message from %s: %+v", remoteAddr.IP.String(), message)

		shared.DevicesMutex.Lock()
		shared.DiscoveredDevices[remoteAddr.IP.String()] = message

		devices := make([]models.SendModel, 0, len(shared.DiscoveredDevices))
		for ip, device := range shared.DiscoveredDevices {
			devices = append(devices, models.SendModel{
				IP:         ip,
				DeviceName: device.Alias,
			})
		}
		shared.DevicesMutex.Unlock()

		logger.Debugf("Updated devices list: %+v", devices)

		select {
		case updates <- devices:
			logger.Debug("Successfully sent device updates")
		default:
			logger.Debug("Updates channel is full, skipping update")
		}
	}
}

func StartUDPBroadcast() {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", multicastIP, broadcastPort))
	if err != nil {
		logger.Errorf("Failed to resolve UDP address: %v", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		logger.Errorf("Failed to dial UDP: %v", err)
		return
	}
	defer conn.Close()

	logger.Info("Started UDP broadcast")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	const maxFailCount = 3 // 最大失败次数
	failCount := 0         // 失败计数器

	refreshConnection := func() {
		conn.Close()
		conn, err = net.DialUDP("udp", nil, addr)
		if err != nil {
			logger.Errorf("Failed to refresh UDP connection: %v", err)
			return
		}
	}

	for range ticker.C {
		data, err := json.Marshal(shared.Message)
		if err != nil {
			logger.Errorf("Failed to marshal broadcast message: %v", err)
			failCount++
			if failCount >= maxFailCount {
				logger.Info("Refreshing UDP connection due to consecutive failures")
				refreshConnection()
				failCount = 0 // 重置失败计数器
			}
			continue
		}

		_, err = conn.Write(data)
		if err != nil {
			logger.Errorf("Failed to send UDP broadcast: %v", err)
			failCount++
			if failCount >= maxFailCount {
				logger.Info("Refreshing UDP connection due to consecutive failures")
				refreshConnection()
				failCount = 0 // 重置失败计数器
			}
			continue
		}

		logger.Debug("Sent UDP broadcast")
		failCount = 0 // 成功发送后重置失败计数器
	}
}
