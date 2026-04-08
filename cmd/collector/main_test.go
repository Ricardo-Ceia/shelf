package main

import (
	"os"
	"testing"
)

func TestParseMetricLine(t *testing.T) {
	for _, tc := range []struct {
		line   string
		name   string
		labels string
		value  string
	}{
		{"requests_total 42", "requests_total", "", "42"},
		{"http_duration_bucket{le=\"0.1\"} 5", "http_duration_bucket", `le="0.1"`, "5"},
		{"shelf_store_size 100", "shelf_store_size", "", "100"},
		{"counter 3.14", "counter", "", "3.14"},
		{"metric{a=\"1\",b=\"2\"} 0", "metric", `a="1",b="2"`, "0"},
	} {
		name, labels, value := parseMetricLine(tc.line)
		if name != tc.name {
			t.Errorf("parseMetricLine(%q) name = %q, want %q", tc.line, name, tc.name)
		}
		if labels != tc.labels {
			t.Errorf("parseMetricLine(%q) labels = %q, want %q", tc.line, labels, tc.labels)
		}
		if value != tc.value {
			t.Errorf("parseMetricLine(%q) value = %q, want %q", tc.line, value, tc.value)
		}
	}
}

func TestParseMetricLineInvalid(t *testing.T) {
	for _, line := range []string{
		"# this is a comment",
		"not_a_number abc",
		"missing_value",
		"",
	} {
		name, _, _ := parseMetricLine(line)
		if name != "" {
			t.Errorf("parseMetricLine(%q) should return empty name, got %q", line, name)
		}
	}
}

func TestParseMetrics(t *testing.T) {
	body := `# TYPE requests counter
requests 42
# TYPE duration histogram
duration_bucket{le="0.1"} 5
duration_bucket{le="+Inf"} 10
duration_sum 1.5
duration_count 10
# TYPE size gauge
size 100
`

	lines := parseMetrics(body, 1712345678)

	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6", len(lines))
	}

	if lines[0].name != "requests" || lines[0].value != "42" {
		t.Errorf("first line = %+v, want requests 42", lines[0])
	}

	if lines[0].timestamp != 1712345678 {
		t.Errorf("timestamp = %d, want 1712345678", lines[0].timestamp)
	}

	if lines[1].labels != `le="0.1"` {
		t.Errorf("labels = %q, want le=\"0.1\"", lines[1].labels)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/targets.json"

	if err := os.WriteFile(path, []byte(`{
		"output": "test.log",
		"max_size_mb": 50,
		"targets": [
			{"name": "shelf", "url": "http://localhost:8080/metrics", "interval_seconds": 10}
		]
	}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Output != "test.log" {
		t.Errorf("output = %q, want test.log", cfg.Output)
	}
	if cfg.MaxSizeMB != 50 {
		t.Errorf("max_size_mb = %d, want 50", cfg.MaxSizeMB)
	}

	if len(cfg.Targets) != 1 {
		t.Fatalf("targets count = %d, want 1", len(cfg.Targets))
	}

	if cfg.Targets[0].Name != "shelf" {
		t.Errorf("target name = %q, want shelf", cfg.Targets[0].Name)
	}

	if cfg.Targets[0].Seconds != 10 {
		t.Errorf("target interval = %d, want 10", cfg.Targets[0].Seconds)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/targets.json"

	if err := os.WriteFile(path, []byte(`{"targets": [{"name": "test", "url": "http://localhost/metrics"}]}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Output != "metrics.log" {
		t.Errorf("output = %q, want metrics.log", cfg.Output)
	}

	if cfg.Targets[0].Seconds != 15 {
		t.Errorf("interval = %d, want 15 (default)", cfg.Targets[0].Seconds)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/targets.json"

	if err := os.WriteFile(path, []byte(`not json`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWriteMetrics(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.log"

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer f.Close()

	metrics := []metricLine{
		{timestamp: 1712345678, name: "requests", value: "42", labels: ""},
		{timestamp: 1712345678, name: "duration_bucket", value: "5", labels: `le="0.1"`},
	}

	if err := writeMetrics(f, metrics); err != nil {
		t.Fatalf("writeMetrics: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	expected := "1712345678 requests 42\n1712345678 duration_bucket{le=\"0.1\"} 5\n"
	if string(data) != expected {
		t.Errorf("output = %q, want %q", string(data), expected)
	}
}
