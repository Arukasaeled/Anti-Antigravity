package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/2ag/2ag/internal/supervisor"
)

func (s *Server) handleLocalExtensions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	catalog, err := supervisor.LocalUIExtensions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(catalog)
}

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	collection := strings.TrimPrefix(r.URL.Path, "/api/v1/workspace")
	collection = strings.TrimPrefix(collection, "/")
	if r.Method == http.MethodGet {
		var data any
		var err error
		if collection == "" {
			data, err = supervisor.LoadWorkspace()
		} else {
			data, err = supervisor.LoadWorkspaceCollection(collection)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(data)
		return
	}
	if r.Method != http.MethodPost || collection == "" {
		http.Error(w, "use GET to load workspace, POST to mutate a collection", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Action string         `json:"action"`
		ID     string         `json:"id"`
		Item   map[string]any `json:"item"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Draft history is maintained by the store, never accepted from clients.
	delete(request.Item, "versions")
	delete(request.Item, "version")
	items, item, err := supervisor.SaveWorkspaceItem(collection, request.Action, request.ID, request.Item)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"items": items, "item": item})
}
