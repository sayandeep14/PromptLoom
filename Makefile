VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/sayandeepgiri/promptloom/internal/cli.version=$(VERSION)

.PHONY: build test vet fmt fmt-check check install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/loom ./cmd/loom

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/loom

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "not gofmt-formatted:"; gofmt -l .; exit 1)

check: fmt-check vet test

clean:
	rm -rf bin
