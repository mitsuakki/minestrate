package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mitsuakki/minestrate/internal/server"
)

type Handler struct {
	orchestrator *server.Orchestrator
}

func NewHandler(o *server.Orchestrator) *Handler {
	return &Handler{orchestrator: o}
}

type CreateServerRequest struct {
	Game    string `json:"game"`
	Players int    `json:"players"`
}

func (h *Handler) CreateServer(w http.ResponseWriter, r *http.Request) {
	var req CreateServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Game) == "" {
		http.Error(w, "game is required", http.StatusBadRequest)
		return
	}

	if req.Players < 1 || req.Players > 100 {
		http.Error(w, "players must be between 1 and 100", http.StatusBadRequest)
		return
	}

	s, err := h.orchestrator.CreateServer(req.Game, req.Players)
	if err != nil {
		if err.Error() == "max servers reached" || err.Error() == "no ports available" {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(s)
}

func (h *Handler) ListServers(w http.ResponseWriter, r *http.Request) {
	servers := h.orchestrator.ListServers()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(servers)
}

func (h *Handler) GetServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := h.orchestrator.GetServer(id)
	if !ok {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s)
}

