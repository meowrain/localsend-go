package models

// SendModel 描述一个可发送文件的候选设备。
// Port/Protocol 来自对端广播（UDP 多播或 HTTP 探测），发送请求据此拼接目标地址。
type SendModel struct {
	DeviceName string
	IP         string
	Port       int    // 对端 HTTP(S) 服务端口，0 表示未知
	Protocol   string // 对端协议，"http" 或 "https"，空表示未知
}
