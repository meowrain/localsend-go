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

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/discovery"
	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/handlers"
	"github.com/meowrain/localsend-go/internal/pkg/server"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/meowrain/localsend-go/static"
	qrcode "github.com/skip2/go-qrcode"
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
		choices:   []string{"📤 Send", "📥 Receive", "🌎 Web", "❌ Exit"},
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
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = len(m.choices) - 1
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

func WebServerMode(httpServer *http.ServeMux, port int) {
	err := os.MkdirAll("uploads", 0o755)
	if err != nil {
		logger.Errorf("Failed to create uploads directory: %v", err)
		return
	}
	if config.ConfigData.HttpFileServerEnabled() {
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
		httpServer.HandleFunc("/send", handlers.NormalSendHandler) // Upload handler
	}
	ips, _ := discovery.GetLocalIP()
	localIP := ""
	for _, ip := range ips {
		ipStr := ip.String()
		if strings.HasPrefix(ipStr, "10.") || strings.HasPrefix(ipStr, "192.168.") {
			logger.Infof("If you opened the HTTP file server, you can view your files on %s", fmt.Sprintf("http://%v:%d", ip, port))
		}
		if strings.HasPrefix(ipStr, "192.168.") {
			localIP = ip.String()
		}
	}
	qr, err := qrcode.New(fmt.Sprintf("http://%s:%d", localIP, port), qrcode.Highest)
	if err != nil {
		fmt.Println("生成二维码失败:", err)
		return
	}

	// 打印二维码到终端
	fmt.Println(qr.ToString(false))
	select {}
}

func ReceiveMode() {
	err := os.MkdirAll("uploads", 0o755)
	if err != nil {
		logger.Errorf("Failed to create uploads directory: %v", err)
		return
	}
	discovery.ListenAndStartBroadcasts(nil)
	logger.Info("Waiting to receive files...")
	select {}
}

func SendMode(filePath string) {
	err := handlers.SendFile(filePath)
	reportSendResult(err)
}

// reportSendResult 统一处理发送结果：204（无需传输）已经是成功路径，
// SendFile 内部按 ErrNothingToTransfer 提前返回了 nil，这里只需报错分支。
func reportSendResult(err error) {
	if err != nil {
		logger.Errorf("Send failed: %v", err)
	}
}

// SendTextMode 把文本封装成虚拟 .txt 文件发送，复用 SendFile 的 prepare/upload 流程。
func SendTextMode(text string) {
	reportSendResult(handlers.SendText(text))
}

// runSendCommand 统一处理 `send` 子命令的参数解析、校验与分发。
func runSendCommand(args []string) {
	opts, err := parseSendOptions(args, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			ExitMode()
		}
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		sendUsage()
		os.Exit(1)
	}

	// send --pin 才是「对端接收端的 PIN」，与全局 --pin（本机接收端的 PIN）分开存，
	// 两者互不覆盖：本机要求 123456 不代表对端也要求 123456。
	// 命令行优先；没给就看环境变量（避免把 PIN 写进 shell history）。
	config.ConfigData.SetSendPIN(opts.PeerPIN)
	if config.ConfigData.SendPIN() == "" {
		config.ConfigData.SetSendPIN(os.Getenv(config.EnvSendPIN))
	}

	text, isText, err := opts.resolve(readClipboard)
	if err != nil {
		fmt.Fprintf(os.Stderr, "send: %v\n", err)
		sendUsage()
		os.Exit(1)
	}

	if isText {
		SendTextMode(text)
		return
	}
	SendMode(opts.FilePath)
}

// sendUsage 打印 send 子命令的用法。
func sendUsage() {
	fmt.Fprintln(os.Stderr, "Usage: localsend-go send [--pin <peer_pin>] <file_path>")
	fmt.Fprintln(os.Stderr, "       localsend-go send [--pin <peer_pin>] --text \"<content>\"")
	fmt.Fprintln(os.Stderr, "       localsend-go send [--pin <peer_pin>] --clipboard")
	fmt.Fprintln(os.Stderr, "  --pin <peer_pin>  对端接收端要求的 PIN（可用 "+config.EnvSendPIN+" 环境变量提供）")
}

// showHelp 打印全局帮助信息。
func showHelp() {
	fmt.Println("Usage: <command> [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  web                 Start Web mode")
	fmt.Println("  send <file_path>    Send a file or directory")
	fmt.Println("  send --text <text>  Send a piece of text (as a virtual .txt file)")
	fmt.Println("  send --clipboard    Send local clipboard content as text")
	fmt.Println("  send --pin <pin>    PIN required by the *remote* receiver")
	fmt.Println("  receive             Start Receive mode")
	fmt.Println("  help                Display this help information")
	fmt.Println("Options:")
	fmt.Println("  --help              Display this help information")
	fmt.Println("  --version           Display version information")
	fmt.Println("  --port=<number>     Specify server port (default: 53317)")
	fmt.Println("  --config=<path>     Specify config file path (default: ./config.yaml)")
	fmt.Println("  --pin=<pin>         Require this PIN on *this* device's receiver (overrides functions.pin)")
	fmt.Println("")
	fmt.Println("Config lookup order: --config/LOCALSEND_CONFIG > ./config.yaml >")
	fmt.Println("  user config dir (e.g. ~/.config/localsend-go/config.yaml) >")
	fmt.Println("  legacy internal/config/config.yaml > embedded defaults")
	fmt.Println("Notes:")
	fmt.Println("  send 的 <file_path>、--text、--clipboard 三者互斥，同时给出会报错退出")
	fmt.Println("  接收端 PIN 与对端 PIN 是两件事，互不影响：")
	fmt.Println("    localsend-go --pin 123456 receive     # 本机要求 123456")
	fmt.Println("    localsend-go send --pin 654321 a.txt  # 对端要求 654321")
}

