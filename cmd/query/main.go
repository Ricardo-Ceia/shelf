package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

type LogIndex struct {
	path      string
	retention time.Duration
	mu        sync.RWMutex
	entries   []MetricEntry
	offset    int64
	partial   string
}

func NewLogIndex(path string, retention time.Duration) (*LogIndex, error) {
	idx := &LogIndex{path: path, retention: retention}
	if err := idx.reload(); err != nil {
		return nil, err
	}
	return idx, nil
}

func (l *LogIndex) reload() error {
	entries, err := parseLog(l.path)
	if err != nil {
		return err
	}
	stat, err := os.Stat(l.path)
	if err != nil {
		return fmt.Errorf("stat log: %w", err)
	}

	l.mu.Lock()
	l.entries = entries
	l.offset = stat.Size()
	l.partial = ""
	l.evictOldEntries()
	l.mu.Unlock()
	return nil
}

func (l *LogIndex) evictOldEntries() {
	if l.retention <= 0 {
		return
	}
	cutoff := time.Now().Add(-l.retention).Unix()
	idx := sort.Search(len(l.entries), func(i int) bool {
		return l.entries[i].Timestamp >= cutoff
	})
	if idx > 0 {
		newEntries := make([]MetricEntry, len(l.entries)-idx)
		copy(newEntries, l.entries[idx:])
		l.entries = newEntries
	}
}

func (l *LogIndex) Refresh() error {
	stat, err := os.Stat(l.path)
	if err != nil {
		return fmt.Errorf("stat log: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if stat.Size() < l.offset {
		l.mu.Unlock()
		err := l.reload()
		l.mu.Lock()
		return err
	}

	if stat.Size() == l.offset {
		return nil
	}

	f, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	deltaSize := stat.Size() - l.offset
	buf := make([]byte, deltaSize)
	n, err := f.ReadAt(buf, l.offset)
	if err != nil && n <= 0 {
		return fmt.Errorf("read log delta: %w", err)
	}
	buf = buf[:n]

	chunk := l.partial + string(buf)
	lines := strings.Split(chunk, "\n")
	if !strings.HasSuffix(chunk, "\n") {
		l.partial = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	} else {
		l.partial = ""
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry, err := parseLogLine(line)
		if err != nil {
			continue
		}
		l.entries = append(l.entries, entry)
	}

	l.offset += int64(n)
	l.evictOldEntries()

	return nil
}

func (l *LogIndex) Execute(q *Query) QueryResult {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return executeQuery(l.entries, q)
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

	return MetricEntry{Timestamp: ts, Name: name, Labels: labels, Value: value}, nil
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
			result.Data = append(result.Data, DataPoint{Timestamp: e.Timestamp, Value: e.Value})
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
	retention := flag.Duration("retention", 24*time.Hour, "Time-based retention limit for in-memory queries (0 to disable)")
	flag.Parse()

	logFile := "metrics.log"
	addr := ":9090"

	args := flag.Args()
	if len(args) > 0 {
		logFile = args[0]
	}
	if len(args) > 1 {
		addr = args[1]
	}

	if _, err := os.Stat(logFile); err != nil {
		log.Fatalf("log file not accessible: %v", err)
	}

	index, err := NewLogIndex(logFile, *retention)
	if err != nil {
		log.Fatalf("failed to initialize log index: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
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

		if err := index.Refresh(); err != nil {
			writeJSON(w, http.StatusInternalServerError, QueryResult{Error: err.Error()})
			return
		}

		result := index.Execute(parsed)
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("query API listening on %s (log=%s)", addr, logFile)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("query API shutdown error: %v", err)
	}
	log.Printf("query API stopped")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
