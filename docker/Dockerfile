# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.3
ARG VERSION=0.1.0

FROM golang:${GO_VERSION}-bookworm AS builder
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version.Version=${VERSION}" \
    -o /out/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go

FROM debian:bookworm-slim
ARG VERSION
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/mcp-semantic-search-zvec-go /usr/local/bin/mcp-semantic-search-zvec-go
COPY config.yaml /etc/mcp-semantic-search-zvec-go/config.yaml

ENV WORKSPACE_ROOT=/workspace \
    INDEX_DIR=/data/index \
    CONFIG_PATH=/workspace/.mcp-semantic-search-zvec-go/config.yaml \
    AUTO_INDEX_ON_START=true \
    HTTP_ADDR=:8080

WORKDIR /workspace
EXPOSE 8080
ENTRYPOINT ["mcp-semantic-search-zvec-go"]
CMD ["--http", "--http-addr", ":8080"]
