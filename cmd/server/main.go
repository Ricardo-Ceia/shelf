package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	store         *shelf.Store
	registry      *shelf.Registry
	total         *shelf.Counter
	duration      *shelf.Histogram
	maxValueBytes int64
}

const defaultMaxValueBytes int64 = 1 << 20

func NewServer(store *shelf.Store, registry *shelf.Registry) *Server {
	return NewServerWithMaxValueBytes(store, registry, defaultMaxValueBytes)
}

func NewServerWithMaxValueBytes(store *shelf.Store, registry *shelf.Registry, maxValueBytes int64) *Server {
	if maxValueBytes <= 0 {
		maxValueBytes = defaultMaxValueBytes
	}
	return &Server{
		store:         store,
		registry:      registry,
		total:         registry.Counter("shelf_http_requests_total"),
		duration:      registry.Histogram("shelf_http_request_duration_seconds", []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0}),
		maxValueBytes: maxValueBytes,
	}
}

func (s *Server) Metrics() (*shelf.Counter, *shelf.Histogram) {
	return s.total, s.duration
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/metrics" && r.Method == http.MethodGet:
		s.handleMetrics(w)
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

func (s *Server) handleMetrics(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.registry.WriteTo(w, s.store)
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
	maxBodyBytes := maxBase64BodySize(s.maxValueBytes)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var req struct {
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if isBodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	value, err := base64.StdEncoding.DecodeString(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "value must be base64 encoded")
		return
	}
	if int64(len(value)) > s.maxValueBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "decoded value exceeds limit")
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

type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func metricsMiddleware(next http.Handler, total *shelf.Counter, duration *shelf.Histogram) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		elapsed := time.Since(start).Seconds()

		total.Inc()
		duration.Observe(elapsed)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

func maxBase64BodySize(maxValueBytes int64) int64 {
	encoded := ((maxValueBytes + 2) / 3) * 4
	return encoded + 1024
}

func isBodyTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return false
	}
	if strings.Contains(err.Error(), "request body too large") {
		return true
	}
	return false
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./shelf.db", "data directory")
	shards := flag.Int("shards", 16, "number of shards")
	snapshotThreshold := flag.Int("snapshot", 10000, "auto-snapshot threshold (0 to disable)")
	syncWrites := flag.Bool("sync-writes", false, "sync WAL on every write (stronger durability, lower throughput)")
	maxValueBytes := flag.Int64("max-value-bytes", defaultMaxValueBytes, "maximum decoded value size in bytes")
	readTimeout := flag.Duration("read-timeout", 5*time.Second, "maximum duration for reading request")
	writeTimeout := flag.Duration("write-timeout", 10*time.Second, "maximum duration for writing response")
	idleTimeout := flag.Duration("idle-timeout", 60*time.Second, "maximum keep-alive idle duration")
	flag.Parse()

	store, err := shelf.OpenWithOptions(*dataDir, *shards, *snapshotThreshold, shelf.StoreOptions{SyncOnWrite: *syncWrites})
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}

	registry := shelf.NewRegistry()
	server := NewServerWithMaxValueBytes(store, registry, *maxValueBytes)
	total, duration := server.Metrics()
	handler := metricsMiddleware(server, total, duration)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       *readTimeout,
		WriteTimeout:      *writeTimeout,
		IdleTimeout:       *idleTimeout,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("shelf listening on %s (data=%s, shards=%d, snapshot=%d, sync-writes=%t, max-value-bytes=%d)\n",
			*addr, *dataDir, *shards, *snapshotThreshold, *syncWrites, *maxValueBytes)
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
