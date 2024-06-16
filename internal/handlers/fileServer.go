package handlers

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const uploadDir = "./uploads"

func GetFilesFromDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func FileServerHandler(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/uploads/")
	http.ServeFile(w, r, filepath.Join(uploadDir, file))
}

func IndexFileHandler(w http.ResponseWriter, r *http.Request) {
	dirPath := filepath.Join(uploadDir, strings.TrimPrefix(r.URL.Path, "/uploads/"))

	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		files, err := GetFilesFromDir(dirPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl := template.Must(template.ParseFiles("templates/cloudstorage.html"))
		data := struct {
			Path  string
			Files []os.DirEntry
		}{
			Path:  r.URL.Path,
			Files: files,
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	} else {
		http.ServeFile(w, r, dirPath)
	}
}