func ExitMode() {
	fmt.Println("Exiting program...")
	os.Exit(0)
}

func flagParse(httpServer *http.ServeMux, port int, flagOpen *bool) {
	// 只取第一个非 flag 参数作为模式，允许 `localsend-go --port 53318 web` 这样的写法
	args := flag.Args()
	if len(args) > 0 {
		*flagOpen = true
		mode := args[0]

		switch mode {
		case "web":
			WebServerMode(httpServer, port)
		case "send":
			// 用 flag.Args() 的子切片，兼容 `localsend-go --port 53318 send ...` 这种写法
			runSendCommand(args[1:])
		case "receive":
			ReceiveMode()
		case "help":
			showHelp()
			ExitMode()
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", mode)
			showHelp()
			os.Exit(1)
		}
	}
}

var (
	port       int
	configPath string
	pin        string
)

func init() {
	flag.IntVar(&port, "port", 53317, "Port to listen on")
	flag.StringVar(&configPath, "config", "", "Path to config file")
	// --pin 是「本机接收端」的配置，属于全局参数，必须写在子命令之前：
	// `localsend-go --pin 123456 receive`。
	// 发送时提供给对端的 PIN 由 `send --pin` 单独提供（见 send_flags.go），
	// 二者独立存储，互相不会覆盖。
	flag.StringVar(&pin, "pin", "", "PIN required by this device's receiver (overrides functions.pin in config)")
}

func main() {
	var flagOpen bool = false
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("\n收到中断信号，正在退出...")
		os.Exit(0)
	}()
	logger.InitLogger()

	// 先解析命令行参数：--config 与 --port 都必须早于配置加载与监听端口绑定。
	// 之前 flag.Parse() 放在 flagParse() 里，导致 --config 读到的是零值、
	// --port 也无法影响已经启动的监听，二者实际都不生效。
	flag.Usage = showHelp
	flag.Parse()
	// --version 在加载配置之前处理：它只依赖构建期注入的信息。
	if showVersion {
		printVersion()
		os.Exit(0)
	}
	for _, arg := range os.Args {
		if arg == "--help" || arg == "-h" {
			showHelp()
			ExitMode()
		}
	}

	// 显式指定的配置文件优先级最高，必须在读取 config.ConfigData 之前设置
	config.SetPath(configPath)
	// 提前加载配置，方便在启动日志里提示实际使用的配置文件
	config.Load()
	// 命令行 --pin 优先于配置文件里的 functions.pin
	if strings.TrimSpace(pin) != "" {
		config.ConfigData.SetPIN(pin)
		logger.Infof("Receiver PIN enabled via --pin")
	}
	// 配置就绪后再同步广播消息里的设备名与监听端口
	shared.SyncFromConfig()
	shared.SetListenPort(port)

	// Start HTTP server
	httpServer := server.New()

	/* Send and receive section */
	if config.ConfigData.LocalSendServerEnabled() {
		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		// 协议 3.1 / 3.2 的双向发现：多播公告与 HTTP 探测都会打这条路由，
		// 缺了它本机永远进不了对端的设备列表。
		httpServer.HandleFunc("/api/localsend/v2/register", handlers.RegisterHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
		httpServer.HandleFunc("/api/localsend/v2/cancel", handlers.HandleCancel)
	}
	go func() {
		logger.Info("Server started at :" + fmt.Sprintf("%d", port))
		if err := http.ListenAndServe(":"+fmt.Sprintf("%d", port), httpServer); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()
	// 参数解析
	flagParse(httpServer, port, &flagOpen)

	if !flagOpen {
		// Run Bubble Tea program
		p := bubbletea.NewProgram(initialModel(), bubbletea.WithoutSignalHandler())
		m, err := p.Run()
		if err != nil {
			log.Fatal(err)
		}

		mTyped := m.(model)
		mode := mTyped.mode

		if mode == "❌ Exit" {
			ExitMode()
		}

		if mode == "📤 Send" {
			filePath := mTyped.textInput.Value()
			if filePath == "" {
				fmt.Println("Send mode requires a file path")
				os.Exit(1)
			}
			SendMode(filePath)
		}

		if mode == "📥 Receive" {
			ReceiveMode()
		}
		if mode == "🌎 Web" {
			WebServerMode(httpServer, port)
		}
	}
}
