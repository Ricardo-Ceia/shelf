package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ANSI colors and control sequences
const (
	clearScreen = "\033[H\033[2J"
	reset       = "\033[0m"
	bold        = "\033[1m"
	cyan        = "\033[36m"
	green       = "\033[32m"
	yellow      = "\033[33m"
)

func main() {
	url := flag.String("url", "http://localhost:8080/metrics", "URL of the shelf metrics endpoint")
	interval := flag.Duration("interval", 1*time.Second, "Refresh interval")
	flag.Parse()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	// State for calculating rates
	var prevRequests float64
	var prevTime time.Time

	fmt.Print(clearScreen)

	// Fetch once before entering the loop to populate prevRequests
	if metrics, err := fetchMetrics(*url); err == nil {
		prevRequests = metrics["shelf_http_requests_total"]
		prevTime = time.Now()
		renderDashboard(metrics, 0.0)
	} else {
		renderError(err)
	}

	for {
		select {
		case <-sigCh:
			fmt.Println(reset + "\nExiting shelf top...")
			return
		case <-ticker.C:
			metrics, err := fetchMetrics(*url)
			if err != nil {
				renderError(err)
				continue
			}

			now := time.Now()
			qps := 0.0
			if !prevTime.IsZero() {
				elapsed := now.Sub(prevTime).Seconds()
				reqs := metrics["shelf_http_requests_total"]
				if reqs >= prevRequests {
					qps = (reqs - prevRequests) / elapsed
				}
			}

			prevRequests = metrics["shelf_http_requests_total"]
			prevTime = now

			renderDashboard(metrics, qps)
		}
	}
}

func fetchMetrics(url string) (map[string]float64, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	metrics := make(map[string]float64)
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[0]
			// Strip prometheus tags like {le="..."} to just get the base name or specific bucket
			if idx := strings.Index(name, "{"); idx != -1 {
				name = name[:idx] + "_" + extractTag(parts[0], "le")
			}
			val, err := strconv.ParseFloat(parts[1], 64)
			if err == nil {
				metrics[name] = val
			}
		}
	}
	return metrics, nil
}

func extractTag(s, key string) string {
	tagStart := key + "=\""
	idx := strings.Index(s, tagStart)
	if idx == -1 {
		return ""
	}
	valStart := idx + len(tagStart)
	endIdx := strings.Index(s[valStart:], "\"")
	if endIdx == -1 {
		return ""
	}
	return s[valStart : valStart+endIdx]
}

func renderDashboard(m map[string]float64, qps float64) {
	fmt.Print(clearScreen)

	allocMB := m["go_memstats_alloc_bytes"] / 1024 / 1024
	sysMB := m["go_memstats_sys_bytes"] / 1024 / 1024

	fmt.Printf("%s%s=== SHELF NODE TOP ===%s\n\n", bold, cyan, reset)

	fmt.Printf("%sSystem Health%s\n", bold, reset)
	fmt.Printf("  Goroutines: %.0f\n", m["go_goroutines"])
	fmt.Printf("  Memory:     %.2f MB (Alloc) / %.2f MB (Sys)\n\n", allocMB, sysMB)

	fmt.Printf("%sData Store%s\n", bold, reset)
	fmt.Printf("  Total Keys: %.0f\n\n", m["shelf_store_size"])

	fmt.Printf("%sHTTP Server%s\n", bold, reset)
	fmt.Printf("  Total Reqs: %.0f\n", m["shelf_http_requests_total"])
	fmt.Printf("  QPS:        %s%.1f req/s%s\n\n", green, qps, reset)
}

func renderError(err error) {
	fmt.Print(clearScreen)
	fmt.Printf("%s%s=== SHELF NODE TOP ===%s\n\n", bold, cyan, reset)
	fmt.Printf("%sConnection Error:%s %v\n", yellow, reset, err)
	fmt.Printf("\nRetrying in 1 second...\n")
}
