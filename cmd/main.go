package main

import (
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
				m.value = m.value[:m.cursor] + msg.String() + m.value[m.cursor:]
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m textInputModel) View() string {
	if len(m.value) == 0 {
		return m.placeholder
	}
	return m.value[:m.cursor] + "_" + m.value[m.cursor:]
}

func (m textInputModel) Value() string {
	return m.value
}

type model struct {
	mode       string
	choices    []string
	cursor     int
	filePrompt bool
	textInput  textInputModel
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
func (m model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
		// 忽略鼠标事件
		return m, nil

	case bubbletea.KeyMsg:
		if m.filePrompt {
			m.textInput, _ = m.textInput.Update(msg)
			if m.textInput.done {
				m.mode = "📤 Send"
				return m, bubbletea.Quit
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

	titleStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Align(lipgloss.Center).
		Foreground(lipgloss.Color("#FAFAFA"))

	s.WriteString(titleStyle.Render("💻 LocalSend CLI 💻"))
	s.WriteString("\n\n")

	choiceStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("205"))

	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("99"))

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = cursorStyle.Render(">")
		}
		s.WriteString(fmt.Sprintf("%s %s\n", cursor, choiceStyle.Render(choice)))
	}

	if m.filePrompt {
		s.WriteString("\n\nEnter file path: " + m.textInput.View())
	}

	return s.String()
}
func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM) // Function to handle clean shutdown.
	go func() {
		<-signalChan
		fmt.Println("\nReceived interrupt signal. Exiting...")
		os.Exit(0)
	}()
	logger.InitLogger()

	// Start HTTP server
	httpServer := server.New()
	if config.ConfigData.Functions.HttpFileServer {
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
		httpServer.HandleFunc("/send", handlers.NormalSendHandler)       // Upload handler
		httpServer.HandleFunc("/receive", handlers.NormalReceiveHandler) // Download handler
	}
	/* Send and receive section */
	if config.ConfigData.Functions.LocalSendServer {
		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
	}
	go func() {
		logger.Info("Server started at :53317")
		if err := http.ListenAndServe(":53317", httpServer); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Run Bubble Tea program
	p := bubbletea.NewProgram(initialModel())
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
		discovery.ListenAndStartBroadcasts(nil)
		logger.Info("Waiting to receive files...")
		ips, _ := discovery.GetLocalIP()
		local_ips := make([]string, 0)

		for _, ip := range ips {
			if strings.HasPrefix(ip.String(), "192.168") {
				local_ips = append(local_ips, ip.String())
			}
		}

		logger.Infof("If you opened the HTTP file server, you can view your files on %s", fmt.Sprintf("http://%v:53317", local_ips[0]))
		select {}

	}
}
