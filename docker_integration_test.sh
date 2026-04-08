#!/usr/bin/env bash
set -e

echo "=== Docker Integration Test ==="

echo "[1/6] Starting docker-compose stack..."
docker compose up -d --build

cleanup() {
    echo "[6/6] Tearing down docker-compose stack..."
    docker compose down -v
}
trap cleanup EXIT

echo "[2/6] Waiting for server to be healthy..."
for i in {1..15}; do
    if curl -s http://localhost:8080/health > /dev/null; then
        echo "  Server is healthy!"
        break
    fi
    sleep 1
    if [ "$i" -eq 15 ]; then
        echo "  Server failed to start."
        docker compose logs server
        exit 1
    fi
done

echo "[3/6] Generating traffic..."
curl -s -X PUT http://localhost:8080/keys/docker1 -d '"value1"' > /dev/null
curl -s -X PUT http://localhost:8080/keys/docker2 -d '"value2"' > /dev/null
curl -s http://localhost:8080/metrics > /dev/null

echo "[4/6] Waiting for collector to scrape and query API to serve..."
# The collector scrapes every 2 seconds according to docker-targets.json
success=0
for i in {1..15}; do
    # Query API runs on 9090 and reads /data/metrics.log
    if curl -s "http://localhost:9090/query?q=shelf_store_size" | grep -q "data"; then
        echo "  Query API has data!"
        success=1
        break
    fi
    sleep 1
done

if [ "$success" -eq 0 ]; then
    echo "  Query API failed to serve metrics."
    docker compose logs
    exit 1
fi

echo "[5/6] Testing 'top' command in docker..."
# top runs indefinitely, we can use timeout
# The 'server' service image has 'top' installed in /usr/local/bin
OUTPUT=$(docker compose run --rm --entrypoint /bin/sh top -c "timeout 2 /usr/local/bin/top -url http://server:8080/metrics || true")
if echo "$OUTPUT" | grep -q "SHELF NODE TOP"; then
    echo "  'top' command executed successfully."
else
    echo "  'top' command failed."
    echo "Output: $OUTPUT"
    exit 1
fi

echo "=== Docker Integration Test Passed ==="
