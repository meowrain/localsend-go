package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meowrain/localsend-go/internal/models"
)

// requireTTY 在没有终端的环境（如 CI）下跳过需要 TTY 的交互测试。
// Bubble Tea 默认会打开 /dev/tty，无终端时直接返回错误。
func requireTTY(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("skip interactive TUI test: no TTY available: %v", err)
	}
	_ = f.Close()
}

// TestSelectDevice 测试 SelectDevice 函数
//
// SelectDevice 会拉起 bubbletea 程序，必须有 TTY 才能运行；CI 等无终端环境下跳过。
func TestSelectDevice(t *testing.T) {
	requireTTY(t)
	// 创建一个设备更新 channel
	updates := make(chan []models.SendModel)

	// 模拟设备更新
	go func() {
		time.Sleep(1 * time.Second)
		updates <- []models.SendModel{
			{IP: "192.168.1.1", DeviceName: "Device 1"},
			{IP: "192.168.1.2", DeviceName: "Device 2"},
		}
		time.Sleep(1 * time.Second)
		updates <- []models.SendModel{
			{IP: "192.168.1.1", DeviceName: "Device 1"},
			{IP: "192.168.1.2", DeviceName: "Device 2"},
			{IP: "192.168.1.3", DeviceName: "Device 3"},
		}
	}()

	// 调用 SelectDevice 函数（需要 TTY，无终端的环境下跳过）
	ip, err := SelectDevice(updates)
	if err != nil && strings.Contains(err.Error(), "/dev/tty") {
		t.Skipf("skip TUI test without a TTY: %v", err)
	}
	if err != nil {
		t.Fatalf("SelectDevice returned an error: %v", err)
	}

	// 检查返回的 IP 是否在模拟的设备列表中
	expectedIPs := map[string]bool{
		"192.168.1.1": true,
		"192.168.1.2": true,
		"192.168.1.3": true,
	}
	if !expectedIPs[ip] {
		t.Fatalf("SelectDevice returned an unexpected IP: %s", ip)
	}
}

// TestMergeDeviceUpdatesDropsOfflineDevices TUI 不能只增不减：
// 发现层的快照里没了某台设备，列表也必须随之移除。
func TestMergeDeviceUpdatesDropsOfflineDevices(t *testing.T) {
	current := []models.SendModel{
		{IP: "192.168.1.1", DeviceName: "A"},
		{IP: "192.168.1.2", DeviceName: "B"},
	}
	// 1.2 已下线，1.3 新上线
	next := mergeDeviceUpdates(current, []models.SendModel{
		{IP: "192.168.1.1", DeviceName: "A"},
		{IP: "192.168.1.3", DeviceName: "C"},
	})

	want := []string{"192.168.1.1", "192.168.1.3"}
	if len(next) != len(want) {
		t.Fatalf("devices = %+v, want %v", next, want)
	}
	for i, ip := range want {
		if next[i].IP != ip {
			t.Errorf("position %d = %s, want %s", i, next[i].IP, ip)
		}
	}
}

// TestMergeDeviceUpdatesRefreshesMetadata 同一 IP 的端口/协议/别名变化必须生效，
// 否则选中设备后会按旧端口发送。
func TestMergeDeviceUpdatesRefreshesMetadata(t *testing.T) {
	current := []models.SendModel{{IP: "192.168.1.1", DeviceName: "Old", Port: 53317, Protocol: "https"}}
	next := mergeDeviceUpdates(current, []models.SendModel{
		{IP: "192.168.1.1", DeviceName: "New", Port: 53318, Protocol: "http"},
	})

	if len(next) != 1 {
		t.Fatalf("devices = %+v, want 1", next)
	}
	if next[0].DeviceName != "New" || next[0].Port != 53318 || next[0].Protocol != "http" {
		t.Errorf("metadata not refreshed: %+v", next[0])
	}
}

// TestMergeDeviceUpdatesKeepsStableOrder 已在列表里的设备保持原位置，新设备追加到末尾。
func TestMergeDeviceUpdatesKeepsStableOrder(t *testing.T) {
	current := []models.SendModel{
		{IP: "192.168.1.1"},
		{IP: "192.168.1.2"},
		{IP: "192.168.1.3"},
	}
	next := mergeDeviceUpdates(current, []models.SendModel{
		{IP: "192.168.1.3"},
		{IP: "192.168.1.1"},
		{IP: "192.168.1.2"},
		{IP: "192.168.1.4"},
	})

	want := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4"}
	for i, ip := range want {
		if next[i].IP != ip {
			t.Fatalf("order = %+v, want %v", next, want)
		}
	}
}

// TestMergeDeviceUpdatesEmptySnapshot 所有设备下线时列表必须清空。
func TestMergeDeviceUpdatesEmptySnapshot(t *testing.T) {
	current := []models.SendModel{{IP: "192.168.1.1"}}
	if next := mergeDeviceUpdates(current, nil); len(next) != 0 {
		t.Fatalf("devices = %+v, want empty", next)
	}
}

// TestSameDevices 无变化时不应触发重排。
func TestSameDevices(t *testing.T) {
	a := []models.SendModel{{IP: "192.168.1.1", DeviceName: "A", Port: 53317, Protocol: "http"}}
	b := []models.SendModel{{IP: "192.168.1.1", DeviceName: "A", Port: 53317, Protocol: "http"}}
	if !sameDevices(a, b) {
		t.Error("identical lists reported as different")
	}
	b[0].Port = 53318
	if sameDevices(a, b) {
		t.Error("changed port reported as identical")
	}
}
