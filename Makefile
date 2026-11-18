BINARY  := proxy
VERSION ?= 0.1.0-dev
LDFLAGS := -s -w -X github.com/proxytui/proxy-tui/internal/app.Version=$(VERSION)
GO      ?= go

.PHONY: all build run test vet fmt tidy check cross clean

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/proxy

run: build
	./$(BINARY)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

check: fmt vet test

cross:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/proxy-windows-amd64.exe ./cmd/proxy
	GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/proxy-linux-amd64 ./cmd/proxy
	GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/proxy-linux-arm64 ./cmd/proxy
	GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/proxy-darwin-amd64 ./cmd/proxy
	GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/proxy-darwin-arm64 ./cmd/proxy

clean:
	rm -rf dist $(BINARY) $(BINARY).exe
