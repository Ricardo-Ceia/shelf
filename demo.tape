# Output file
Output demo.gif

# Settings for clarity
Set FontSize 18
Set Width 1000
Set Height 650
Set Padding 20
Set Theme "Dracula"
Set TypingSpeed 60ms

# Setup silently
Hide
Type "make build > /dev/null 2>&1"
Enter
Type "killall server collector || true"
Enter
Type "rm -f shelf.db metrics.log"
Enter
Type "clear"
Enter
Show

# --- The Story Begins ---

Type "# 1. Start the Shelf KV store (runs in < 10MB of RAM)"
Sleep 1.5s
Enter
Type "./bin/server -addr :8080 -data ./shelf.db > server.log 2>&1 &"
Sleep 1s
Enter
Sleep 2s

Type "clear"
Enter

Type "# 2. Interact via a simple REST API"
Sleep 1.5s
Enter
Type `curl -s -X PUT localhost:8080/keys/app_config -d '{"value":"dHJ1ZQ=="}'`
Sleep 1s
Enter
Sleep 2s

Type "clear"
Enter

Type "# 3. Start the metrics collector to track QPS, latency, and memory"
Sleep 1.5s
Enter
Type "./bin/collector targets.json > collector.log 2>&1 &"
Sleep 1s
Enter
Sleep 2s

Type "clear"
Enter

# Generate background traffic silently so the dashboard looks alive
Hide
Type "while true; do curl -s localhost:8080/keys/app_config > /dev/null 2>&1; sleep 0.05; done &"
Enter
Type "TRAFFIC_PID=$!"
Enter
Type "clear"
Enter
Show

Type "# 4. Monitor the node's health in real-time with the 'top' CLI"
Sleep 1.5s
Enter
Type "./bin/top -url http://localhost:8080/metrics"
Sleep 1s
Enter

# Let the dashboard run and show the live updating stats for 8 seconds
Sleep 8s

# Cleanup
Hide
Ctrl+C
Type "kill $TRAFFIC_PID || true"
Enter
Type "killall server collector || true"
Enter
