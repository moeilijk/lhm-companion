BINARY   := lhm-companion
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

.PHONY: build test lint install clean

build:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o build/$(BINARY) ./cmd/$(BINARY)

test:
	go test ./...

lint:
	go vet ./...

install: build
	install -Dm755 build/$(BINARY) /usr/local/bin/$(BINARY)
	install -Dm644 systemd/$(BINARY).service /etc/systemd/system/$(BINARY).service
	systemctl daemon-reload

clean:
	rm -rf build/
