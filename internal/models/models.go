package models

type BroadcastMessage struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel"`
	DeviceType  string `json:"deviceType"`
	Fingerprint string `json:"fingerprint"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Download    bool   `json:"download"`
	Announce    bool   `json:"announce"`
}

type DeviceInfo struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel"`
	DeviceType  string `json:"deviceType"`
	Fingerprint string `json:"fingerprint"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Download    bool   `json:"download"`
}
type FileInfo struct {
	ID       string `json:"id"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
	FileType string `json:"fileType"`
	SHA256   string `json:"sha256,omitempty"`
	Preview  string `json:"preview,omitempty"`
}
type PrepareReceiveRequest struct {
	Info struct {
		Alias       string `json:"alias"`
		Version     string `json:"version"`
		DeviceModel string `json:"deviceModel,omitempty"`
		DeviceType  string `json:"deviceType,omitempty"`
		Fingerprint string `json:"fingerprint"`
		Port        int    `json:"port"`
		Protocol    string `json:"protocol"`
		Download    bool   `json:"download"`
	} `json:"info"`
	Files map[string]FileInfo `json:"files"`
}

type PrepareReceiveResponse struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"` // File ID to Token map
}
