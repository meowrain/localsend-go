package clipboard

import (
	"localsend_cli/internal/utils/logger"

	clipboard "github.com/atotto/clipboard"
)

func WriteToClipBoard(text string) {
	err := clipboard.WriteAll(text)
	if err != nil {
		logger.Errorf("Error copying to clipboard: %v\n", err)
	} else {
		logger.Success("Text copied to clipboard!")
	}
}
