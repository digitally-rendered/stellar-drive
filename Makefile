.PHONY: build test test-int test-e2e test-all lint vet generate clean

build:
	go build ./...

test:
	go test ./... -short -count=1

test-int:
	go test ./... -tags=integration -count=1 -timeout=5m

test-e2e:
	go test ./... -tags=e2e -count=1 -timeout=10m

test-all:
	go test ./... -tags="integration e2e" -count=1 -timeout=10m

lint:
	golangci-lint run ./...

vet:
	go vet ./...

generate:
	go generate ./...

clean:
	go clean ./...
	rm -f coverage.out coverage.html

coverage:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html

tidy:
	go mod tidy
