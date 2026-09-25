# Dev Makefile. `make install` builds grove and puts `grove` on your PATH (BINDIR)
# so you can use it daily while hacking on it. Needs the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@latest
BINDIR  ?= $(HOME)/.local/bin
APPDIR  ?= /Applications
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
WAILS   ?= $(shell command -v wails 2>/dev/null || echo $(HOME)/go/bin/wails)
# Ubuntu 24.04+ and other current distros ship WebKitGTK 4.1.
TAGS    ?= $(if $(filter Linux,$(shell uname)),webkit2_41,)

.PHONY: build install dev frontend test vet fmt

# build/bin/grove.app on macOS, build/bin/grove on Linux.
build:
	$(WAILS) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)"

install: build
ifeq ($(shell uname),Darwin)
	mkdir -p "$(APPDIR)" "$(BINDIR)"
	rm -rf "$(APPDIR)/grove.app"
	cp -R build/bin/grove.app "$(APPDIR)/"
	ln -sf "$(APPDIR)/grove.app/Contents/MacOS/grove" "$(BINDIR)/grove"
else
	install -D -m 755 build/bin/grove "$(BINDIR)/grove"
endif
	@echo "installed grove $(VERSION) -> $(BINDIR)/grove"

dev:
	$(WAILS) dev -tags "$(TAGS)"

# The Go binary embeds frontend/dist, so Go builds and tests need it first.
frontend:
	cd frontend && npm install --no-audit --no-fund && npm run build

test: frontend
	go test -tags "$(TAGS)" ./...

vet: frontend
	go vet -tags "$(TAGS)" ./...

fmt:
	gofmt -w *.go
