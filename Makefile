# Local development. Expects PostgreSQL on localhost and the holybook/data
# repository checked out next to this one (../data).

DATABASE_URL ?= postgres://127.0.0.1:5432/holybook
TEST_DATABASE_URL ?= postgres://127.0.0.1:5432/holybook_test
CONTENT ?= ../data/content

export DATABASE_URL

.PHONY: run import test test-live build docker fmt

## run: serve the site on :8080, reloading templates and static files on change
run:
	go run ./cmd/holybook serve -dev internal/web

## import: load the content of $(CONTENT) into the database
import:
	go run ./cmd/holybook import -i $(CONTENT)

## test: unit tests, plus database tests against $(TEST_DATABASE_URL) (wiped!)
test:
	HOLYBOOK_TEST_DATABASE_URL=$(TEST_DATABASE_URL) HOLYBOOK_CONTENT=$(abspath $(CONTENT)) go test ./...

## test-live: compare fetched books with the data repository (needs network)
test-live:
	HOLYBOOK_DATA=$(abspath $(CONTENT)/..) go test ./internal/fetch -run Live -v

## build: build the binary
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o holybook ./cmd/holybook

## docker: build the container image
docker:
	docker build -t holybook .

## fmt: format and vet
fmt:
	gofmt -w .
	go vet ./...
