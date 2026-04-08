package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchMetrics(t *testing.T) {
	// Mock prometheus output
	mockData := `
# TYPE go_goroutines gauge
go_goroutines 12
# TYPE shelf_http_requests_total counter
shelf_http_requests_total 42
# TYPE shelf_http_request_duration_seconds histogram
shelf_http_request_duration_seconds_bucket{le="0.001"} 5
shelf_http_request_duration_seconds_sum 0.123
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockData))
	}))
	defer ts.Close()

	metrics, err := fetchMetrics(ts.URL)
	if err != nil {
		t.Fatalf("fetchMetrics failed: %v", err)
	}

	if metrics["go_goroutines"] != 12 {
		t.Errorf("expected 12 goroutines, got %v", metrics["go_goroutines"])
	}
	if metrics["shelf_http_requests_total"] != 42 {
		t.Errorf("expected 42 requests, got %v", metrics["shelf_http_requests_total"])
	}
	if metrics["shelf_http_request_duration_seconds_bucket_0.001"] != 5 {
		t.Errorf("expected 5 bucket items, got %v", metrics["shelf_http_request_duration_seconds_bucket_0.001"])
	}
}
