BINARY    := lhm-companion
BUILD_DIR := build
TARGET    := $(BUILD_DIR)/$(BINARY)
DIST_DIR  := dist
GO        ?= go
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PACKAGE   := $(BINARY)_$(VERSION)_linux_amd64
ARCHIVE   := $(DIST_DIR)/$(PACKAGE).tar.gz
SHA256    := $(ARCHIVE).sha256
LDFLAGS   := -ldflags "-X main.version=$(VERSION)"
SOURCES   := $(shell find cmd internal -type f -name '*.go' 2>/dev/null)

.PHONY: build test lint install package clean check-go

build:
	@command -v $(GO) >/dev/null 2>&1 || { \
		echo "Error: Go 1.22+ is required and '$(GO)' was not found on PATH."; \
		echo "Run 'make build' as your user before 'sudo make install'."; \
		exit 127; \
	}
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(TARGET) ./cmd/$(BINARY)

$(TARGET): $(SOURCES) go.mod $(wildcard go.sum) Makefile
	@command -v $(GO) >/dev/null 2>&1 || { \
		echo "Error: Go 1.22+ is required and '$(GO)' was not found on PATH."; \
		echo "Run 'make build' as your user before 'sudo make install'."; \
		exit 127; \
	}
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $@ ./cmd/$(BINARY)

check-go:
	@command -v $(GO) >/dev/null 2>&1 || { \
		echo "Error: Go 1.22+ is required and '$(GO)' was not found on PATH."; \
		exit 127; \
	}

test: check-go
	$(GO) test ./...

lint: check-go
	$(GO) vet ./...

install: $(TARGET)
	install -Dm755 $(TARGET) /usr/local/bin/$(BINARY)
	install -Dm644 systemd/$(BINARY).service /etc/systemd/system/$(BINARY).service
	systemctl daemon-reload

package: build $(ARCHIVE) $(SHA256)

$(ARCHIVE): $(TARGET) systemd/$(BINARY).service README.md LICENSE
	@rm -rf $(DIST_DIR)/$(PACKAGE)
	@mkdir -p $(DIST_DIR)/$(PACKAGE)
	install -m755 $(TARGET) $(DIST_DIR)/$(PACKAGE)/$(BINARY)
	install -m644 systemd/$(BINARY).service $(DIST_DIR)/$(PACKAGE)/$(BINARY).service
	install -m644 README.md LICENSE $(DIST_DIR)/$(PACKAGE)/
	tar -C $(DIST_DIR) -czf $@ $(PACKAGE)

$(SHA256): $(ARCHIVE)
	@cd $(DIST_DIR) && sha256sum $(PACKAGE).tar.gz > $(PACKAGE).tar.gz.sha256

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
