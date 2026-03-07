.PHONY: build test test-int test-e2e test-all lint vet generate clean coverage tidy install

# Build all packages (verify compilation).
build:
	go build ./...

# Build the stellar-drive binary.
install:
	go build -o stellar-drive ./cmd/stellar-drive

# Run unit tests (short mode, no caching).
test:
	go test ./... -short -count=1

# Run integration tests (requires MongoDB).
test-int:
	go test ./... -tags=integration -count=1 -timeout=5m

# Run end-to-end tests.
test-e2e:
	go test ./... -tags=e2e -count=1 -timeout=10m

# Run all tests including integration and e2e.
test-all:
	go test ./... -tags="integration e2e" -count=1 -timeout=10m

# Run golangci-lint.
lint:
	golangci-lint run ./...

# Run go vet.
vet:
	go vet ./...

# Run go generate.
generate:
	go generate ./...

# Remove build artifacts.
clean:
	go clean ./...
	rm -f stellar-drive coverage.out coverage.html

# Generate test coverage report.
coverage:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html

# Tidy go.mod and go.sum.
tidy:
	go mod tidy
