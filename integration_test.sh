#!/usr/bin/env bash
set -euo pipefail

echo "=== Integration Test: Full Observability Pipeline ==="

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"; kill %1 %2 %3 2>/dev/null || true' EXIT

PASS=0
FAIL=0

check() {
    local desc="$1"
    local expected="$2"
    local actual="$3"
    if echo "$actual" | grep -q "$expected"; then
        echo "  PASS: $desc"
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $desc"
        echo "    expected: $expected"
        echo "    got: $actual"
        FAIL=$((FAIL + 1))
    fi
}

# 1. Build all binaries
echo ""
echo "[1/7] Building binaries..."
go build -o "$TMPDIR/server" ./cmd/server
go build -o "$TMPDIR/collector" ./cmd/collector
go build -o "$TMPDIR/query" ./cmd/query
echo "  Built: server, collector, query"

# 2. Start shelf
echo ""
echo "[2/7] Starting shelf server..."
"$TMPDIR/server" \
    -addr :18080 \
    -data "$TMPDIR/shelf.db" \
    -shards 16 \
    -snapshot 0 &
sleep 1

# 3. Generate traffic
echo ""
echo "[3/7] Generating traffic..."
curl -s -X PUT localhost:18080/keys/name -d '{"value":"QWxpY2U="}' > /dev/null
curl -s -X PUT localhost:18080/keys/city -d '{"value":"QmVybGlu"}' > /dev/null
curl -s localhost:18080/keys/name > /dev/null
curl -s localhost:18080/health > /dev/null
curl -s localhost:18080/size > /dev/null
echo "  5 requests sent"

# 4. Verify /metrics
echo ""
echo "[4/7] Checking /metrics endpoint..."
METRICS=$(curl -s localhost:18080/metrics)
check "shelf_store_size present" "shelf_store_size" "$METRICS"
check "shelf_http_requests_total present" "shelf_http_requests_total" "$METRICS"
check "shelf_http_request_duration_seconds present" "shelf_http_request_duration_seconds" "$METRICS"
check "store size is 2" "shelf_store_size 2" "$METRICS"

# 5. Start collector
echo ""
echo "[5/7] Starting collector..."
cat > "$TMPDIR/targets.json" <<EOF
{"output":"$TMPDIR/metrics.log","targets":[{"name":"shelf","url":"http://localhost:18080/metrics","interval_seconds":2}]}
EOF
"$TMPDIR/collector" "$TMPDIR/targets.json" &
sleep 3

check "metrics.log created" "shelf_store_size" "$(cat "$TMPDIR/metrics.log" 2>/dev/null || echo '')"

# 6. Start query API
echo ""
echo "[6/7] Starting query API..."
"$TMPDIR/query" "$TMPDIR/metrics.log" :19090 &
sleep 1

# 7. Run queries
echo ""
echo "[7/7] Running queries..."

# Raw query
RESULT=$(curl -s 'localhost:19090/query?q=shelf_store_size')
check "raw query returns data" '"data"' "$RESULT"
check "raw query has value 2" '"value":2' "$RESULT"

# Sum query
RESULT=$(curl -s 'localhost:19090/query?q=sum(shelf_http_requests_total)')
check "sum query returns result" '"result"' "$RESULT"

# Count query
RESULT=$(curl -s 'localhost:19090/query?q=count(shelf_store_size)')
check "count query returns result" '"result"' "$RESULT"

# Health
RESULT=$(curl -s 'localhost:19090/health')
check "query health" '"status":"ok"' "$RESULT"

# Summary
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
