.PHONY: build test lint generate-test-coverage clean help

.DEFAULT_GOAL := help

build:
	go build -o n2o ./cmd/

test:
	go test ./...

lint:
	golangci-lint run

format:
	golangci-lint fmt .

generate-test-coverage:
	@echo "Generating coverage report"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

clean:
	rm -f coverage.out coverage.html

help:
	@echo "Available targets:"
	@echo "  build                  - Build the CLI binary"
	@echo "  test                   - Run all tests"
	@echo "  generate-test-coverage - Generate coverage report"
	@echo "  lint                   - Run golangci-lint"
	@echo "  format                 - Format Go code"
	@echo "  clean                  - Clean build artifacts"
	@echo "  help                   - Show this help message"
