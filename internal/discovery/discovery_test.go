package discovery

import (
	"fmt"
	probing "github.com/prometheus-community/pro-bing"
	"net"
	"sync"
	"testing"
	"time"
)

// TestDiscover 测试 pingScan 函数
func TestDiscover(t *testing.T) {
	ips, err := pingScan()
	if err != nil {
		t.Error(err)
	}

	if len(ips) == 0 {
		t.Error("No IP addresses discovered")
	} else {
		fmt.Println("Discovered IPs:", ips)
	}
}

// pingScan 使用 ICMP ping 扫描局域网内的所有活动设备
func pingScans() ([]string, error) {
	var ips []string
	ip, err := getLocalIP()
	if err != nil {
		return nil, err
	}

	ip = ip.Mask(net.IPv4Mask(255, 255, 255, 0)) // 假设是 24 子网掩码
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("invalid IPv4 address")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 1; i < 255; i++ {
		ip4[3] = byte(i)
		targetIP := ip4.String()

		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			pinger, err := probing.NewPinger(ip)
			if err != nil {
				fmt.Println("Failed to create pinger:", err)
				return
			}
			pinger.Count = 1
			pinger.Timeout = time.Second * 1
			//pinger.SetPrivileged(true)

			pinger.OnRecv = func(pkt *probing.Packet) {
				mu.Lock()
				ips = append(ips, ip)
				mu.Unlock()
				fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v\n",
					pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt)
			}
			err = pinger.Run()
			if err != nil {
				fmt.Println("Failed to run pinger:", err)
			}
		}(targetIP)
	}

	wg.Wait()
	return ips, nil
}
