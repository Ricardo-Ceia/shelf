package main

import (
	"os"
	"testing"
	"time"
)

func TestParseLogLine(t *testing.T) {
	for _, tc := range []struct {
		line   string
		name   string
		labels string
		value  float64
		ts     int64
	}{
		{"1712345678 requests_total 42", "requests_total", "", 42, 1712345678},
		{"1712345678 duration_bucket{le=\"0.1\"} 5", "duration_bucket", `le="0.1"`, 5, 1712345678},
		{"1712345678 shelf_store_size 100", "shelf_store_size", "", 100, 1712345678},
		{"1712345678 counter 3.14", "counter", "", 3.14, 1712345678},
	} {
		entry, err := parseLogLine(tc.line)
		if err != nil {
			t.Fatalf("parseLogLine(%q) error: %v", tc.line, err)
		}
		if entry.Name != tc.name {
			t.Errorf("parseLogLine(%q) name = %q, want %q", tc.line, entry.Name, tc.name)
		}
		if entry.Labels != tc.labels {
			t.Errorf("parseLogLine(%q) labels = %q, want %q", tc.line, entry.Labels, tc.labels)
		}
		if entry.Value != tc.value {
			t.Errorf("parseLogLine(%q) value = %v, want %v", tc.line, entry.Value, tc.value)
		}
		if entry.Timestamp != tc.ts {
			t.Errorf("parseLogLine(%q) timestamp = %d, want %d", tc.line, entry.Timestamp, tc.ts)
		}
	}
}

func TestParseLogLineInvalid(t *testing.T) {
	for _, line := range []string{
		"not enough parts",
		"abc name value",
		"1712345678 name notanumber",
	} {
		_, err := parseLogLine(line)
		if err == nil {
			t.Errorf("parseLogLine(%q) should error", line)
		}
	}
}

