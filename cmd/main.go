package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"localsend_cli/internal/config"
	"localsend_cli/internal/discovery"
	"localsend_cli/internal/handlers"
	"localsend_cli/internal/pkg/server"
	"localsend_cli/internal/utils/logger"
	"localsend_cli/static"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type textInputModel struct {
	value       string
	cursor      int
	placeholder string
	done        bool
}

func initialTextInputModel() textInputModel {
	return textInputModel{
		value:       "",
		cursor:      0,
		placeholder: "Enter file path...",
		done:        false,
	}
}

func (m textInputModel) Init() bubbletea.Cmd {
	return nil
}

func getPathSuggestions(input string) []string {
	if input == "" {
		input = "."
	}

	dir := input
	if !strings.HasSuffix(input, string(os.PathSeparator)) {
		dir = filepath.Dir(input)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil
	}

	prefix := filepath.Clean(input)
	var suggestions []string
	for _, file := range files {
		if strings.HasPrefix(filepath.Clean(file), prefix) {
			suggestions = append(suggestions, file)
		}
	}
	return suggestions
}

func (m textInputModel) Update(msg bubbletea.Msg) (textInputModel, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
		// 忽略鼠标事件
		return m, nil

	case bubbletea.KeyMsg:
		switch msg.String() {
		case "backspace":
			if m.cursor > 0 {
				m.value = m.value[:m.cursor-1] + m.value[m.cursor:]
				m.cursor--
			}
		case "left":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right":
			if m.cursor < len(m.value) {
				m.cursor++
			}
		case "tab":
			suggestions := getPathSuggestions(m.value)
			if len(suggestions) > 0 {
				m.value = suggestions[0]
				m.cursor = len(m.value)
			}
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(m.value)
		case "up", "down":
			// Ignore up and down key+s

		case "enter":
			m.done = true

		default:
			if msg.String() != "enter" && msg.String() != "home" && msg.String() != "end" {
				// 只允许输入有效的路径字符
				char := msg.String()
				// 检查是否是有效的路径字符
				if char == "." || char == "/" || char == "\\" || char == ":" || char == "-" || char == "_" ||
					(char >= "a" && char <= "z") || (char >= "A" && char <= "Z") || (char >= "0" && char <= "9") {
					m.value = m.value[:m.cursor] + char + m.value[m.cursor:]
					m.cursor++
				}
			}
		}
	}
	return m, nil
}

func (m textInputModel) View() string {
	if len(m.value) == 0 {
		return m.placeholder
	}
	value := m.value
	cursor := m.cursor
	if cursor > len(value) {
		cursor = len(value)
	}
	return value[:cursor] + "_" + value[cursor:]
}

func (m textInputModel) Value() string {
	return m.value
}

type model struct {
	mode        string
	choices     []string
	cursor      int
	filePrompt  bool
	textInput   textInputModel
	suggestions []string
}

func initialModel() model {
	return model{
		mode:      "",
		choices:   []string{"📤 Send", "📥 Receive", "❌ Exit"},
		cursor:    0,
		textInput: initialTextInputModel(),
	}
}

func (m model) Init() bubbletea.Cmd {
	return m.textInput.Init()
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7571F9")).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 2).
			MarginBottom(1)

	menuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			PaddingLeft(4)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7571F9")).
				PaddingLeft(2).
				SetString("❯ ")

	unselectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				PaddingLeft(4)

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7571F9")).
				PaddingLeft(2)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			PaddingLeft(1)
)

