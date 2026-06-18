# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.3
ARG VERSION=0.1.3
ARG ONNXRUNTIME_VERSION=1.26.0
ARG ZVEC_GO_TAG=v0.5.0

FROM golang:${GO_VERSION}-bookworm AS builder
ARG VERSION
ARG ONNXRUNTIME_VERSION
ARG ZVEC_GO_TAG
WORKDIR /src

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git unzip build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
COPY scripts/fetch/ scripts/fetch/
RUN mkdir -p .deps \
    && bash scripts/fetch/fetch-zvec-libs.sh > .deps/zvec-lib.env \
    && go mod download

COPY . .

RUN bash scripts/fetch/fetch-zvec-libs.sh > .deps/zvec-lib.env \
    && bash scripts/fetch/fetch-onnx-runtime.sh > .deps/onnxruntime.env \
    && bash scripts/fetch/fetch-onnx-model.sh /opt/models/paraphrase-multilingual-MiniLM-L12-v2

RUN . .deps/zvec-lib.env \
    && . .deps/onnxruntime.env \
    && CGO_ENABLED=1 \
       LD_LIBRARY_PATH="${ZVEC_LIB_DIR}:${ORT_LIB_DIR}:${LD_LIBRARY_PATH}" \
       go build -tags "zvec,onnx,treesitter" \
       -ldflags="-s -w -X github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version.Version=${VERSION}" \
       -o /out/mcp-semantic-search-zvec-go ./cmd/mcp-semantic-search-zvec-go

RUN . .deps/zvec-lib.env \
    && . .deps/onnxruntime.env \
    && cp -f "${ZVEC_LIB_DIR}/libzvec_c_api.so" /out/ \
    && cp -f "${ONNXRUNTIME_SHARED_LIBRARY_PATH}" /out/

FROM debian:bookworm-slim
ARG VERSION
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/mcp-semantic-search-zvec-go /usr/local/bin/mcp-semantic-search-zvec-go
COPY --from=builder /out/libzvec_c_api.so /usr/local/lib/
COPY --from=builder /out/libonnxruntime.so /usr/local/lib/
COPY --from=builder /opt/models /opt/models
COPY docker/config.yaml /etc/mcp-semantic-search-zvec-go/config.yaml

ENV WORKSPACE_ROOT=/workspace \
    INDEX_DIR=/data/index \
    CONFIG_PATH=/etc/mcp-semantic-search-zvec-go/config.yaml \
    AUTO_INDEX_ON_START=true \
    HTTP_ADDR=:8080 \
    LD_LIBRARY_PATH=/usr/local/lib \
    FILE_WATCHER_BACKEND=polling \
    FILE_WATCHER_POLL_INTERVAL_SECONDS=10

WORKDIR /workspace
EXPOSE 8080
ENTRYPOINT ["mcp-semantic-search-zvec-go"]
CMD ["--http", "--http-addr", ":8080"]
