# Channel Three build targets.
# The dev root holds real channel config and media and never lives in this repo.

GO ?= go
GOFMT ?= gofmt
DEV_ROOT ?= $(HOME)/srv/channel3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.DEFAULT_GOAL := build
.PHONY: build build-arm64 test lint run dev-root

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/channel3 ./cmd/channel3

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/channel3-linux-arm64 ./cmd/channel3

test:
	$(GO) test -race ./...

lint:
	@unformatted="$$($(GOFMT) -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to run on:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	$(GO) vet ./...

run:
	$(GO) run ./cmd/channel3 serve --root $(DEV_ROOT) --listen :3333

dev-root:
	mkdir -p $(DEV_ROOT)/channels $(DEV_ROOT)/library $(DEV_ROOT)/local