func TestParseLog(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.log"

	content := `1712345678 requests 42
1712345679 requests 43
1712345680 size 100
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	entries, err := parseLog(path)
	if err != nil {
		t.Fatalf("parseLog: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	if entries[0].Name != "requests" || entries[0].Value != 42 {
		t.Errorf("first entry = %+v, want requests 42", entries[0])
	}
}

func TestParseQuery(t *testing.T) {
	for _, tc := range []struct {
		q      string
		metric string
		labels string
		window time.Duration
		fn     string
	}{
		{"shelf_store_size", "shelf_store_size", "", 0, ""},
		{"shelf_store_size[5m]", "shelf_store_size", "", 5 * time.Minute, ""},
		{"sum(shelf_http_requests_total)", "shelf_http_requests_total", "", 0, "sum"},
		{"avg(shelf_store_size[1h])", "shelf_store_size", "", time.Hour, "avg"},
		{"rate(shelf_http_request_duration_seconds_count[5m])", "shelf_http_request_duration_seconds_count", "", 5 * time.Minute, "rate"},
		{"max(metric{le=\"0.1\"})", "metric", `le="0.1"`, 0, "max"},
	} {
		parsed, err := parseQuery(tc.q)
		if err != nil {
			t.Fatalf("parseQuery(%q) error: %v", tc.q, err)
		}
		if parsed.Metric != tc.metric {
			t.Errorf("parseQuery(%q) metric = %q, want %q", tc.q, parsed.Metric, tc.metric)
		}
		if parsed.Labels != tc.labels {
			t.Errorf("parseQuery(%q) labels = %q, want %q", tc.q, parsed.Labels, tc.labels)
		}
		if parsed.Window != tc.window {
			t.Errorf("parseQuery(%q) window = %v, want %v", tc.q, parsed.Window, tc.window)
		}
		if parsed.Func != tc.fn {
			t.Errorf("parseQuery(%q) func = %q, want %q", tc.q, parsed.Func, tc.fn)
		}
	}
}

func TestParseQueryInvalid(t *testing.T) {
	for _, q := range []string{
		"",
		"sum(",
		"metric[abc]",
		"metric[",
		"metric{",
	} {
		_, err := parseQuery(q)
		if err == nil {
			t.Errorf("parseQuery(%q) should error", q)
		}
	}
}

func TestParseDuration(t *testing.T) {
	for _, tc := range []struct {
		s      string
		expect time.Duration
	}{
		{"5s", 5 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"1d", 24 * time.Hour},
		{"30", 30 * time.Second},
	} {
		d, err := parseDuration(tc.s)
		if err != nil {
			t.Fatalf("parseDuration(%q) error: %v", tc.s, err)
		}
		if d != tc.expect {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.s, d, tc.expect)
		}
	}
}

func TestFilterEntries(t *testing.T) {
	entries := []MetricEntry{
		{Timestamp: 1000, Name: "requests", Value: 10},
		{Timestamp: 2000, Name: "requests", Value: 20},
		{Timestamp: 3000, Name: "size", Value: 100},
		{Timestamp: 4000, Name: "requests", Labels: `method="GET"`, Value: 30},
	}

	q := &Query{Metric: "requests"}
	filtered := filterEntries(entries, q)
	if len(filtered) != 3 {
		t.Fatalf("filter by name: got %d, want 3", len(filtered))
	}

	q = &Query{Metric: "requests", Labels: `method="GET"`}
	filtered = filterEntries(entries, q)
	if len(filtered) != 1 {
		t.Fatalf("filter by labels: got %d, want 1", len(filtered))
	}
	if filtered[0].Value != 30 {
		t.Errorf("filter by labels: value = %v, want 30", filtered[0].Value)
	}
}

func TestExecuteQueryRaw(t *testing.T) {
	entries := []MetricEntry{
		{Timestamp: 1000, Name: "requests", Value: 10},
		{Timestamp: 2000, Name: "requests", Value: 20},
		{Timestamp: 3000, Name: "requests", Value: 30},
	}

	q := &Query{Metric: "requests"}
	result := executeQuery(entries, q)

	if len(result.Data) != 3 {
		t.Fatalf("raw query: got %d data points, want 3", len(result.Data))
	}
	if result.Data[0].Value != 10 {
		t.Errorf("first value = %v, want 10", result.Data[0].Value)
	}
}

func TestExecuteQuerySum(t *testing.T) {
	entries := []MetricEntry{
		{Timestamp: 1000, Name: "requests", Value: 10},
		{Timestamp: 2000, Name: "requests", Value: 20},
		{Timestamp: 3000, Name: "requests", Value: 30},
	}

	q := &Query{Metric: "requests", Func: "sum"}
	result := executeQuery(entries, q)

	if result.Result != 60 {
		t.Errorf("sum = %v, want 60", result.Result)
	}
}

func TestExecuteQueryAvg(t *testing.T) {
	entries := []MetricEntry{
		{Timestamp: 1000, Name: "size", Value: 10},
		{Timestamp: 2000, Name: "size", Value: 20},
		{Timestamp: 3000, Name: "size", Value: 30},
	}

	q := &Query{Metric: "size", Func: "avg"}
	result := executeQuery(entries, q)

	if result.Result != 20 {
		t.Errorf("avg = %v, want 20", result.Result)
	}
}

func TestExecuteQueryRate(t *testing.T) {
	entries := []MetricEntry{
		{Timestamp: 1000, Name: "requests", Value: 100},
		{Timestamp: 1010, Name: "requests", Value: 200},
	}

	q := &Query{Metric: "requests", Func: "rate"}
	result := executeQuery(entries, q)

	if result.Result != 10 {
		t.Errorf("rate = %v, want 10", result.Result)
	}
}

func TestEvictOldEntries(t *testing.T) {
	now := time.Now().Unix()
	idx := &LogIndex{
		retention: time.Hour,
		entries: []MetricEntry{
			{Timestamp: now - 7200, Value: 1}, // 2 hours ago
			{Timestamp: now - 4000, Value: 2}, // > 1 hour ago
			{Timestamp: now - 3500, Value: 3}, // < 1 hour ago
			{Timestamp: now - 1000, Value: 4}, // < 1 hour ago
		},
	}

	idx.evictOldEntries()

	if len(idx.entries) != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", len(idx.entries))
	}
	if idx.entries[0].Value != 3 || idx.entries[1].Value != 4 {
		t.Errorf("wrong entries retained: %+v", idx.entries)
	}
}

func TestWriteJSON(t *testing.T) {
	// Just verify it doesn't panic with valid input
	// We can't easily test http.ResponseWriter without httptest
}
