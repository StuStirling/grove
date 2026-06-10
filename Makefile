# Dev Makefile. `make install` builds to BINDIR (on your PATH) so you can use
# grove daily while hacking on it.
BINDIR  ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test vet fmt

build:
	go build -ldflags "$(LDFLAGS)" -o grove .

install:
	go build -ldflags "$(LDFLAGS)" -o "$(BINDIR)/grove" .
	@echo "installed grove $(VERSION) -> $(BINDIR)/grove"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w *.go
