package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"shelf"
)

type Server struct {
	store *shelf.Store
}

func NewServer(store *shelf.Store) *Server {
	return &Server{store: store}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health" && r.Method == http.MethodGet:
		s.handleHealth(w)
	case r.URL.Path == "/size" && r.Method == http.MethodGet:
		s.handleSize(w)
	case r.URL.Path == "/keys" && r.Method == http.MethodGet:
		s.handleListKeys(w)
	case strings.HasPrefix(r.URL.Path, "/keys/"):
		s.handleKey(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSize(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]int{"size": s.store.Size()})
}

func (s *Server) handleListKeys(w http.ResponseWriter) {
	keys := s.store.Keys()
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/keys/")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, key)
	case http.MethodPut:
		s.handlePut(w, r, key)
	case http.MethodDelete:
		s.handleDelete(w, key)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGet(w http.ResponseWriter, key string) {
	value, found := s.store.Get(key)
	if !found {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"value": base64.StdEncoding.EncodeToString(value),
		"found": true,
	})
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	value, err := base64.StdEncoding.DecodeString(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "value must be base64 encoded")
		return
	}

	if err := s.store.Set(key, value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDelete(w http.ResponseWriter, key string) {
	deleted, err := s.store.Delete(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./shelf.db", "data directory")
	shards := flag.Int("shards", 16, "number of shards")
	snapshotThreshold := flag.Int("snapshot", 10000, "auto-snapshot threshold (0 to disable)")
	flag.Parse()

	store, err := shelf.Open(*dataDir, *shards, *snapshotThreshold)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}

	server := NewServer(store)
	handler := loggingMiddleware(server)

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("shelf listening on %s (data=%s, shards=%d, snapshot=%d)\n",
			*addr, *dataDir, *shards, *snapshotThreshold)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	if err := store.Close(); err != nil {
		log.Printf("store close error: %v", err)
	}

	fmt.Println("shelf stopped")
}
