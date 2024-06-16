package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const uploadDir = "uploads"

// SendHandler 处理文件上传请求
func SendHandler(w http.ResponseWriter, r *http.Request) {
	// 解析multipart/form-data
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, fmt.Sprintf("Could not parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	// 获取文件
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not get uploaded file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 创建上传目录（如果不存在）
	if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
		http.Error(w, fmt.Sprintf("Could not create upload directory: %v", err), http.StatusInternalServerError)
		return
	}

	// 创建目标文件
	filePath := filepath.Join(uploadDir, handler.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not create file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// 将上传的文件内容写入目标文件
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, fmt.Sprintf("Could not save file: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "File uploaded successfully: %s\n", handler.Filename)
}
