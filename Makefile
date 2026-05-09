BINARY    := ingress-nginx-migration
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -ldflags "-X main.version=$(VERSION) -s -w"
BUILD_DIR := dist

.PHONY: all build build-all test lint e2e-test install clean

all: build

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

build-all:
	GOOS=linux  GOARCH=amd64  go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-amd64  ./cmd/$(BINARY)
	GOOS=linux  GOARCH=arm64  go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-linux-arm64  ./cmd/$(BINARY)
	GOOS=darwin GOARCH=amd64  go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/$(BINARY)
	GOOS=darwin GOARCH=arm64  go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/$(BINARY)

test:
	go test ./internal/... -v -count=1

lint:
	golangci-lint run ./...

e2e-test:
	@if [ -z "$(ISTIO_IMAGE)" ]; then echo "❌  ISTIO_IMAGE is required."; exit 1; fi
	ISTIO_IMAGE=$(ISTIO_IMAGE) E2E_REUSE_CLUSTER=$(E2E_REUSE_CLUSTER) \
	go test ./e2e/... -v -count=1 -timeout 10m

install: build
	install -m 0755 $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)
