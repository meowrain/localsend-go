package tui

import (
	"fmt"
	"time"

	"github.com/meowrain/localsend-go/internal/models"

	bubbletea "github.com/charmbracelet/bubbletea"
)

// selectDevice 使用 Bubble Tea 库显示可供选择的设备列表并等待用户选择
func SelectDevice(updates <-chan []models.SendModel) (string, error) {
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
		return "", err
	}

	if m, ok := m.(model); ok && len(m.devices) > 0 {
		return m.devices[m.cursor].IP, nil
	}
	return "", nil
}

// model 结构体用于 Bubble Tea
type model struct {
	devices    []models.SendModel
	deviceMap  map[string]models.SendModel // 使用 IP 作为键来存储设备
	sortedKeys []string                    // 保持固定的显示顺序
	cursor     int
	updates    <-chan []models.SendModel
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
			if m.deviceMap == nil {
				m.deviceMap = make(map[string]models.SendModel)
			}

			// 更新设备映射
			changed := false
			for _, device := range newDevices {
				if _, exists := m.deviceMap[device.IP]; !exists {
					m.deviceMap[device.IP] = device
					m.sortedKeys = append(m.sortedKeys, device.IP)
					changed = true
				}
			}

			// 只有在有新设备时才更新设备列表
			if changed {
				m.devices = make([]models.SendModel, 0, len(m.deviceMap))
				for _, key := range m.sortedKeys {
					if device, ok := m.deviceMap[key]; ok {
						m.devices = append(m.devices, device)
					}
				}

				// 确保光标不会超出设备列表范围
				if m.cursor >= len(m.devices) {
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
