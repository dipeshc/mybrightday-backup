.DEFAULT_GOAL := build

VERSION ?= development
LDFLAGS := -ldflags "-X github.com/dipesh/mybrightday-backup/internal/app.Version=$(VERSION)"

.PHONY: build clean tidy fmt

clean:
	go clean
	rm -f mbdb

tidy:
	go mod tidy

fmt:
	go fmt ./...

build: clean tidy fmt
	go build $(LDFLAGS) -o mbdb ./cmd/mbdb
