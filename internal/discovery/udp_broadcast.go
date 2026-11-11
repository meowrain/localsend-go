package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
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

		// 只校验 alias：协议 3.1 的公告里 alias 是唯一的必填项，
		// deviceModel / deviceType 都标着 nullable（官方实现是 Option<DeviceType>）。
		// 之前把 deviceType 当必填，会把官方 App 发来的「无 deviceType」公告整个丢掉。
		if message.Alias == "" {
			logger.Errorf("Invalid broadcast message from %s: missing alias", remoteAddr.IP.String())
			continue
		}
		// 协议 7.1：未知/缺失的设备类型回退 desktop，与官方实现一致。
		message.DeviceType = normalizeDeviceType(message.DeviceType)

		message.LastSeen = time.Now()

		logger.Debugf("Parsed message from %s: %+v", remoteAddr.IP.String(), message)

		// 协议 3.1：收到 announce == true 的公告后要向来源回发 POST /v2/register，
		// 完成双向确认。回发失败只记日志，不影响下面的设备列表更新与主循环。
		// 放到 goroutine 里是因为 HTTP 回发可能阻塞数百毫秒，不能卡住收包循环。
		go replyToAnnouncement(remoteAddr.IP.String(), message, postbackRegister)

		shared.DevicesMutex.Lock()
		shared.DiscoveredDevices[remoteAddr.IP.String()] = message
		shared.DevicesMutex.Unlock()

		devices := snapshotDevices()

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
