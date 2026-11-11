package discovery

import (
	"testing"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
)

// seedDevices 替换全局设备表，并在用例结束后还原。
func seedDevices(t *testing.T, devices map[string]models.BroadcastMessage) {
	t.Helper()
	shared.DevicesMutex.Lock()
	old := shared.DiscoveredDevices
	shared.DiscoveredDevices = devices
	shared.DevicesMutex.Unlock()
	t.Cleanup(func() {
		shared.DevicesMutex.Lock()
		shared.DiscoveredDevices = old
		shared.DevicesMutex.Unlock()
	})
}

// TestPruneExpiredDevicesRemovesStale 是「deviceTTL 从未被使用」的验收测试：
// 超过 TTL 未再出现的设备必须从表里删掉，仍在续期的设备必须保留。
func TestPruneExpiredDevicesRemovesStale(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "fresh", LastSeen: now.Add(-2 * time.Second)},
		"192.168.1.11": {Alias: "stale", LastSeen: now.Add(-deviceTTL - time.Second)},
		"192.168.1.12": {Alias: "zero"}, // 零值 LastSeen 也按过期处理
	})

	removed := pruneExpiredDevices(now, deviceTTL)
	if len(removed) != 2 {
		t.Fatalf("removed = %v, want 2 entries", removed)
	}
	for _, ip := range []string{"192.168.1.11", "192.168.1.12"} {
		if _, ok := shared.DiscoveredDevices[ip]; ok {
			t.Errorf("%s should be pruned", ip)
		}
	}
	if _, ok := shared.DiscoveredDevices["192.168.1.10"]; !ok {
		t.Error("fresh device must be kept")
	}
}

// TestPruneKeepsDeviceExactlyAtTTL 边界：正好等于 TTL 时保留，避免续期抖动。
func TestPruneKeepsDeviceExactlyAtTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "edge", LastSeen: now.Add(-deviceTTL)},
	})

	if removed := pruneExpiredDevices(now, deviceTTL); len(removed) != 0 {
		t.Fatalf("removed = %v, want none at exact TTL", removed)
	}
}

// TestPruneRepeatedlyRefreshedDeviceNeverExpires TTL 之内反复刷新（公告）的设备不能过期。
func TestPruneRepeatedlyRefreshedDeviceNeverExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "alive", LastSeen: now},
	})

	for i := 0; i < 50; i++ {
		now = now.Add(2 * time.Second)
		shared.DevicesMutex.Lock()
		device := shared.DiscoveredDevices["192.168.1.10"]
		device.LastSeen = now
		shared.DiscoveredDevices["192.168.1.10"] = device
		shared.DevicesMutex.Unlock()

		if removed := pruneExpiredDevices(now, deviceTTL); len(removed) != 0 {
			t.Fatalf("iteration %d: device expired while still announcing", i)
		}
	}
}

// TestJanitorTickPushesUpdatedSnapshot 设备过期后要把新快照推给 updates。
func TestJanitorTickPushesUpdatedSnapshot(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "fresh", LastSeen: now, Port: 53317, Protocol: "http"},
		"192.168.1.11": {Alias: "gone", LastSeen: now.Add(-deviceTTL - time.Second)},
	})

	updates := make(chan []models.SendModel, 1)
	janitorTick(updates, deviceTTL, func() time.Time { return now })

	select {
	case devices := <-updates:
		if len(devices) != 1 || devices[0].IP != "192.168.1.10" {
			t.Fatalf("pushed devices = %+v, want only 192.168.1.10", devices)
		}
		if devices[0].DeviceName != "fresh" || devices[0].Port != 53317 || devices[0].Protocol != "http" {
			t.Errorf("snapshot lost fields: %+v", devices[0])
		}
	default:
		t.Fatal("expected an updates push after pruning")
	}
}

// TestJanitorTickSilentWhenNothingExpires 没有设备过期时不应反复推送。
func TestJanitorTickSilentWhenNothingExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "fresh", LastSeen: now},
	})

	updates := make(chan []models.SendModel, 1)
	janitorTick(updates, deviceTTL, func() time.Time { return now })

	select {
	case devices := <-updates:
		t.Fatalf("unexpected push: %+v", devices)
	default:
	}
}

// TestJanitorTickNilChannelDoesNotBlock 接收模式 / Web 模式传 nil 频道，
// 清理必须照常进行且不能卡死。
func TestJanitorTickNilChannelDoesNotBlock(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.11": {Alias: "gone", LastSeen: now.Add(-deviceTTL - time.Second)},
	})

	done := make(chan struct{})
	go func() {
		janitorTick(nil, deviceTTL, func() time.Time { return now })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("janitorTick blocked with nil updates channel")
	}

	if _, ok := shared.DiscoveredDevices["192.168.1.11"]; ok {
		t.Error("expired device should be pruned even with nil channel")
	}
}

// TestSnapshotDevicesCopiesAllFields 快照必须带上发送链路需要的 Port/Protocol。
func TestSnapshotDevicesCopiesAllFields(t *testing.T) {
	seedDevices(t, map[string]models.BroadcastMessage{
		"192.168.1.10": {Alias: "peer", Port: 53318, Protocol: "https"},
	})

	devices := snapshotDevices()
	if len(devices) != 1 {
		t.Fatalf("len(devices) = %d, want 1", len(devices))
	}
	got := devices[0]
	if got.IP != "192.168.1.10" || got.DeviceName != "peer" || got.Port != 53318 || got.Protocol != "https" {
		t.Errorf("snapshot = %+v", got)
	}
}
