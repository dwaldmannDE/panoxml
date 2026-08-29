BIN := panoxml
PREFIX ?= $(HOME)/.local

.PHONY: build install test clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) .

install:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(PREFIX)/bin/$(BIN) .

test:
	go test ./...

clean:
	rm -f $(BIN)
