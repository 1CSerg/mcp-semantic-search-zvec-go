.PHONY: build test test-cover test-cover-check lint fmt clean run-http run-stdio setup-hooks

BINARY := mcp-semantic-search-zvec-go
CMD := ./cmd/mcp-semantic-search-zvec-go
COVERAGE_MIN ?= 80
COVERAGE_PACKAGES ?= ./internal/...

setup-hooks:
	@git config core.autocrlf false
	@git config core.safecrlf false
	@git config alias.addnorm '!bash scripts/git-add.sh'
	@-git config --unset alias.add
	@-git config --unset alias.stage
	@echo "Git: core.autocrlf=false, core.safecrlf=false, alias.addnorm=scripts/git-add.sh"

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

test-cover:
	go test -coverprofile=coverage.out $(COVERAGE_PACKAGES)
	go tool cover -func=coverage.out

test-cover-check:
	COVERAGE_MIN=$(COVERAGE_MIN) COVERAGE_PACKAGES="$(COVERAGE_PACKAGES)" bash scripts/check-coverage.sh

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
