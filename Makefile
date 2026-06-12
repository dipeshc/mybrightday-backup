.DEFAULT_GOAL := build

VERSION ?= development
LDFLAGS := -ldflags "-X github.com/dipeshc/mybrightday-backup/internal/app.Version=$(VERSION)"

.PHONY: build clean tidy fmt test

clean:
	go clean
	rm -f mbdb coverage.out

tidy:
	go mod tidy

fmt:
	go fmt ./...

test:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

build: clean tidy fmt test
	go build $(LDFLAGS) -o mbdb ./cmd/mbdb
