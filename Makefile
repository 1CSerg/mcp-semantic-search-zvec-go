.PHONY: build build-zvec build-release fetch-zvec-libs fetch-onnx-model fetch-onnx-runtime test test-integration test-cover test-cover-check lint fmt clean run-http run-stdio setup-hooks seed-index copy-zvec-runtime smoke-phase1 smoke-phase2 smoke-phase3 smoke-phase4 smoke-phase5 smoke-phase5-docker smoke-staging-multi-windows

BINARY := mcp-semantic-search-zvec-go
CMD := ./cmd/mcp-semantic-search-zvec-go
COVERAGE_MIN ?= 80
COVERAGE_PKG_MIN ?= 50
COVERAGE_PACKAGES ?= ./internal/...
ZVEC_TAGS := -tags zvec
PRODUCTION_TAGS := -tags "zvec,onnx"
INTEGRATION_TAGS := -tags "integration,zvec"
ZVEC_ENV := .deps/zvec-lib.env
ORT_ENV := .deps/onnxruntime.env

setup-hooks:
	@git config core.autocrlf false
	@git config core.safecrlf false
	@git config alias.addnorm '!bash scripts/dev/git-add.sh'
	@-git config --unset alias.add
	@-git config --unset alias.stage
	@echo "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/dev/git-add.sh"

fetch-zvec-libs:
	bash scripts/fetch/fetch-zvec-libs.sh > $(ZVEC_ENV)

fetch-onnx-model:
	bash scripts/fetch/fetch-onnx-model.sh

fetch-onnx-runtime:
	bash scripts/fetch/fetch-onnx-runtime.sh > $(ORT_ENV)

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
	bash scripts/dev/build-release.sh

smoke-phase1: build-zvec
	bash scripts/smoke/run-phase1.sh

smoke-phase2: build-zvec
	bash scripts/smoke/run-phase2.sh

smoke-phase3: build-zvec
	bash scripts/smoke/run-phase3.sh

smoke-phase4: build-zvec fetch-onnx-model
	bash scripts/smoke/run-phase4.sh

smoke-phase5: build-zvec
	bash scripts/smoke/run-phase5.sh

smoke-phase5-docker:
	bash scripts/smoke/run-phase5-docker.sh

smoke-staging-multi-windows: build-zvec
	powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke/run-mcp-staging-multi-windows.ps1

seed-index: fetch-zvec-libs
	. $(ZVEC_ENV) && \
	CGO_ENABLED=1 LD_LIBRARY_PATH="$$ZVEC_LIB_DIR:$$LD_LIBRARY_PATH" \
	go run $(ZVEC_TAGS) ./cmd/seed-index

test:
	go test ./...

test-integration: fetch-zvec-libs
	. $(ZVEC_ENV) && \
	CGO_ENABLED=1 LD_LIBRARY_PATH="$$ZVEC_LIB_DIR:$$LD_LIBRARY_PATH" \
	go test $(INTEGRATION_TAGS) -v ./internal/store/zvec/...

test-cover:
	go test -coverprofile=coverage.out $(COVERAGE_PACKAGES)
	go tool cover -func=coverage.out

test-cover-check:
	COVERAGE_MIN=$(COVERAGE_MIN) COVERAGE_PKG_MIN=$(COVERAGE_PKG_MIN) COVERAGE_PACKAGES="$(COVERAGE_PACKAGES)" bash scripts/dev/check-coverage.sh

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
