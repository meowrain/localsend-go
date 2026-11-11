package models

// RegisterRequest 是 POST /api/localsend/v2/register 的请求体（协议 3.1 / 3.2）。
// 多播公告与 HTTP 探测发的是同一份设备信息，因此字段与 BroadcastMessage 基本一致，
// 只是不包含 announce（那是 UDP 公告专用字段）。
type RegisterRequest struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel,omitempty"`
	DeviceType  string `json:"deviceType,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Port        int    `json:"port,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Download    bool   `json:"download"`
}

// RegisterResponse 是 register 的响应体：本机的设备信息。
// 协议示例里没有 port / protocol（它们属于公告字段），但带上它们可以让对端
// 直接连到本机实际监听的端口，而不必假设 53317；官方实现对多余字段是容忍的。
type RegisterResponse struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel,omitempty"`
	DeviceType  string `json:"deviceType,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Port        int    `json:"port,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Download    bool   `json:"download"`
}
