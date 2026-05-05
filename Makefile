.DEFAULT_GOAL := build

VERSION ?= development
LDFLAGS := -ldflags "-X github.com/dipesh/daycare-photos/internal/app.Version=$(VERSION)"

.PHONY: build clean tidy fmt

clean:
	go clean
	rm -f daycare-photos

tidy:
	go mod tidy

fmt:
	go fmt ./...

build: clean tidy fmt
	go build $(LDFLAGS) -o daycare-photos ./cmd/daycare-photos
