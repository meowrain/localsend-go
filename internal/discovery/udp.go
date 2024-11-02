package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"localsend_cli/internal/discovery/shared"
	. "localsend_cli/internal/models"
)

// StartBroadcast 发送广播消息
func StartBroadcast() {
	// 设置多播地址和端口
	multicastAddr := &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.167"),
		Port: 53317,
	}

	data, err := json.Marshal(shared.Messsage)
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
		fmt.Println("Error creating UDP connection:", err)
		return
	}
	defer conn.Close()
	for {
		_, err := conn.WriteToUDP(data, multicastAddr)
		if err != nil {
			fmt.Println("Failed to send message:", err)
			panic(err)
		}
		// fmt.Println(num, "bytes write to multicastAddr")
		// log
		// fmt.Println("UDP Broadcast message sent!")
		time.Sleep(5 * time.Second) // 每5秒发送一次广播消息
	}
}

// ListenForBroadcasts 监听UDP广播消息
func ListenForBroadcasts() {
	fmt.Println("Listening for broadcasts...")

	// 设置多播地址和端口
	multicastAddr := &net.UDPAddr{
		IP:   net.ParseIP("224.0.0.167"),
		Port: 53317,
	}

	// 创建 UDP 多播监听连接
	conn, err := net.ListenMulticastUDP("udp", nil, multicastAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// 设置读取缓冲区大小
	conn.SetReadBuffer(1024)

	for {
		buf := make([]byte, 1024)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			panic(err)
		}
		var message BroadcastMessage
		err = json.Unmarshal(buf[:n], &message)
		if err != nil {
			fmt.Println("Failed to unmarshal broadcast message:", err)
			continue
		}

		shared.Mu.Lock()
		if _, exists := shared.DiscoveredDevices[remoteAddr.IP.String()]; !exists {
			shared.DiscoveredDevices[remoteAddr.IP.String()] = message
			fmt.Printf("Discovered device: %s (%s) at %s\n", message.Alias, message.DeviceModel, remoteAddr.IP.String())
		}
		shared.Mu.Unlock()
	}
}
