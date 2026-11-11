package handlers

import (
	"testing"

	"github.com/meowrain/localsend-go/internal/models"
)

func TestResolveSendTarget(t *testing.T) {
	tests := []struct {
		name       string
		device     models.SendModel
		wantScheme string
		wantPort   int
	}{
		{
			name:       "broadcast http on custom port",
			device:     models.SendModel{IP: "192.168.1.2", Port: 8080, Protocol: "http"},
			wantScheme: "http",
			wantPort:   8080,
		},
		{
			name:       "broadcast https on default port",
			device:     models.SendModel{IP: "192.168.1.2", Port: 53317, Protocol: "https"},
			wantScheme: "https",
			wantPort:   53317,
		},
		{
			name:       "protocol is case insensitive and trimmed",
			device:     models.SendModel{IP: "192.168.1.2", Port: 1234, Protocol: " HTTP "},
			wantScheme: "http",
			wantPort:   1234,
		},
		{
			name:       "missing protocol falls back to https",
			device:     models.SendModel{IP: "192.168.1.2", Port: 8080},
			wantScheme: "https",
			wantPort:   8080,
		},
		{
			name:       "unknown protocol falls back to https",
			device:     models.SendModel{IP: "192.168.1.2", Port: 8080, Protocol: "ftp"},
			wantScheme: "https",
			wantPort:   8080,
		},
		{
			name:       "missing port falls back to 53317",
			device:     models.SendModel{IP: "192.168.1.2", Protocol: "http"},
			wantScheme: "http",
			wantPort:   53317,
		},
		{
			name:       "missing everything falls back to protocol defaults",
			device:     models.SendModel{IP: "192.168.1.2"},
			wantScheme: "https",
			wantPort:   53317,
		},
		{
			name:       "out of range port falls back to 53317",
			device:     models.SendModel{IP: "192.168.1.2", Port: 70000, Protocol: "http"},
			wantScheme: "http",
			wantPort:   53317,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, port := resolveSendTarget(tt.device)
			if scheme != tt.wantScheme || port != tt.wantPort {
				t.Fatalf("resolveSendTarget(%+v) = (%q, %d), want (%q, %d)",
					tt.device, scheme, port, tt.wantScheme, tt.wantPort)
			}
		})
	}
}

func TestBuildSendURL(t *testing.T) {
	tests := []struct {
		name   string
		device models.SendModel
		path   string
		want   string
	}{
		{
			name:   "http device",
			device: models.SendModel{IP: "192.168.1.2", Port: 53317, Protocol: "http"},
			path:   "/api/localsend/v2/prepare-upload",
			want:   "http://192.168.1.2:53317/api/localsend/v2/prepare-upload",
		},
		{
			name:   "https device on custom port",
			device: models.SendModel{IP: "10.0.0.5", Port: 8443, Protocol: "https"},
			path:   "/api/localsend/v2/upload",
			want:   "https://10.0.0.5:8443/api/localsend/v2/upload",
		},
		{
			name:   "missing info falls back to https and 53317",
			device: models.SendModel{IP: "10.0.0.6"},
			path:   "/api/localsend/v2/upload",
			want:   "https://10.0.0.6:53317/api/localsend/v2/upload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSendURL(tt.device, tt.path); got != tt.want {
				t.Fatalf("buildSendURL(%+v, %q) = %q, want %q", tt.device, tt.path, got, tt.want)
			}
		})
	}
}
