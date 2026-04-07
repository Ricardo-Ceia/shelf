.PHONY: build test bench lint vet clean run-server run-collector run-query help

BINARY_DIR := bin
SERVER := $(BINARY_DIR)/server
COLLECTOR := $(BINARY_DIR)/collector
QUERY := $(BINARY_DIR)/query
DEMO := $(BINARY_DIR)/demo

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build            Build all binaries"
	@echo "  test             Run all tests with race detector"
	@echo "  bench            Run all benchmarks"
	@echo "  lint             Run go vet"
	@echo "  clean            Remove build artifacts"
	@echo "  run-server       Run the KV store server"
	@echo "  run-collector    Run the metrics collector"
	@echo "  run-query        Run the query API"
	@echo "  run-demo         Run the quick demo"

build: $(SERVER) $(COLLECTOR) $(QUERY) $(DEMO)

$(BINARY_DIR):
	@mkdir -p $(BINARY_DIR)

$(SERVER): $(BINARY_DIR)
	go build -o $(SERVER) ./cmd/server

$(COLLECTOR): $(BINARY_DIR)
	go build -o $(COLLECTOR) ./cmd/collector

$(QUERY): $(BINARY_DIR)
	go build -o $(QUERY) ./cmd/query

$(DEMO): $(BINARY_DIR)
	go build -o $(DEMO) ./cmd/demo

test:
	go test -race -v ./...

bench:
	go test -bench=. -benchmem -count=3 ./...

lint: vet

vet:
	go vet ./...

clean:
	rm -rf $(BINARY_DIR) shelf.db metrics.log targets.json

run-server: build
	$(SERVER) -addr :8080 -data ./shelf.db -shards 16 -snapshot 10000

run-collector: build
	$(COLLECTOR) targets.json

run-query: build
	$(QUERY) metrics.log :9090

run-demo: build
	$(DEMO)
