package discovery

import (
	"encoding/json"
	"net"
	"time"

	"localsend_cli/internal/discovery/shared"
	"localsend_cli/internal/utils/logger"
)

// StartBroadcast 发送广播消息
func StartUDPBroadcast() {
	// 设置多播地址和端口
	multicastAddr := &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.167"),
		Port: 53317,
	}

	data, err := json.Marshal(shared.Message)
	if err != nil {
		panic(err)
	}
	// 创建本地地址，绑定到所有接口
	localAddr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: 0,
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		logger.Errorf("Error creating UDP connection:", err)
		return
	}
	defer conn.Close()
	for {
		_, err := conn.WriteToUDP(data, multicastAddr)
		if err != nil {
			logger.Errorf("Failed to send message:", err)
			panic(err)
		}

		time.Sleep(5 * time.Second) // 每5秒发送一次广播消息
	}
}
