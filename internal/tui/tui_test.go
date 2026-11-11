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
