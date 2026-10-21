.PHONY: build build-zvec build-release fetch-zvec-libs fetch-onnx-model fetch-onnx-runtime test test-local test-local-race test-integration test-cover test-cover-check test-realworld test-realworld-lmstudio lint fmt clean run-http run-stdio setup-hooks seed-index copy-zvec-runtime smoke-phase1 smoke-phase2 smoke-phase3 smoke-phase4 smoke-phase5 smoke-phase5-docker smoke-staging-multi-windows smoke-global-install smoke-global-daemon

BINARY := mcp-semantic-search-zvec-go
CMD := ./cmd/mcp-semantic-search-zvec-go
COVERAGE_MIN ?= 80
COVERAGE_PKG_MIN ?= 50
COVERAGE_PACKAGES ?= ./internal/...
ZVEC_TAGS := -tags zvec
PRODUCTION_TAGS := -tags "zvec,onnx,treesitter"
INTEGRATION_TAGS := -tags "integration,zvec"
# Shell used for the .sh helpers. On Windows with WSL installed, `bash` on PATH is
# usually WSL's bash, which has no Go/MinGW toolchain; point BASH at Git Bash instead:
#   make smoke-phase1 BASH="C:/Program Files/Git/bin/bash.exe"
BASH ?= bash

ZVEC_ENV := .deps/zvec-lib.env
ORT_ENV := .deps/onnxruntime.env

setup-hooks:
	@git config core.autocrlf false
	@git config core.safecrlf false
	@git config alias.addnorm '!bash scripts/dev/git-add.sh'
	@-git config --unset alias.add
	@-git config --unset alias.stage
	@echo "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/dev/git-add.sh"

# Write the env file via a temp file + mv so a failed fetch cannot leave a
# truncated (empty) .deps/zvec-lib.env behind: the shell creates the redirect
# target before the script runs, so a plain `> $(ZVEC_ENV)` empties it on error.
fetch-zvec-libs:
	"$(BASH)" scripts/fetch/fetch-zvec-libs.sh > $(ZVEC_ENV).tmp && mv -f $(ZVEC_ENV).tmp $(ZVEC_ENV)

fetch-onnx-model:
	"$(BASH)" scripts/fetch/fetch-onnx-model.sh

fetch-onnx-runtime:
	"$(BASH)" scripts/fetch/fetch-onnx-runtime.sh > $(ORT_ENV).tmp && mv -f $(ORT_ENV).tmp $(ORT_ENV)

copy-zvec-runtime: fetch-zvec-libs
	@. $(ZVEC_ENV) && \
	case "$$(uname -s)" in \
	  MINGW*|MSYS*|CYGWIN*) \
	    mkdir -p bin && cp -f "$$ZVEC_LIB_DIR/zvec_c_api.dll" bin/ ;; \
	  Darwin*) \
	    mkdir -p bin && cp -f "$$ZVEC_LIB_DIR/libzvec_c_api.dylib" bin/ 2>/dev/null || true ;; \
	  Linux*) \
	    mkdir -p bin && cp -f "$$ZVEC_LIB_DIR/libzvec_c_api.so" bin/ 2>/dev/null || true ;; \
	esac

build:
	go build -o bin/$(BINARY) $(CMD)

build-zvec: fetch-zvec-libs fetch-onnx-runtime copy-zvec-runtime
	. $(ZVEC_ENV) && . $(ORT_ENV) && \
	CGO_ENABLED=1 LD_LIBRARY_PATH="$$ZVEC_LIB_DIR:$$ORT_LIB_DIR:$$LD_LIBRARY_PATH" \
	go build $(PRODUCTION_TAGS) -o bin/$(BINARY) $(CMD)
	. $(ORT_ENV) && cp -f "$$ONNXRUNTIME_SHARED_LIBRARY_PATH" bin/ 2>/dev/null || true

build-release:
	"$(BASH)" scripts/dev/build-release.sh

smoke-phase1: build-zvec
	"$(BASH)" scripts/smoke/run-phase1.sh

smoke-phase2: build-zvec
	"$(BASH)" scripts/smoke/run-phase2.sh

smoke-phase3: build-zvec
	"$(BASH)" scripts/smoke/run-phase3.sh

smoke-phase4: build-zvec fetch-onnx-model
	"$(BASH)" scripts/smoke/run-phase4.sh

smoke-phase5: build-zvec
	"$(BASH)" scripts/smoke/run-phase5.sh

smoke-phase5-docker:
	"$(BASH)" scripts/smoke/run-phase5-docker.sh

smoke-staging-multi-windows: build-zvec
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke/run-mcp-staging-multi-windows.ps1

# Global install layout: one application per user, N projects, isolated data.
# Builds a stub binary on its own (no native tags needed).
smoke-global-install:
	"$(BASH)" scripts/smoke/run-global-install.sh

# Native end-to-end for the global layout: shared daemon + real MCP stdio
# proxies over several collections. Requires the production binary.
smoke-global-daemon: build-zvec
	"$(BASH)" scripts/smoke/run-global-daemon.sh --binary "./bin/$(BINARY)"

seed-index: fetch-zvec-libs
	. $(ZVEC_ENV) && \
	CGO_ENABLED=1 LD_LIBRARY_PATH="$$ZVEC_LIB_DIR:$$LD_LIBRARY_PATH" \
	go run $(ZVEC_TAGS) ./cmd/seed-index

test:
	go test ./...

# Windows hosts with an on-access antivirus/EDR agent can fail to link or exec the
# PIE test binary of internal/embeddings/openai ("Access is denied"). Linking a
# plain exe avoids the agent's scan window; see docs/DEVELOPMENT.md
# ("go test ./... on Windows: PIE test binaries and security software").
# CI keeps the default PIE path on Linux.
TEST_LOCAL_LDFLAGS := -ldflags=-buildmode=exe

test-local:
	go test $(TEST_LOCAL_LDFLAGS) ./...

test-local-race:
	go test $(TEST_LOCAL_LDFLAGS) -race -count=1 ./...

test-integration: fetch-zvec-libs
	. $(ZVEC_ENV) && \
	CGO_ENABLED=1 LD_LIBRARY_PATH="$$ZVEC_LIB_DIR:$$LD_LIBRARY_PATH" \
	go test $(INTEGRATION_TAGS) -v ./internal/store/zvec/...

test-cover:
	go test -coverprofile=coverage.out $(COVERAGE_PACKAGES)
	go tool cover -func=coverage.out

test-cover-check:
	COVERAGE_MIN=$(COVERAGE_MIN) COVERAGE_PKG_MIN=$(COVERAGE_PKG_MIN) COVERAGE_PACKAGES="$(COVERAGE_PACKAGES)" "$(BASH)" scripts/dev/check-coverage.sh

test-realworld: build-zvec
	"$(BASH)" scripts/realworld/run-all.sh --profile onnx

test-realworld-lmstudio: build-zvec
	"$(BASH)" scripts/realworld/run-all.sh --profile lmstudio

lint:
	@which golangci-lint >/dev/null 2>&1 || (echo "install golangci-lint for lint target" && exit 1)
	golangci-lint run ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ dist/ coverage.out

run-http: build
	./bin/$(BINARY) --http --http-addr :8080

run-stdio: build
	./bin/$(BINARY) --stdio
