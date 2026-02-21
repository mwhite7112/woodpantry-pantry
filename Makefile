.PHONY: test test-unit test-integration test-all test-coverage test-coverage-html generate-mocks sqlc

test: test-unit

test-unit:
	go test ./... -count=1 -race

test-integration:
	go test ./... -count=1 -race -tags=integration

test-all: test-unit test-integration

test-coverage:
	go test ./... -count=1 -race -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

test-coverage-html:
	go test ./... -count=1 -race -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html

generate-mocks:
	mockery

sqlc:
	cd internal/db && sqlc generate
