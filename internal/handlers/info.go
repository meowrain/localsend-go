package handlers

import (
	"encoding/json"
	"net/http"

	"localsend_cli/internal/discovery/shared"
	"localsend_cli/internal/utils/logger"
)

func GetInfoHandler(w http.ResponseWriter, r *http.Request) {
	msg := shared.Message
	res, err := json.Marshal(msg)
	if err != nil {
		logger.Errorf("json convert failed:", err)
		http.Error(w, "json convert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(res)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		logger.Errorf("Error writing file:", err)
		return
	}
}
