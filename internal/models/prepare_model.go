package models

type PrepareReceiveRequest struct {
	Info  Info                `json:"info"`
	Files map[string]FileInfo `json:"files"`
}

type PrepareReceiveResponse struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"` // File ID to Token map
}
