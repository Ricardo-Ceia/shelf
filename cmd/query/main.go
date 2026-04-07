package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DataPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type QueryResult struct {
	Metric string      `json:"metric"`
	Data   []DataPoint `json:"data,omitempty"`
	Result float64     `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type MetricEntry struct {
	Timestamp int64
	Name      string
	Labels    string
	Value     float64
}

func parseLog(path string) ([]MetricEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}

	var entries []MetricEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		entry, err := parseLogLine(line)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func parseLogLine(line string) (MetricEntry, error) {
	// Format: timestamp name{labels} value  or  timestamp name value
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return MetricEntry{}, fmt.Errorf("invalid line format")
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return MetricEntry{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	nameAndLabels := parts[1]
	valueStr := parts[2]

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return MetricEntry{}, fmt.Errorf("invalid value: %w", err)
	}

	var name, labels string
	if idx := strings.Index(nameAndLabels, "{"); idx >= 0 {
		name = nameAndLabels[:idx]
		labels = nameAndLabels[idx+1:]
		if strings.HasSuffix(labels, "}") {
			labels = labels[:len(labels)-1]
		}
	} else {
		name = nameAndLabels
	}

	return MetricEntry{
		Timestamp: ts,
		Name:      name,
		Labels:    labels,
		Value:     value,
	}, nil
}

type Query struct {
	Metric string
	Labels string
	Window time.Duration
	Func   string
}

var queryRegex = regexp.MustCompile(`^(\w+)\((.+)\)$`)

func parseQuery(q string) (*Query, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}

	if matches := queryRegex.FindStringSubmatch(q); matches != nil {
		fn := matches[1]
		inner := matches[2]

		if inner == "" {
			return nil, fmt.Errorf("empty function argument in %q", q)
		}

		parsed, err := parseMetricSelector(inner)
		if err != nil {
			return nil, err
		}
		parsed.Func = fn
		return parsed, nil
	}

	return parseMetricSelector(q)
}

func parseMetricSelector(s string) (*Query, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty selector")
	}

	if strings.ContainsAny(s, "()") {
		return nil, fmt.Errorf("invalid metric name: %q", s)
	}

	q := &Query{}

	// Check for time window: metric[5m]
	if idx := strings.Index(s, "["); idx >= 0 {
		if !strings.HasSuffix(s, "]") {
			return nil, fmt.Errorf("unclosed bracket in %q", s)
		}
		windowStr := s[idx+1 : len(s)-1]
		s = s[:idx]
		d, err := parseDuration(windowStr)
		if err != nil {
			return nil, fmt.Errorf("invalid window: %w", err)
		}
		q.Window = d
	}

	// Check for labels: metric{key="value"}
	if idx := strings.Index(s, "{"); idx >= 0 {
		if !strings.HasSuffix(s, "}") {
			return nil, fmt.Errorf("unclosed brace in %q", s)
		}
		q.Labels = s[idx+1 : len(s)-1]
		s = s[:idx]
	}

	q.Metric = s
	return q, nil
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(s)
	multipliers := map[string]time.Duration{
		"s": time.Second,
		"m": time.Minute,
		"h": time.Hour,
		"d": 24 * time.Hour,
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			num, err := strconv.Atoi(s[:len(s)-len(suffix)])
			if err != nil {
				return 0, err
			}
			return time.Duration(num) * mult, nil
		}
	}

	num, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	return time.Duration(num) * time.Second, nil
}

func filterEntries(entries []MetricEntry, q *Query) []MetricEntry {
	var filtered []MetricEntry
	cutoff := time.Now().Add(-q.Window).Unix()

	for _, e := range entries {
		if e.Name != q.Metric {
			continue
		}
		if q.Labels != "" && e.Labels != q.Labels {
			continue
		}
		if q.Window > 0 && e.Timestamp < cutoff {
			continue
		}
		filtered = append(filtered, e)
	}

	return filtered
}

func executeQuery(entries []MetricEntry, q *Query) QueryResult {
	result := QueryResult{Metric: q.Metric}

	filtered := filterEntries(entries, q)

	if q.Func == "" {
		for _, e := range filtered {
			result.Data = append(result.Data, DataPoint{
				Timestamp: e.Timestamp,
				Value:     e.Value,
			})
		}
		return result
	}

	switch q.Func {
	case "sum":
		for _, e := range filtered {
			result.Result += e.Value
		}
	case "avg":
		if len(filtered) > 0 {
			for _, e := range filtered {
				result.Result += e.Value
			}
			result.Result /= float64(len(filtered))
		}
	case "min":
		if len(filtered) > 0 {
			result.Result = filtered[0].Value
			for _, e := range filtered[1:] {
				if e.Value < result.Result {
					result.Result = e.Value
				}
			}
		}
	case "max":
		if len(filtered) > 0 {
			result.Result = filtered[0].Value
			for _, e := range filtered[1:] {
				if e.Value > result.Result {
					result.Result = e.Value
				}
			}
		}
	case "count":
		result.Result = float64(len(filtered))
	case "rate":
		if len(filtered) >= 2 {
			dt := float64(filtered[len(filtered)-1].Timestamp - filtered[0].Timestamp)
			if dt > 0 {
				dv := filtered[len(filtered)-1].Value - filtered[0].Value
				result.Result = dv / dt
			}
		}
	default:
		result.Error = fmt.Sprintf("unknown function: %q", q.Func)
	}

	return result
}

func main() {
	logFile := "metrics.log"
	addr := ":9090"

	if len(os.Args) > 1 {
		logFile = os.Args[1]
	}
	if len(os.Args) > 2 {
		addr = os.Args[2]
	}

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, QueryResult{Error: "missing query parameter 'q'"})
			return
		}

		parsed, err := parseQuery(q)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, QueryResult{Error: err.Error()})
			return
		}

		entries, err := parseLog(logFile)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, QueryResult{Error: err.Error()})
			return
		}

		result := executeQuery(entries, parsed)
		writeJSON(w, http.StatusOK, result)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	log.Printf("query API listening on %s (log=%s)", addr, logFile)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
