package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mitsuakki/minestrate/config"
	"github.com/mitsuakki/minestrate/internal/server"
)

func setupTestHandler() *Handler {
	cfg := &config.Config{}
	cfg.Orchestrator.MaxServers = 10
	cfg.Orchestrator.Workers = 1
	cfg.Ports.RangeStart = 19132
	cfg.Ports.RangeEnd = 19142

	o := server.NewOrchestrator(cfg)
	return NewHandler(o)
}

func TestCreateServer(t *testing.T) {
	h := setupTestHandler()
	
	t.Run("ValidRequest", func(t *testing.T) {
		reqBody := CreateServerRequest{Game: "skywars", Players: 8}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		h.CreateServer(w, req)

		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("expected status %d, got %d", http.StatusAccepted, res.StatusCode)
		}

		var respBody server.Server
		if err := json.NewDecoder(res.Body).Decode(&respBody); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if respBody.Game != "skywars" || respBody.Players != 8 {
			t.Fatalf("unexpected response body: %+v", respBody)
		}
	})

	t.Run("InvalidGame", func(t *testing.T) {
		reqBody := CreateServerRequest{Game: "", Players: 8}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		h.CreateServer(w, req)

		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Result().StatusCode)
		}
	})

	t.Run("InvalidPlayers", func(t *testing.T) {
		reqBody := CreateServerRequest{Game: "skywars", Players: 0}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		h.CreateServer(w, req)

		if w.Result().StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Result().StatusCode)
		}
	})
}

func TestListServers(t *testing.T) {
	h := setupTestHandler()
	
	// Create one
	_, _ = h.orchestrator.CreateServer("skywars", 8)

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	w := httptest.NewRecorder()

	h.ListServers(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %d", res.StatusCode)
	}

	var body []*server.Server
	err := json.NewDecoder(res.Body).Decode(&body)
	if err != nil {
		t.Fatal(err)
	}

	if len(body) != 1 {
		t.Fatalf("expected 1 server, got %d", len(body))
	}
}

func TestIntegration_CreateAndPoll(t *testing.T) {
	h := setupTestHandler()
	h.orchestrator.StartWorkers()

	// 1. POST /servers
	reqBody := CreateServerRequest{Game: "skywars", Players: 8}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/servers", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	h.CreateServer(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}

	var s server.Server
	_ = json.NewDecoder(w.Body).Decode(&s)
	id := s.ID

	// 2. Poll GET /servers/{id}
	maxAttempts := 10
	for i := 0; i < maxAttempts; i++ {
		req := httptest.NewRequest(http.MethodGet, "/servers/"+id, nil)
		// We need to simulate chi URL param for direct handler call
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		
		w := httptest.NewRecorder()
		h.GetServer(w, req)

		var polled struct {
			State server.ServerState `json:"state"`
		}
		_ = json.NewDecoder(w.Body).Decode(&polled)

		if polled.State == server.StateRunning {
			return // Success
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("server never reached running state")
}
