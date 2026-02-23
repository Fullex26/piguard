BINARY := piguard
VERSION := 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/Fullex26/piguard/internal/daemon.Version=$(VERSION)-$(COMMIT)"

.PHONY: build build-pi build-all test clean install

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/piguard

# Build for Raspberry Pi (ARM64)
build-pi:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-arm64 ./cmd/piguard

# Build for Pi 3/Zero 2 (ARMv7)
build-pi3:
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o bin/$(BINARY)-linux-armv7 ./cmd/piguard

# Build for x86 Linux servers
build-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-linux-amd64 ./cmd/piguard

# Build all targets
build-all: build-pi build-pi3 build-amd64

# Run tests
test:
	go test ./... -v

# Clean build artifacts
clean:
	rm -rf bin/

# Install locally (for development on Pi)
install: build
	sudo cp bin/$(BINARY) /usr/local/bin/$(BINARY)
	sudo chmod 755 /usr/local/bin/$(BINARY)
	@if [ ! -f /etc/piguard/config.yaml ]; then \
		sudo mkdir -p /etc/piguard; \
		sudo cp configs/default.yaml /etc/piguard/config.yaml; \
		echo "Default config installed to /etc/piguard/config.yaml"; \
	fi
	@if [ ! -f /etc/systemd/system/piguard.service ]; then \
		sudo cp configs/piguard.service /etc/systemd/system/; \
		sudo systemctl daemon-reload; \
		echo "Systemd service installed. Enable with: sudo systemctl enable --now piguard"; \
	fi

# Dev: run locally
dev:
	go run ./cmd/piguard run --config configs/default.yaml
