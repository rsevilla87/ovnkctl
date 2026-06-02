BINARY    := ovnkctl
MODULE    := github.com/rsevilla/ovnkctl
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
GOFLAGS   := -trimpath

.PHONY: build clean install lint test

build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) .

install:
	go install $(GOFLAGS) -ldflags '$(LDFLAGS)' .

clean:
	rm -f $(BINARY)

lint:
	golangci-lint run ./...

test:
	go test ./...
