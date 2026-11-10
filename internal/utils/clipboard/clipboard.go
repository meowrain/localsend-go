package clipboard

import (
	clipboard "github.com/atotto/clipboard"
	"github.com/meowrain/localsend-go/internal/utils/logger"
)

func WriteToClipBoard(text string) {
	err := clipboard.WriteAll(text)
	if err != nil {
		logger.Errorf("Error copying to clipboard: %v\n", err)
	} else {
		logger.Success("Text copied to clipboard!")
	}
}

// ReadFromClipboard 读取本机剪贴板内容。
// 无可用剪贴板时（如 headless 环境）返回 error，由调用方决定如何提示。
func ReadFromClipboard() (string, error) {
	return clipboard.ReadAll()
}
