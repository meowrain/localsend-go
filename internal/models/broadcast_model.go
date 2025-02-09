package models

import "time"

type BroadcastMessage struct {
	Alias       string    `json:"alias"`       // 设备名称
	Version     string    `json:"version"`     // 协议版本
	DeviceModel string    `json:"deviceModel"` // 设备型号
	DeviceType  string    `json:"deviceType"`  // 设备类型: mobile, desktop, web, headless, server
	Fingerprint string    `json:"fingerprint"` // 设备指纹
	Port        int       `json:"port"`        // HTTP(S)服务端口
	Protocol    string    `json:"protocol"`    // 使用的协议: http 或 https
	Download    bool      `json:"download"`    // 是否支持下载API
	Announce    bool      `json:"announce"`    // 是否广播自己的存在
	LastSeen    time.Time `json:"-"`           // 最后一次发现时间 (本地使用)
}
