# Makefile for cleanslate

BIN_DIR := bin
BINARY := $(BIN_DIR)/cleanslate

VERSION_MAJOR := $(shell cat .stencil/version_major)
VERSION_MINOR := $(shell cat .stencil/version_minor)
VERSION_PATCH := $(shell cat .stencil/version_patch)
VERSION := v$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)

COMMIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BRANCH      := $(shell git symbolic-ref --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -X 'main.appVersion=$(VERSION)' \
           -X 'main.appBuildDate=$(BUILD_DATE)' \
           -X 'main.appCommitHash=$(COMMIT_HASH)' \
           -X 'main.appBranch=$(BRANCH)'

.PHONY: all build check test vet fmt clean major minor patch

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/cli

# The package list is explicit rather than ./...: a completed image build
# leaves root-owned mkosi.cache/ and mkosi.tools/ trees in the working
# directory that the Go tool cannot walk.
check: fmt vet test

test:
	go test ./cmd/... ./internal/...

vet:
	go vet ./cmd/... ./internal/...

fmt:
	gofmt -l -w ./cmd ./internal

clean:
	rm -rf $(BIN_DIR)

major:
	@echo "Bumping major version"
	@echo $(shell expr $(VERSION_MAJOR) + 1) > .stencil/version_major
	@echo 0 > .stencil/version_minor
	@echo 0 > .stencil/version_patch

minor:
	@echo "Bumping minor version"
	@echo $(VERSION_MAJOR) > .stencil/version_major
	@echo $(shell expr $(VERSION_MINOR) + 1) > .stencil/version_minor
	@echo 0 > .stencil/version_patch

patch:
	@echo "Bumping patch version"
	@echo $(VERSION_MAJOR) > .stencil/version_major
	@echo $(VERSION_MINOR) > .stencil/version_minor
	@echo $(shell expr $(VERSION_PATCH) + 1) > .stencil/version_patch