func (m model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
		if msg.Type == bubbletea.MouseLeft {
			if msg.Y > 3 && msg.Y <= len(m.choices)+3 {
				m.cursor = msg.Y - 4
				m.mode = m.choices[m.cursor]
				if m.mode == "📤 Send" {
					m.filePrompt = true
					return m, nil
				} else {
					return m, bubbletea.Quit
				}
			}
		}

	case bubbletea.KeyMsg:
		if m.filePrompt {
			if msg.String() == "ctrl+c" {
				return m, bubbletea.Quit
			}
			m.textInput, _ = m.textInput.Update(msg)
			if m.textInput.done {
				m.mode = "📤 Send"
				return m, bubbletea.Quit
			}
			m.suggestions = getPathSuggestions(m.textInput.value)
			switch msg.String() {
			case "tab":
				if len(m.suggestions) > 0 {
					if m.cursor >= len(m.suggestions)-1 {
						m.cursor = 0
					} else {
						m.cursor++
					}
					m.textInput.value = m.suggestions[m.cursor]
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				if m.textInput.done {
					m.mode = "📤 Send"
					return m, bubbletea.Quit
				}
				return m, nil
			} else {
				m.mode = m.choices[m.cursor]
				if m.mode == "📤 Send" {
					m.filePrompt = true
					return m, nil
				} else {
					return m, bubbletea.Quit
				}
			}
		case "backspace", "tab":
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				return m, nil
			}
		case "esc":
			if m.filePrompt {
				m.filePrompt = false
				m.textInput = initialTextInputModel()
			}
		default:
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				return m, nil
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var s strings.Builder

	// 标题
	s.WriteString(titleStyle.Render("💫 LocalSend CLI 💫"))
	s.WriteString("\n\n")

	// 菜单
	if m.mode == "" {
		for i, choice := range m.choices {
			if i == m.cursor {
				s.WriteString(selectedItemStyle.Render(choice))
			} else {
				s.WriteString(unselectedItemStyle.Render(choice))
			}
			s.WriteString("\n")
		}
	} else {
		// 显示当前模式
		s.WriteString(menuStyle.Render(m.mode))
		s.WriteString("\n\n")

		// 文件路径输入
		if m.filePrompt {
			s.WriteString(inputPromptStyle.Render("Enter file path: "))
			s.WriteString(inputStyle.Render(m.textInput.View()))
		}
	}

	return s.String()
}

var port int

func init() {
	flag.IntVar(&port, "port", 53317, "Port to listen on")
}

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("\n收到中断信号，正在退出...")
		os.Exit(0)
	}()
	logger.InitLogger()

	// Start HTTP server
	httpServer := server.New()
	if config.ConfigData.Functions.HttpFileServer {
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
		httpServer.HandleFunc("/send", handlers.NormalSendHandler) // Upload handler
	}
	/* Send and receive section */
	if config.ConfigData.Functions.LocalSendServer {
		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
		httpServer.HandleFunc("/api/localsend/v2/cancel", handlers.HandleCancel)
	}
	go func() {
		logger.Info("Server started at :" + fmt.Sprintf("%d", port))
		if err := http.ListenAndServe(":"+fmt.Sprintf("%d", port), httpServer); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// ./localsend <filepath>
	if len(os.Args) > 1 {
		err := handlers.SendFile(os.Args[1])
		if err != nil {
			logger.Errorf("Send failed: %v", err)
		}
		return
	}

	// Run Bubble Tea program
	p := bubbletea.NewProgram(initialModel(), bubbletea.WithoutSignalHandler())
	m, err := p.Run()
	if err != nil {
		log.Fatal(err)
	}

	mTyped := m.(model)
	mode := mTyped.mode

	if mode == "❌ Exit" {
		fmt.Println("Exiting program...")
		os.Exit(0)
	}

	if mode == "📤 Send" {
		filePath := mTyped.textInput.Value()
		if filePath == "" {
			fmt.Println("Send mode requires a file path")
			os.Exit(1)
		}

		logger.InitLogger()
		err := handlers.SendFile(filePath)
		if err != nil {
			logger.Errorf("Send failed: %v", err)
		}
	}

	if mode == "📥 Receive" {
		err = os.MkdirAll("uploads", 0o755)
		if err != nil {
			logger.Errorf("Failed to create uploads directory: %v", err)
			return
		}
		discovery.ListenAndStartBroadcasts(nil)
		logger.Info("Waiting to receive files...")
		ips, _ := discovery.GetLocalIP()

		for _, ip := range ips {
			ipStr := ip.String()
			if strings.HasPrefix(ipStr, "10.") || strings.HasPrefix(ipStr, "192.168.") || (strings.HasPrefix(ipStr, "172.") && len(ipStr) >= 7 && ipStr[4] >= '1' && ipStr[4] <= '6') {
				logger.Infof("If you opened the HTTP file server, you can view your files on %s", fmt.Sprintf("http://%v:%d", ip, port))
			}
		}

		select {}

	}
}
