.PHONY: test build build-all

VERSION ?= 0.1.0
LDFLAGS := -s -w -X ghx/internal/version.Version=$(VERSION)

test:
	go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ghx ./cmd/ghx

build-all:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ghx-linux-amd64 ./cmd/ghx
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ghx-linux-arm64 ./cmd/ghx
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ghx-windows-amd64.exe ./cmd/ghx
