# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

# Release version and commit date: git describe, else the .version file baked
# into source archives by git export-subst; man pages take -M date="$(version_date)".
TAG != git log -1 --pretty='%(describe:tags,match=v[0-9]*.[0-9]*)' HEAD 2>/dev/null || sed -n 's/ .*//p' .version 2>/dev/null
version_date != git log -1 --format=%ci 2>/dev/null || sed -n 's/^[^ ]* //p' .version 2>/dev/null
version = $(patsubst v%,%,$(TAG))
distname := cmscout-$(version)
platform = $(shell go env GOOS GOARCH | paste -sd-)
package = $(distname)-$(platform)

# == all ==
all: build ## Compile all packages
.PHONY: all

# == test ==
test: ## Run all tests
	CGO_ENABLED=1 go test -count=1 ./...
.PHONY: test

# == bench ==
bench: ## Run all benchmarks
	CGO_ENABLED=1 go test -bench=. ./...
.PHONY: bench

# == vet ==
vet: ## Run go vet
	go vet ./...
.PHONY: vet

# == build ==
# CGO is required because tree-sitter grammars are C libraries.
build: ## Build the cmscout binary
	CGO_ENABLED=1 go build -trimpath $(if $(version),-ldflags '-X main.version=$(version)') -o cmscout ./cmd/cmscout
.PHONY: build

version: ## Print the release version
	@echo "$(version)  $(version_date)"
.PHONY: version

dist: build ## Build source and binary release archives
	git diff --quiet HEAD -- || echo 'WARNING: working tree is dirty' >&2
	rm -rf artifacts && mkdir artifacts
	git archive --prefix=$(distname)/ HEAD | xz -T1 -9 > artifacts/$(distname).tar.xz
	git archive --prefix=$(package)/ --add-file=cmscout HEAD git-diff-wrapper.sh LICENSE README.md doc | \
	  xz -T1 -9 > artifacts/$(package).tar.xz
	cd artifacts && sha256sum $(distname).tar.xz $(package).tar.xz > $(distname)-SHA256SUMS
	ls -lh artifacts/*
.PHONY: dist
$(distname)-SHA256SUMS: dist

distcheck: $(distname)-SHA256SUMS ## Check binary and source archive
	cd artifacts && sha256sum -c $(distname)-SHA256SUMS
	work=$$(mktemp -d) && trap 'rm -rf $$work' EXIT && \
	  test "$$(xz -dc artifacts/$(package).tar.xz | git get-tar-commit-id || :)" = "$$(git rev-parse HEAD)" && \
	  mkdir $$work/pkg && xz -dc artifacts/$(package).tar.xz | tar -x -C $$work/pkg && \
	  xz -dc artifacts/$(distname).tar.xz > $$work/source.tar && tar -xf $$work/source.tar -C $$work && \
	  test "$$(git get-tar-commit-id < $$work/source.tar)" = "$$(git rev-parse HEAD)" && \
	  cd / && test "$$($$work/pkg/$(package)/cmscout --version)" = "cmscout $(version)" && \
	  $(MAKE) -C $$work/$(distname) build test vet && shellcheck $$work/$(distname)/.github/workflows/*.sh && \
	  test "$$($$work/$(distname)/cmscout --version)" = "cmscout $(version)" && \
	  $$work/$(distname)/cmscout --no-color $$work/$(distname)/testdata/old/knob.tsx $$work/$(distname)/testdata/new/knob.tsx | \
	  grep -qE 'Matched:.*[1-9]'
.PHONY: distcheck

# == run ==
run: build ## Build and run on testdata fixtures
	./cmscout --no-color testdata/old/knob.tsx testdata/new/knob.tsx
.PHONY: run

# == clean ==
clean: ## Remove cached build artifacts and binary
	go clean ./...
	rm -rf artifacts cmscout
.PHONY: clean

# == meta ==
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN { FS = ":.*?## " } { printf "\033[36m%-12s\033[0m %s\n", $$1, $$2 }'
.PHONY: help
