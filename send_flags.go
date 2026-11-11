package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/meowrain/localsend-go/internal/utils/clipboard"
)

// sendOptions 描述 `send` 子命令的参数：三种发送来源彼此互斥，另有一个可选的对端 PIN。
type sendOptions struct {
	FilePath  string // 位置参数：send <file_path>
	Text      string
	TextSet   bool // --text 是否出现（允许显式传入空串，由校验统一报错）
	Clipboard bool
	// PeerPIN 是「对端接收端」要求的 PIN（`send --pin 123456`）。
	// 与全局的 --pin（本机接收端要求的 PIN）是两回事，互不影响。
	PeerPIN string
}

// SourceCount 返回被选中的发送来源个数。
func (o sendOptions) SourceCount() int {
	n := 0
	if o.FilePath != "" {
		n++
	}
	if o.TextSet {
		n++
	}
	if o.Clipboard {
		n++
	}
	return n
}

// validate 校验参数互斥与完整性，返回用户可直接阅读的错误。
func (o sendOptions) validate() error {
	if n := o.SourceCount(); n > 1 {
		return errors.New("send 只接受一种来源：<file_path>、--text、--clipboard 三者互斥")
	}
	if o.SourceCount() == 0 {
		return errors.New("send 需要指定来源：<file_path>、--text \"<内容>\" 或 --clipboard")
	}
	if o.TextSet && strings.TrimSpace(o.Text) == "" {
		return errors.New("--text 的内容不能为空")
	}
	if o.FilePath != "" && strings.TrimSpace(o.FilePath) == "" {
		return errors.New("file_path 不能为空")
	}
	return nil
}

// resolve 把参数解析成实际要发送的文本；文件来源返回空字符串。
// readClipboard 允许测试注入，生产环境用 clipboard.ReadFromClipboard。
func (o sendOptions) resolve(readClipboard func() (string, error)) (text string, isText bool, err error) {
	if err := o.validate(); err != nil {
		return "", false, err
	}
	if o.TextSet {
		return o.Text, true, nil
	}
	if o.Clipboard {
		content, err := readClipboard()
		if err != nil {
			return "", false, fmt.Errorf("读取剪贴板失败: %w", err)
		}
		if strings.TrimSpace(content) == "" {
			return "", false, errors.New("剪贴板内容为空，没有可发送的文本")
		}
		return content, true, nil
	}
	return "", false, nil
}

// parseSendOptions 解析 `send` 之后的参数（args 不含 "send" 本身）。
// 未知参数一律报错，避免把拼错的 flag 当成文件路径静默发送。
func parseSendOptions(args []string, usage io.Writer) (sendOptions, error) {
	var opts sendOptions
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(usage)
	fs.StringVar(&opts.Text, "text", "", "要发送的文本内容")
	fs.BoolVar(&opts.Clipboard, "clipboard", false, "发送本机剪贴板内容")
	fs.StringVar(&opts.PeerPIN, "pin", "", "对端接收端要求的 PIN（与全局 --pin 无关）")

	// 先统计每个 flag 出现的次数：flag 包对重复传入是「后者覆盖前者」，
	// 用户写 `--text a --text b` 时会静默只发 b，这里直接拒绝。
	counts := make(map[string]int)
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
		}
		if name == "text" || name == "clipboard" || name == "pin" {
			counts[name]++
		}
	}
	for name, n := range counts {
		if n > 1 {
			return opts, fmt.Errorf("--%s 重复出现 %d 次，只允许一次", name, n)
		}
	}

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	// 已解析的参数里是否出现过 --text（无论值是否为空）
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "text" {
			opts.TextSet = true
		}
	})
	// 空白 PIN 等同于没给：避免把 "  " 当 PIN 发给对端
	opts.PeerPIN = strings.TrimSpace(opts.PeerPIN)

	switch fs.NArg() {
	case 0:
	case 1:
		opts.FilePath = fs.Arg(0)
	default:
		return opts, fmt.Errorf("send 只接受一个文件路径参数，收到 %d 个", fs.NArg())
	}

	// 互斥与完整性在解析阶段就校验，调用方不必重复判断。
	if err := opts.validate(); err != nil {
		return opts, err
	}
	return opts, nil
}

// readClipboard 是 resolve 使用的默认剪贴板读取实现。
func readClipboard() (string, error) {
	return clipboard.ReadFromClipboard()
}
