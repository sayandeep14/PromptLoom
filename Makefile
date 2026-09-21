VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/sayandeepgiri/promptloom/internal/cli.version=$(VERSION)

.PHONY: build test vet check install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/loom ./cmd/loom

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/loom

test:
	go test ./...

vet:
	go vet ./...

check: vet test

clean:
	rm -rf bin
