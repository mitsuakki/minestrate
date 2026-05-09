package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateServer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/servers", nil)
	w := httptest.NewRecorder()

	CreateServer(w, req)

	res := w.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(res.Body)

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, res.StatusCode)
	}

	contentType := res.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected content-type application/json, got %s", contentType)
	}

	var body map[string]string

	err := json.NewDecoder(res.Body).Decode(&body)
	if err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	expected := "server created"

	if body["status"] != expected {
		t.Fatalf("expected status %q, got %q", expected, body["status"])
	}
}

func TestListServers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	w := httptest.NewRecorder()

	ListServers(w, req)

	res := w.Result()
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(res.Body)

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %d", res.StatusCode)
	}

	contentType := res.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("Expected Content-Type application/json, got %s", contentType)
	}

	var body []string
	err := json.NewDecoder(res.Body).Decode(&body)
	if err != nil {
		log.Fatal(err)
	}
}
