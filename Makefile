# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

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
build: ## Build the cmdiff binary
	CGO_ENABLED=1 go build -trimpath -o cmdiff ./cmd/cmdiff
.PHONY: build

# == run ==
run: build ## Build and run on testdata fixtures
	./cmdiff --no-color testdata/old/knob.tsx testdata/new/knob.tsx
.PHONY: run

# == clean ==
clean: ## Remove cached build artifacts and binary
	go clean ./...
	rm -f cmdiff
.PHONY: clean

# == meta ==
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'
.PHONY: help
