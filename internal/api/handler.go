package api

import (
	"encoding/json"
	"log"
	"net/http"
)

func CreateServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err := json.NewEncoder(w).Encode(map[string]string{"status": "server created"})
	if err != nil {
		log.Fatal(err)
	}
}

func ListServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err := json.NewEncoder(w).Encode([]string{})
	if err != nil {
		log.Fatal(err)
	}
}
