package tui

import (
	"fmt"
	"time"

	"localsend_cli/internal/models"

	bubbletea "github.com/charmbracelet/bubbletea"
)

// selectDevice 使用 Bubble Tea 库显示可供选择的设备列表并等待用户选择
func SelectDevice(updates <-chan []models.SendModel) (string, error) {
	// 创建模型和 Bubble Tea 程序
	initModel := &model{
		devices: []models.SendModel{},
		cursor:  0,
		updates: updates,
	}
	cmd := bubbletea.NewProgram(initModel)
	// 运行 Bubble Tea 程序
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
	devices []models.SendModel
	cursor  int
	updates <-chan []models.SendModel
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
			m.devices = newDevices
		default:
		}
		return m, tick()
	}
	return m, nil
}

// View 实现 Bubble Tea 的 View 方法
func (m model) View() string {
	s := ""
	for i, device := range m.devices {
		cursor := " " // 默认没有光标
		if m.cursor == i {
			cursor = ">" // 选中的光标
		}
		s += fmt.Sprintf("%s %s (%s)\n", cursor, device.DeviceName, device.IP)
	}
	s += "\nUse arrow keys to navigate and enter to select."
	return s
}
