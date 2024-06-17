package handlers

import (
	"encoding/json"
	"fmt"
	"localsend_cli/internal/discovery/shared"
	"net/http"
)

func GetInfoHandler(w http.ResponseWriter, r *http.Request) {
	msg := shared.Messsage
	res, err := json.Marshal(msg)
	if err != nil {
		fmt.Println("json convert failed:", err)
		http.Error(w, "json convert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(res)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		fmt.Println("Error writing file:", err)
		return
	}
}
