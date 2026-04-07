package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"shelf"
)

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	dir := t.TempDir()

	store, err := shelf.Open(dir, 16, 0)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	cleanup := func() { store.Close() }
	return NewServer(store), cleanup
}

func doRequest(srv *Server, method, path string, body any) (*httptest.ResponseRecorder, error) {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	r := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w, nil
}

func TestGetExistingKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	srv.store.Set("name", []byte("Alice"))

	w, err := doRequest(srv, http.MethodGet, "/keys/name", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("GET /keys/name status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["found"] != true {
		t.Fatalf("found = %v, want true", resp["found"])
	}

	encoded, ok := resp["value"].(string)
	if !ok {
		t.Fatal("value is not a string")
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	if string(decoded) != "Alice" {
		t.Fatalf("value = %q, want Alice", decoded)
	}
}

func TestGetMissingKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/keys/nonexistent", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /keys/nonexistent status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["error"] != "key not found" {
		t.Fatalf("error = %q, want 'key not found'", resp["error"])
	}
}

func TestPutNewKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	encoded := base64.StdEncoding.EncodeToString([]byte("Alice"))

	w, err := doRequest(srv, http.MethodPut, "/keys/name", map[string]string{"value": encoded})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("PUT /keys/name status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !resp["ok"] {
		t.Fatal("ok = false, want true")
	}

	// Verify it was actually stored
	v, found := srv.store.Get("name")
	if !found || string(v) != "Alice" {
		t.Fatalf("store Get(name) = %q, %v; want Alice, true", v, found)
	}
}

func TestPutExistingKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	srv.store.Set("name", []byte("old"))

	encoded := base64.StdEncoding.EncodeToString([]byte("new"))
	w, err := doRequest(srv, http.MethodPut, "/keys/name", map[string]string{"value": encoded})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("PUT /keys/name status = %d, want %d", w.Code, http.StatusOK)
	}

	v, found := srv.store.Get("name")
	if !found || string(v) != "new" {
		t.Fatalf("store Get(name) = %q, %v; want new, true", v, found)
	}

	if srv.store.Size() != 1 {
		t.Fatalf("Size = %d, want 1", srv.store.Size())
	}
}

func TestPutInvalidJSON(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	r := httptest.NewRequest(http.MethodPut, "/keys/name", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestPutInvalidBase64(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodPut, "/keys/name", map[string]string{"value": "!!!not-base64!!!"})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid base64 status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeleteExistingKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	srv.store.Set("name", []byte("Alice"))

	w, err := doRequest(srv, http.MethodDelete, "/keys/name", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /keys/name status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !resp["deleted"] {
		t.Fatal("deleted = false, want true")
	}

	if srv.store.Size() != 0 {
		t.Fatalf("Size after delete = %d, want 0", srv.store.Size())
	}
}

func TestDeleteMissingKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodDelete, "/keys/nonexistent", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /keys/nonexistent status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["deleted"] {
		t.Fatal("deleted = true, want false")
	}
}

func TestUnknownPath(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/unknown", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /unknown status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHealth(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Fatalf("status = %q, want ok", resp["status"])
	}
}

func TestHealthWrongMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodPost, "/health", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusNotFound {
		t.Fatalf("POST /health status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSize(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	srv.store.Set("a", []byte("1"))
	srv.store.Set("b", []byte("2"))

	w, err := doRequest(srv, http.MethodGet, "/size", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("GET /size status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["size"] != 2 {
		t.Fatalf("size = %d, want 2", resp["size"])
	}
}

func TestSizeEmpty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/size", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["size"] != 0 {
		t.Fatalf("size = %d, want 0", resp["size"])
	}
}

func TestListKeys(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	srv.store.Set("a", []byte("1"))
	srv.store.Set("b", []byte("2"))
	srv.store.Set("c", []byte("3"))

	w, err := doRequest(srv, http.MethodGet, "/keys", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("GET /keys status = %d, want %d", w.Code, http.StatusOK)
	}

	var keys []string
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(keys) != 3 {
		t.Fatalf("keys count = %d, want 3", len(keys))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !keySet[want] {
			t.Fatalf("keys missing %q", want)
		}
	}
}

func TestListKeysEmpty(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/keys", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var keys []string
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(keys) != 0 {
		t.Fatalf("keys count = %d, want 0", len(keys))
	}
}

func TestListKeysWrongMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodPost, "/keys", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusNotFound {
		t.Fatalf("POST /keys status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodPatch, "/keys/name", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH /keys/name status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestEmptyKey(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	w, err := doRequest(srv, http.MethodGet, "/keys/", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET /keys/ status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRoundtripPutAndGet(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	for _, tc := range []struct {
		key   string
		value string
	}{
		{"name", "Alice"},
		{"age", "30"},
		{"city", "Berlin"},
		{"empty", ""},
		{"binary", "\x00\x01\xFF\xFE\x80"},
	} {
		encoded := base64.StdEncoding.EncodeToString([]byte(tc.value))

		w, err := doRequest(srv, http.MethodPut, "/keys/"+tc.key, map[string]string{"value": encoded})
		if err != nil {
			t.Fatalf("PUT %s failed: %v", tc.key, err)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s status = %d, want %d", tc.key, w.Code, http.StatusOK)
		}

		w, err = doRequest(srv, http.MethodGet, "/keys/"+tc.key, nil)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tc.key, err)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", tc.key, w.Code, http.StatusOK)
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal GET %s: %v", tc.key, err)
		}

		enc, ok := resp["value"].(string)
		if !ok {
			t.Fatalf("GET %s: value is not a string", tc.key)
		}

		decoded, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("base64 decode %s: %v", tc.key, err)
		}

		if string(decoded) != tc.value {
			t.Fatalf("GET %s = %q, want %q", tc.key, decoded, tc.value)
		}
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	store1, err := shelf.Open(dir, 16, 0)
	if err != nil {
		t.Fatalf("Open 1 failed: %v", err)
	}
	srv1 := NewServer(store1)

	encoded := base64.StdEncoding.EncodeToString([]byte("persistent"))
	w, err := doRequest(srv1, http.MethodPut, "/keys/data", map[string]string{"value": encoded})
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", w.Code, http.StatusOK)
	}

	store1.Close()

	store2, err := shelf.Open(dir, 16, 0)
	if err != nil {
		t.Fatalf("Open 2 failed: %v", err)
	}
	defer store2.Close()
	srv2 := NewServer(store2)

	w, err = doRequest(srv2, http.MethodGet, "/keys/data", nil)
	if err != nil {
		t.Fatalf("GET after restart failed: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("GET after restart status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	enc, ok := resp["value"].(string)
	if !ok {
		t.Fatal("value is not a string")
	}

	decoded, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	if string(decoded) != "persistent" {
		t.Fatalf("value = %q, want persistent", decoded)
	}
}

func TestConcurrentHTTPRequests(t *testing.T) {
	srv, cleanup := newTestServer(t)
	defer cleanup()

	const goroutines = 50
	const opsPerGoroutine = 100

	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- true }()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i)
				encoded := base64.StdEncoding.EncodeToString([]byte("v"))
				doRequest(srv, http.MethodPut, "/keys/"+key, map[string]string{"value": encoded})
				doRequest(srv, http.MethodGet, "/keys/"+key, nil)
				doRequest(srv, http.MethodDelete, "/keys/"+key, nil)
			}
		}(g)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
