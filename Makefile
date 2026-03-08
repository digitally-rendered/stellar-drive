.PHONY: build test test-int test-e2e test-all lint vet generate clean coverage tidy install \
	bench-perf bench-load bench-breaking bench-constrained bench-limits bench-compat bench-clean \
	bench-fuzz bench-stream bench-stream-e2e \
	bench-graphql-fuzz bench-graphql-perf bench-mcp-fuzz bench-stream-fuzz

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

# --- Benchmark targets ---

# Run perf baseline against local MongoDB (native)
bench-perf:
	./benchmarks/measure.sh --app stellar-drive --port 8200 --scenario smoke

# Run perf load test against local MongoDB (native)
bench-load:
	./benchmarks/measure.sh --app stellar-drive --port 8200 --scenario load

# Run stress test (ramp to failure)
bench-breaking:
	./benchmarks/measure.sh --app stellar-drive --port 8200 --scenario breaking

# Run constrained stress test (Docker-based resource limits)
bench-constrained:
	docker compose -f benchmarks/docker-compose.yml up -d
	docker compose -f benchmarks/docker-compose.yml --profile test run \
		-e K6_SCENARIO=breaking k6-perf
	docker compose -f benchmarks/docker-compose.yml down -v

# Run perf test with specific CPU/MEM limits (Docker)
# Usage: make bench-limits STELLAR_CPU=0.5 STELLAR_MEM=128m
bench-limits:
	STELLAR_CPU=$(STELLAR_CPU) STELLAR_MEM=$(STELLAR_MEM) \
	docker compose -f benchmarks/docker-compose.yml up -d
	docker compose -f benchmarks/docker-compose.yml --profile test run \
		-e K6_SCENARIO=$(or $(K6_SCENARIO),load) k6-perf
	docker compose -f benchmarks/docker-compose.yml down -v

# Run compatibility tests (requires slip-stream on :8100)
bench-compat:
	k6 run benchmarks/k6/compat_test.js

# --- Fuzz testing targets ---

# Run schemathesis fuzz tests against local app (must be running on :8200)
bench-fuzz:
	python benchmarks/fuzz/run_fuzz.py --url http://localhost:8200/api/v1 --mode all --output benchmarks/results/fuzz-results.json

# --- Stream testing targets ---

# Run InMemory stream Go benchmarks
bench-stream:
	go test ./benchmarks/stream/ -bench=. -benchmem -count=3

# Run stream e2e tests with Docker brokers
bench-stream-e2e:
	docker compose -f benchmarks/docker-compose.streams.yml up -d --build
	docker compose -f benchmarks/docker-compose.streams.yml --profile test run k6-stream; \
	status=$$?; \
	docker compose -f benchmarks/docker-compose.streams.yml down -v; \
	exit $$status

# --- GraphQL testing targets ---

# Run GraphQL fuzz tests against local app (must be running on :8200)
bench-graphql-fuzz:
	python benchmarks/fuzz/run_graphql_fuzz.py --url http://localhost:8200/graphql --mode all --output benchmarks/results/graphql-fuzz-results.json

# Run GraphQL k6 performance tests (app must be running on :8200)
bench-graphql-perf:
	k6 run benchmarks/k6/graphql_perf.js --env BASE_URL=http://localhost:8200/graphql

# --- MCP testing targets ---

# Run MCP fuzz tests
bench-mcp-fuzz:
	python benchmarks/fuzz/run_mcp_fuzz.py \
		--cmd "./stellar-drive mcp --config benchmarks/stellar.yaml" \
		--mode all --output benchmarks/results/mcp-fuzz-results.json

# --- Stream validation targets ---

# Run stream event schema validation (Go tests)
bench-stream-fuzz:
	go test ./benchmarks/stream/ -run TestStream -v -count=1

# Clean benchmark results
bench-clean:
	rm -f benchmarks/results/*.json
