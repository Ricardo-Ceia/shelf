# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .

# Build statically linked binaries
ENV CGO_ENABLED=0
ENV GOOS=linux
RUN go build -o /bin/server ./cmd/server
RUN go build -o /bin/collector ./cmd/collector
RUN go build -o /bin/query ./cmd/query
RUN go build -o /bin/top ./cmd/top

# --- Server Image ---
FROM alpine:latest AS shelf-server
WORKDIR /data
COPY --from=builder /bin/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["server", "-data", "/data/shelf.db"]

# --- Collector Image ---
FROM alpine:latest AS shelf-collector
WORKDIR /data
COPY --from=builder /bin/collector /usr/local/bin/collector
ENTRYPOINT ["collector"]

# --- Query Image ---
FROM alpine:latest AS shelf-query
WORKDIR /data
COPY --from=builder /bin/query /usr/local/bin/query
EXPOSE 9090
ENTRYPOINT ["query"]

# --- Top Image ---
FROM alpine:latest AS shelf-top
COPY --from=builder /bin/top /usr/local/bin/top
ENTRYPOINT ["top"]
