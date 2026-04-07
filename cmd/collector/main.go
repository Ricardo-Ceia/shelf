package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Target struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Seconds int    `json:"interval_seconds"`
}

type Config struct {
	Output  string   `json:"output"`
	Targets []Target `json:"targets"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Output == "" {
		cfg.Output = "metrics.log"
	}
	for i := range cfg.Targets {
		if cfg.Targets[i].Seconds <= 0 {
			cfg.Targets[i].Seconds = 15
		}
	}
	return &cfg, nil
}

type metricLine struct {
	timestamp int64
	name      string
	value     string
	labels    string
}

func parseMetrics(body string, timestamp int64) []metricLine {
	var lines []metricLine
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, labels, value := parseMetricLine(line)
		if name == "" {
			continue
		}
		lines = append(lines, metricLine{
			timestamp: timestamp,
			name:      name,
			value:     value,
			labels:    labels,
		})
	}
	return lines
}

func parseMetricLine(line string) (name, labels, value string) {
	spaceIdx := strings.LastIndex(line, " ")
	if spaceIdx < 0 {
		return "", "", ""
	}
	value = strings.TrimSpace(line[spaceIdx+1:])
	left := strings.TrimSpace(line[:spaceIdx])

	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return "", "", ""
	}

	if idx := strings.Index(left, "{"); idx >= 0 {
		name = left[:idx]
		labels = left[idx+1:]
		if strings.HasSuffix(labels, "}") {
			labels = labels[:len(labels)-1]
		}
	} else {
		name = left
	}

	return name, labels, value
}

func scrape(target Target, client *http.Client) ([]metricLine, error) {
	resp, err := client.Get(target.URL)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", target.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: status %d", target.Name, resp.StatusCode)
	}

	var buf strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}

	now := time.Now().Unix()
	return parseMetrics(buf.String(), now), nil
}

func writeMetrics(f *os.File, metrics []metricLine) error {
	for _, m := range metrics {
		if m.labels != "" {
			_, err := fmt.Fprintf(f, "%d %s{%s} %s\n", m.timestamp, m.name, m.labels, m.value)
			if err != nil {
				return err
			}
		} else {
			_, err := fmt.Fprintf(f, "%d %s %s\n", m.timestamp, m.name, m.value)
			if err != nil {
				return err
			}
		}
	}
	return f.Sync()
}

func main() {
	configPath := "targets.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open output file: %v", err)
	}
	defer f.Close()

	client := &http.Client{Timeout: 10 * time.Second}

	log.Printf("collector started, output=%s, targets=%d", cfg.Output, len(cfg.Targets))

	type targetState struct {
		target Target
		ticker *time.Ticker
	}

	states := make([]targetState, len(cfg.Targets))
	for i, t := range cfg.Targets {
		states[i] = targetState{
			target: t,
			ticker: time.NewTicker(time.Duration(t.Seconds) * time.Second),
		}
	}

	for {
		for i := range states {
			s := &states[i]
			select {
			case <-s.ticker.C:
				start := time.Now()
				metrics, err := scrape(s.target, client)
				if err != nil {
					log.Printf("error: %v", err)
					continue
				}
				if err := writeMetrics(f, metrics); err != nil {
					log.Printf("error writing metrics: %v", err)
				} else {
					log.Printf("scraped %s: %d metrics in %s", s.target.Name, len(metrics), time.Since(start))
				}
			default:
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}
