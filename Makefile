# Channel Three build targets.
# The dev root holds real channel config and media and never lives in this repo.

GO ?= go
GOFMT ?= gofmt
NPM ?= npm
SHELLCHECK ?= shellcheck
DEV_ROOT ?= $(HOME)/srv/channel3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

# Pi deployment. PI_HOST is the ssh target, PI_DISK the library SSD as a device
# path or a PARTUUID, ARGS the extra arguments passed through to ingest.
PI_TZ ?= America/Toronto

.DEFAULT_GOAL := build
.PHONY: build build-arm64 ui ui-dev test lint run dev-root standby-card \
	deploy pi-setup pi-ingest

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
	@for script in deploy/*.sh; do bash -n "$$script" || exit 1; done
	$(SHELLCHECK) deploy/*.sh

run:
	$(GO) run ./cmd/channel3 serve --root $(DEV_ROOT) --listen :3333

dev-root:
	mkdir -p $(DEV_ROOT)/channels $(DEV_ROOT)/library $(DEV_ROOT)/local

# Redraws the Please Stand By card that is embedded in the binary. The PNG is
# committed, so this only runs when the card itself changes.
standby-card:
	$(GO) run ./tools/standbycard -o internal/player/assets/standby.png

# Builds arm64, copies the binary to the Pi and restarts the service. A deploy
# restarts everything, so never run it while an ingest is running or while the
# kids are watching. deploy.sh refuses if it finds an ingest.
deploy:
	@test -n "$(PI_HOST)" || { echo "make deploy: set PI_HOST, for example PI_HOST=channel3.local"; exit 2; }
	PI_HOST=$(PI_HOST) deploy/deploy.sh

# One-time setup of a freshly flashed Pi. Safe to re-run. PI_DISK is the
# library SSD, as /dev/sda1 or PARTUUID=xxxx. See deploy/README.md.
pi-setup:
	@test -n "$(PI_HOST)" || { echo "make pi-setup: set PI_HOST, for example PI_HOST=channel3.local"; exit 2; }
	@test -n "$(PI_DISK)" || { echo "make pi-setup: set PI_DISK, for example PI_DISK=/dev/sda1"; exit 2; }
	ssh $(PI_HOST) rm -rf /tmp/channel3-deploy
	scp -r deploy $(PI_HOST):/tmp/channel3-deploy
	ssh -t $(PI_HOST) sudo bash /tmp/channel3-deploy/pi-setup.sh "$(PI_DISK)" "$(PI_TZ)"

# Runs an ingest on the Pi. Pass flags through with ARGS="--channel bluey".
pi-ingest:
	@test -n "$(PI_HOST)" || { echo "make pi-ingest: set PI_HOST, for example PI_HOST=channel3.local"; exit 2; }
	PI_HOST=$(PI_HOST) deploy/ingest.sh $(ARGS)
