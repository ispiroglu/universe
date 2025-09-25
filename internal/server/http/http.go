package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"universe/internal/store"
)

type HttpServer interface {
	Start() error
	Stop()

	Set(w http.ResponseWriter, r *http.Request)
	Get(w http.ResponseWriter, r *http.Request)
	Delete(w http.ResponseWriter, r *http.Request)
}

type httpServer struct {
	store  *store.Store
	router *http.ServeMux
}

func NewServer(store *store.Store) HttpServer {
	router := http.NewServeMux()
	s := &httpServer{
		store:  store,
		router: router,
	}

	router.HandleFunc("/set/{key}", s.Set)
	router.HandleFunc("/get/{key}", s.Get)
	router.HandleFunc("/delete/{key}", s.Delete)

	return s
}

func (s *httpServer) Start() error {
	slog.Info("HTTP server starting on :8080")
	err := http.ListenAndServe(":8080", s.router)
	if err != nil {
		return err
	}

	return nil
}

func (s *httpServer) Stop() {
	slog.Info("HTTP server stopping on :8080")
	s.store.Close()
}

// @Summary Set key-value pair
// @Description Set a key-value pair in the store
// @Tags kv
// @Accept json
// @Produce json
// @Param key path string true "Key"
// @Param value body SetBody true "Value"
// @Success 200 {object} map[string]interface{}
// @Router /set/{key} [post]
func (s *httpServer) Set(w http.ResponseWriter, r *http.Request) {
	var body SetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	key := r.PathValue("key")
	x, err := json.Marshal(body.Value)
	if err != nil {
		http.Error(w, "invalid json internally", http.StatusBadRequest)
	}

	s.store.Set(key, x)

	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}

// @Summary Get value by key
// @Description Get the value for a given key
// @Tags kv
// @Produce json
// @Param key path string true "Key"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string "key not found"
// @Router /get/{key} [get]
func (s *httpServer) Get(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	value, ok := s.store.Get(key)
	if !ok {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "value": string(value)})
}

// @Summary Delete key-value pair
// @Description Delete a key-value pair from the store
// @Tags kv
// @Produce json
// @Param key path string true "Key"
// @Success 200 {object} map[string]interface{}
// @Router /delete/{key} [delete]
func (s *httpServer) Delete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, _ = s.store.Delete(key)

	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
