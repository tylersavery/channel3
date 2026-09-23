# Channel Three build targets.
# The dev root holds real channel config and media and never lives in this repo.

GO ?= go
GOFMT ?= gofmt
NPM ?= npm
DEV_ROOT ?= $(HOME)/srv/channel3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.DEFAULT_GOAL := build
.PHONY: build build-arm64 ui ui-dev test lint run dev-root standby-card

# Both binaries embed the web interface, so both wait on the Vite build.
# Plain `go build ./...` does not: web/dist/.gitkeep keeps the embed pattern
# matching on a fresh clone, so the Go toolchain never needs Node.
build: ui
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/channel3 ./cmd/channel3

build-arm64: ui
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o bin/channel3-linux-arm64 ./cmd/channel3

# The guide page. Vite writes web/dist/ui, which web/embed.go carries into the
# binary. It empties that directory on every build and never touches .gitkeep.
ui:
	cd web && $(NPM) ci && $(NPM) run build

# Rebuilds the page on every save. Serve the result with
# `bin/channel3 serve --root $(DEV_ROOT) --ui-dir web/dist/ui` in another
# terminal: one port, no Vite dev server.
ui-dev:
	cd web && $(NPM) run dev

test:
	$(GO) test -race ./...

lint:
	@unformatted="$$($(GOFMT) -l cmd internal tools web/embed.go)"; \
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

# Redraws the Please Stand By card that is embedded in the binary. The PNG is
# committed, so this only runs when the card itself changes.
standby-card:
	$(GO) run ./tools/standbycard -o internal/player/assets/standby.png
