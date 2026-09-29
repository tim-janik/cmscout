# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

# git describe, else the .version file baked into source archives by export-subst.
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
	CGO_ENABLED=1 go build -buildvcs=false -trimpath $(if $(version),-ldflags '-X main.version=$(version)') -o cmscout ./cmd/cmscout
.PHONY: build

version: ## Print the release version
	@echo "$(version)  $(version_date)"
.PHONY: version

dist: ## Build the source release archive
	git diff --quiet HEAD -- || echo 'WARNING: working tree is dirty' >&2
	rm -rf artifacts && mkdir artifacts
	git archive --prefix=$(distname)/ HEAD | xz -T1 -9 > artifacts/$(distname).tar.xz
	ls -lh artifacts/*
.PHONY: dist

package: build ## Build the binary release archive
	rm -rf artifacts/$(package) && mkdir -p artifacts/$(package)
	cp -a cmscout git-diff-wrapper.sh README.md doc artifacts/$(package)/
	{ cat LICENSE "$$(go env GOROOT)/LICENSE"; \
	  go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Dir}}{{end}}{{end}}' ./cmd/cmscout | \
	    sort -u | while IFS= read -r dir; do \
	      test -z "$$dir" || find "$$dir" -type f -name LICENSE -exec cat {} +; \
	    done; } > artifacts/$(package)/LICENSE
	tar -C artifacts -cf - $(package) | xz -T1 -9 > artifacts/$(package).tar.xz
	rm -rf artifacts/$(package)
.PHONY: package

distcheck: dist ## Build and check release archives from the source tarball
	work=$$(mktemp -d) && trap 'rm -rf $$work' EXIT && \
	  tar -xJf artifacts/$(distname).tar.xz -C $$work && \
	  $(MAKE) -C $$work/$(distname) package test vet && \
	  cp $$work/$(distname)/artifacts/$(package).tar.xz artifacts/ && \
	  tar -xJf artifacts/$(package).tar.xz -C $$work && \
	  cd $$work/$(distname) && \
	  test "$$($$work/$(package)/cmscout --version)" = "cmscout $(version)" && \
	  $$work/$(package)/cmscout --no-color testdata/old/knob.tsx testdata/new/knob.tsx | \
	  grep -qE 'Matched:.*[1-9]'
	cd artifacts && sha256sum $(distname).tar.xz $(package).tar.xz > $(distname).SHA256SUMS && \
	  sha256sum -c $(distname).SHA256SUMS
	ls -lh artifacts/*
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
