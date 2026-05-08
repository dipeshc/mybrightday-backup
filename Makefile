.DEFAULT_GOAL := build

VERSION ?= development
LDFLAGS := -ldflags "-X github.com/dipesh/mybrightday-photos-downloader/internal/app.Version=$(VERSION)"

.PHONY: build clean tidy fmt

clean:
	go clean
	rm -f mybrightday-photos-downloader

tidy:
	go mod tidy

fmt:
	go fmt ./...

build: clean tidy fmt
	go build $(LDFLAGS) -o mybrightday-photos-downloader ./cmd/mybrightday-photos-downloader
