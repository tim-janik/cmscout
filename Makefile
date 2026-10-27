# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

# Baked .version, else .version as git archive bakes it.
version_info != grep -xm1 '^v\?[0-9][^$$]*' .version || git archive HEAD .version | tar -xOf -
TAG := $(word 1, $(version_info))
version_date := $(wordlist 2, 4, $(version_info))
version = $(patsubst v%,%,$(TAG))
ifeq ($(word 4, $(version_info)),)	# do we have any version?
$(error Missing version information, need git describe or .version)
endif
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
	CGO_ENABLED=1 go vet ./...
.PHONY: vet

# == check ==
check: test lint check-agents-md check-tooling check-removals ## Run working-tree and committed-history checks
.PHONY: check

lint: vet lint-go lint-shell lint-python ## Check owned source files
.PHONY: lint

# Lint inputs, uncommitted files included; also works without Git (source archives).
LINT_FIND = find . \( -name .git -o -name .jj -o -name __pycache__ -o -name artifacts \
              -o -name tmp -o -name testdata \) -prune -o -type f ! -name '*.g.*'

lint-go:
	@unformatted=$$($(LINT_FIND) -name '*.go' -print0 | xargs -0 -r gofmt -l); \
	  test -z "$$unformatted" || { printf '%s\n' "$$unformatted"; exit 1; }
.PHONY: lint-go

lint-shell:
	$(LINT_FIND) -name '*.sh' -print0 | xargs -0 -r shellcheck --severity=error
.PHONY: lint-shell

lint-python:
	$(LINT_FIND) -name '*.py' -print0 | xargs -0 -r python3 -c \
	  'import ast, pathlib, sys; [ast.parse(pathlib.Path(p).read_text(), filename=p) for p in sys.argv[1:]]'
.PHONY: lint-python

check-agents-md: ## Check paths and make targets in AGENTS.md
ifneq ($(wildcard AGENTS.md),)
	python3 misc/check-agents-md.py AGENTS.md
endif
.PHONY: check-agents-md

check-tooling: ## Test check scripts and mocked release and IRC tooling
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s misc -p 'test_*.py' -v
ifneq ($(wildcard .github/workflows),)
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s .github/workflows -p test_ircbot.py -v
	.github/workflows/release-test.sh
endif
.PHONY: check-tooling

CHECK_HEAD ?= HEAD
export CHECK_BASE CHECK_HEAD
check-removals: ## Check committed code removals; override CHECK_BASE and CHECK_HEAD to select a range
	python3 misc/check-removals.py
.PHONY: check-removals

# == build ==
# CGO is required because tree-sitter grammars are C libraries.
build: ## Build the cmscout binary
	CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags '-X main.version=$(version)' -o cmscout ./cmd/cmscout
.PHONY: build

version: ## Print the release version
	@echo "$(version)  $(version_date)"
.PHONY: version

# DIST_VERSION overrides the release version, e.g. a nightly tag plus -g<hash>; dist bakes it into .version.
dist: ## Build the source release archive
	$(eval TAG := $(or $(DIST_VERSION),$(TAG)))
	$(eval version_info := $(TAG) $(version_date))
	$(eval distname := cmscout-$(version))
	git diff --quiet HEAD -- || echo 'WARNING: working tree is dirty' >&2
	rm -rf artifacts && mkdir artifacts && echo '$(version_info)' > artifacts/.version
	git archive --prefix=$(distname)/ --add-file=artifacts/.version HEAD ':!.version' | xz -T1 -9 > artifacts/$(distname).tar.xz
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
	cd artifacts && sha256sum $(distname).tar.xz $(package).tar.xz > SHA256SUMS && \
	  sha256sum -c SHA256SUMS
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
