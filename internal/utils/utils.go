package utils

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func CheckOSType() string {
	return runtime.GOOS
}
func WriteToClipBoard(text string) {
	os := CheckOSType()
	switch os {
	case "linux":
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		err := cmd.Run()
		if err != nil {
			fmt.Printf("Error copying to clipboard on Linux: %v\n", err)
		} else {
			fmt.Println("Text copied to clipboard on Linux!")
		}
	case "windows":
		cmd := exec.Command("cmd", "/c", "echo "+text+" | clip")
		err := cmd.Run()
		if err != nil {
			fmt.Printf("Error copying to clipboard on Windows: %v\n", err)
		} else {
			fmt.Println("Text copied to clipboard on Windows!")
		}
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		err := cmd.Run()
		if err != nil {
			fmt.Printf("Error copying to clipboard on macOS: %v\n", err)
		} else {
			fmt.Println("Text copied to clipboard on macOS!")
		}
	default:
		fmt.Printf("Unsupported OS: %v\n", os)
	}
}
