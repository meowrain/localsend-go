package tui

import (
	"errors"
	"fmt"
	"time"

	"github.com/meowrain/localsend-go/internal/models"

	bubbletea "github.com/charmbracelet/bubbletea"
)

// SelectDevice 使用 Bubble Tea 库显示可供选择的设备列表并等待用户选择
func SelectDevice(updates <-chan []models.SendModel) (string, error) {
	device, err := SelectDeviceInfo(updates)
	if err != nil {
		return "", err
	}
	return device.IP, nil
}

// SelectDeviceInfo 与 SelectDevice 行为一致，但返回完整的设备信息（含 Port/Protocol），
// 供发送链路按对端广播的协议与端口发起请求。
func SelectDeviceInfo(updates <-chan []models.SendModel) (models.SendModel, error) {
	// 创建一个带缓冲的内部 channel
	internalUpdates := make(chan []models.SendModel, 100)

	// 在后台持续从外部 channel 读取更新
	go func() {
		for devices := range updates {
			// 非阻塞方式发送到内部 channel
			select {
			case internalUpdates <- devices:
			default:
				// 如果 channel 满了，清空后重新发送
				select {
				case <-internalUpdates:
				default:
				}
				internalUpdates <- devices
			}
		}
	}()

	// 创建模型和 Bubble Tea 程序
	initModel := &model{
		devices:    []models.SendModel{},
		deviceMap:  make(map[string]models.SendModel),
		sortedKeys: make([]string, 0),
		cursor:     0,
		updates:    internalUpdates,
	}

	cmd := bubbletea.NewProgram(initModel)
	m, err := cmd.Run()
	if err != nil {
		return models.SendModel{}, err
	}

	if m, ok := m.(model); ok && len(m.devices) > 0 {
		return m.devices[m.cursor], nil
	}
	// 不能让调用方拿到零值设备：那样会拼出 https://:53317 这种地址，
	// 用户最终看到的是 "connection refused"，与「没有选中设备」毫无关系。
	return models.SendModel{}, errors.New("no device selected")
}

// model 结构体用于 Bubble Tea
type model struct {
	devices    []models.SendModel
	deviceMap  map[string]models.SendModel // 使用 IP 作为键来存储设备
	sortedKeys []string                    // 保持固定的显示顺序
	cursor     int
	updates    <-chan []models.SendModel
}

// mergeDeviceUpdates 用最新快照重建设备列表，同时保持已有设备的显示顺序。
//
// 不能只做「新增合并」：发现层的快照会删掉过期设备，只增不减会让下线设备
// 永远留在 TUI 里；同一 IP 的元数据（端口/协议/别名）也可能变化，必须允许覆盖。
// newDevices 中同一 IP 出现多次时以最后一条为准。
func mergeDeviceUpdates(current, newDevices []models.SendModel) []models.SendModel {
	latest := make(map[string]models.SendModel, len(newDevices))
	for _, device := range newDevices {
		latest[device.IP] = device
	}

	out := make([]models.SendModel, 0, len(latest))
	// 先按当前顺序保留仍在线的设备
	for _, device := range current {
		if updated, ok := latest[device.IP]; ok {
			out = append(out, updated)
			delete(latest, device.IP)
		}
	}
	// 再把新出现的设备按快照顺序追加
	for _, device := range newDevices {
		if updated, ok := latest[device.IP]; ok {
			out = append(out, updated)
			delete(latest, device.IP)
		}
	}
	return out
}

// sameDevices 判断两份设备列表是否完全一致（顺序敏感）。
// 用于跳过无变化的重复渲染。
func sameDevices(a, b []models.SendModel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TickMsg 用于定期触发更新
type TickMsg time.Time

// Init 实现 Bubble Tea 的 Init 方法
func (m model) Init() bubbletea.Cmd {
	return tick()
}

// tick 每秒钟触发一次
func tick() bubbletea.Cmd {
	return bubbletea.Tick(time.Second, func(t time.Time) bubbletea.Msg {
		return TickMsg(t)
	})
}

// Update 实现 Bubble Tea 的 Update 方法
func (m model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, bubbletea.Quit
		case "down", "j":
			if len(m.devices) > 0 {
				m.cursor = (m.cursor + 1) % len(m.devices) // 向下移动
			}
		case "up", "k":
			if len(m.devices) > 0 {
				m.cursor = (m.cursor - 1 + len(m.devices)) % len(m.devices) // 向上移动
			}
		case "enter":
			return m, bubbletea.Quit // 退出选择
		}
	case TickMsg:
		select {
		case newDevices := <-m.updates:
			// 整表替换而不是「只补新设备」：发现层会把离线设备从快照里删掉，
			// 只增不减会让 TUI 继续展示已下线的设备；而且对端改端口/协议/别名后，
			// 只补新增会把旧值一直留在列表里，选中后按旧端口发送。
			// 顺序保持稳定：已在列表中的 IP 沿用原位置，新设备追加到末尾。
			devices := mergeDeviceUpdates(m.devices, newDevices)
			if !sameDevices(m.devices, devices) {
				m.deviceMap = make(map[string]models.SendModel, len(devices))
				m.sortedKeys = make([]string, 0, len(devices))
				for _, device := range devices {
					m.deviceMap[device.IP] = device
					m.sortedKeys = append(m.sortedKeys, device.IP)
				}
				m.devices = devices

				// 确保光标不会超出设备列表范围
				if len(m.devices) == 0 {
					m.cursor = 0
				} else if m.cursor >= len(m.devices) {
					m.cursor = len(m.devices) - 1
				}
			}
		default:
		}
		return m, tick()
	}
	return m, nil
}

// View 实现 Bubble Tea 的 View 方法
func (m model) View() string {
	if len(m.devices) == 0 {
		return "Scanning Devices...\n\n Press Ctrl+C to exit"
	}

	s := "Found Devices:\n\n"
	for i, device := range m.devices {
		cursor := " " // 默认没有光标
		if m.cursor == i {
			cursor = ">" // 选中的光标
		}
		s += fmt.Sprintf("%s %s (%s)\n", cursor, device.DeviceName, device.IP)
	}
	s += "\nUse arrow keys to navigate and enter to select. Press Ctrl+C to exit."
	return s
}
