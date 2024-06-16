package discovery

import (
	"net"
	"testing"
)

func TestLocalIpGet(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Log(err)
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				if v.IP.To4() != nil && !v.IP.IsLoopback() {
					t.Log(v.IP)
				}
			}
		}
	}
	// t.Log(ifaces)
}
