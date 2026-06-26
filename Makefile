# HEXPLUS v2 build targets.
# Cross-compile is intentional and tested on every PR.

BINARY  := hexplus
PKG     := github.com/lolyhexey/hexplus
CMD     := ./cmd/hexplus

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X $(PKG)/internal/version.Version=$(VERSION) \
  -X $(PKG)/internal/version.Commit=$(COMMIT) \
  -X $(PKG)/internal/version.Date=$(DATE)

DIST := dist

.PHONY: build build-all clean test fmt vet tidy run-extract

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

# Three primary linux targets. armv7 covers cheap ARM VPSes.
build-all: $(DIST)/$(BINARY)-linux-amd64 $(DIST)/$(BINARY)-linux-arm64 $(DIST)/$(BINARY)-linux-armv7

$(DIST)/$(BINARY)-linux-amd64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/$(BINARY)-linux-arm64:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $@ $(CMD)

$(DIST)/$(BINARY)-linux-armv7:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o $@ $(CMD)

clean:
	rm -rf $(DIST) $(BINARY) $(BINARY)-*

test:
	go test ./...

fmt:
	gofmt -w -s .

vet:
	go vet ./...

tidy:
	go mod tidy

# Smoke-test the extract pipeline locally (writes to ./tmp/hexplus).
run-extract: build
	./$(BINARY) -extract -lib-dir ./tmp/hexplus
