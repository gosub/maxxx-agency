.PHONY: build clean run fmt vet tidy lint test help

BINARY := nudgent
DB     := agency.db

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	go build -o $(BINARY)

clean: ## Remove binary and database
	rm -f $(BINARY) $(DB)

run: build ## Build and run
	./$(BINARY)

tidy: ## Tidy and verify dependencies
	go mod tidy
	go mod verify

fmt: ## Format all Go files
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: fmt vet ## Format + vet

test: ## Run tests
	go test ./...

deps: tidy ## Download and verify dependencies
	go mod download
