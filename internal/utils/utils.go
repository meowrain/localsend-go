package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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
func CalculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
